/*
Copyright 2022 The KodeRover Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package job

import (
	"fmt"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/koderover/zadig/pkg/microservice/aslan/config"
	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	commonrepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb"
	templaterepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb/template"
	"github.com/koderover/zadig/pkg/microservice/aslan/core/common/service/repository"
	commontypes "github.com/koderover/zadig/pkg/microservice/aslan/core/common/types"
	commonutil "github.com/koderover/zadig/pkg/microservice/aslan/core/common/util"
	"github.com/koderover/zadig/pkg/setting"
	"github.com/koderover/zadig/pkg/tool/log"
	"github.com/koderover/zadig/pkg/util"
)

type DeployJob struct {
	job      *commonmodels.Job
	workflow *commonmodels.WorkflowV4
	spec     *commonmodels.ZadigDeployJobSpec
}

func (j *DeployJob) Instantiate() error {
	j.spec = &commonmodels.ZadigDeployJobSpec{}
	if err := commonmodels.IToiYaml(j.job.Spec, j.spec); err != nil {
		return err
	}
	j.setDefaultDeployContent()
	j.job.Spec = j.spec
	return nil
}

func (j *DeployJob) setDefaultDeployContent() {
	if j.spec.DeployContents == nil || len(j.spec.DeployContents) <= 0 {
		j.spec.DeployContents = []config.DeployContent{config.DeployImage}
	}
}

func (j *DeployJob) getOriginReferedJobTargets(jobName string) ([]*commonmodels.ServiceAndImage, error) {
	serviceAndImages := []*commonmodels.ServiceAndImage{}
	for _, stage := range j.workflow.Stages {
		for _, job := range stage.Jobs {
			if job.Name != j.spec.JobName {
				continue
			}
			if job.JobType == config.JobZadigBuild {
				buildSpec := &commonmodels.ZadigBuildJobSpec{}
				if err := commonmodels.IToi(job.Spec, buildSpec); err != nil {
					return serviceAndImages, err
				}
				for _, build := range buildSpec.ServiceAndBuilds {
					serviceAndImages = append(serviceAndImages, &commonmodels.ServiceAndImage{
						ServiceName:   build.ServiceName,
						ServiceModule: build.ServiceModule,
						Image:         build.Image,
					})
				}
				return serviceAndImages, nil
			}
			if job.JobType == config.JobZadigDistributeImage {
				distributeSpec := &commonmodels.ZadigDistributeImageJobSpec{}
				if err := commonmodels.IToi(job.Spec, distributeSpec); err != nil {
					return serviceAndImages, err
				}
				for _, distribute := range distributeSpec.Tatgets {
					serviceAndImages = append(serviceAndImages, &commonmodels.ServiceAndImage{
						ServiceName:   distribute.ServiceName,
						ServiceModule: distribute.ServiceModule,
						Image:         distribute.TargetImage,
					})
				}
				return serviceAndImages, nil
			}
		}
	}
	return nil, fmt.Errorf("build job %s not found", jobName)
}

func (j *DeployJob) SetPreset() error {
	j.spec = &commonmodels.ZadigDeployJobSpec{}
	if err := commonmodels.IToi(j.job.Spec, j.spec); err != nil {
		return err
	}
	j.setDefaultDeployContent()
	j.job.Spec = j.spec
	var err error
	project, err := templaterepo.NewProductColl().Find(j.workflow.Project)
	if err != nil {
		return fmt.Errorf("failed to find project %s, err: %v", j.workflow.Project, err)
	}
	if project.ProductFeature != nil {
		j.spec.DeployType = project.ProductFeature.DeployType
	}
	// if quoted job quote another job, then use the service and image of the quoted job
	if j.spec.Source == config.SourceFromJob {
		j.spec.OriginJobName = j.spec.JobName
		j.spec.JobName = getOriginJobName(j.workflow, j.spec.JobName)
	} else if j.spec.Source == config.SourceRuntime {
		envName := strings.ReplaceAll(j.spec.Env, setting.FixedValueMark, "")
		product, err := commonrepo.NewProductColl().Find(&commonrepo.ProductFindOptions{Name: j.workflow.Project, EnvName: envName})
		if err != nil {
			log.Errorf("can't find product %s in env %s, error: %w", j.workflow.Project, envName, err)
			return nil
		}

		findOption := &commonrepo.RenderSetFindOption{
			Name:     product.Render.Name,
			Revision: product.Render.Revision,
			EnvName:  product.EnvName,
		}
		renderSet, err := commonrepo.NewRenderSetColl().Find(findOption)
		if err != nil {
			log.Errorf("can't find renderSet for %s in env %s, error: %w", product.ProductName, product.EnvName, err)
			return nil
		}
		globalVariableMap := map[string]*commontypes.GlobalVariableKV{}
		for _, kv := range renderSet.GlobalVariables {
			globalVariableMap[kv.Key] = kv
		}

		// validate global variables
		/*
			for _, deploySvc := range j.spec.Services {
				updatedVariableConfigs := []*commonmodels.DeplopyVariableConfig{}
				for _, varConfig := range deploySvc.VariableConfigs {
					if varConfig.UseGlobalVariable {
						if _, ok := globalVariableMap[varConfig.VariableKey]; !ok {
							continue
						}
						updatedVariableConfigs = append(updatedVariableConfigs, varConfig)
					}
				}
				deploySvc.VariableConfigs = updatedVariableConfigs
			}
		*/

		tmplSvcMap, err := repository.GetMaxRevisionsServicesMap(product.ProductName, product.Production)
		if err != nil {
			return fmt.Errorf("failed to get max revision services map, productName %s, isProduction %v, err: %w", product.ProductName, product.Production, err)
		}

		// update image name
		for _, svc := range j.spec.ServiceAndImages {
			productSvc := product.GetServiceMap()[svc.ServiceName]
			if productSvc == nil {
				// get from template
				tmplSvc := tmplSvcMap[svc.ServiceName]
				if tmplSvc == nil {
					return fmt.Errorf("service %s not found in template service, productName %s, isProduction %v", svc.ServiceName, product.ProductName, product.Production)
				}
				for _, container := range tmplSvc.Containers {
					if container.Name == svc.ServiceModule {
						svc.ImageName = commonutil.ExtractImageName(container.Image)
						break
					}
				}
			} else {
				// get from env
				for _, container := range productSvc.Containers {
					if container.Name == svc.ServiceModule {
						svc.ImageName = commonutil.ExtractImageName(container.Image)
						break
					}
				}
			}

			if svc.ImageName == "" {
				log.Errorf("service: %s module: %s not found in env and tmplSvc", svc.ServiceName, svc.ServiceModule)
				svc.ImageName = svc.ServiceModule
			}
		}

	}

	return nil
}

func (j *DeployJob) MergeArgs(args *commonmodels.Job) error {
	if j.job.Name == args.Name && j.job.JobType == args.JobType {
		j.spec = &commonmodels.ZadigDeployJobSpec{}
		if err := commonmodels.IToi(j.job.Spec, j.spec); err != nil {
			return err
		}
		j.job.Spec = j.spec
		argsSpec := &commonmodels.ZadigDeployJobSpec{}
		if err := commonmodels.IToi(args.Spec, argsSpec); err != nil {
			return err
		}
		j.spec.Env = argsSpec.Env
		j.spec.Services = argsSpec.Services
		if j.spec.Source == config.SourceRuntime {
			j.spec.ServiceAndImages = argsSpec.ServiceAndImages
		}

		j.job.Spec = j.spec
	}
	return nil
}

func (j *DeployJob) ToJobs(taskID int64) ([]*commonmodels.JobTask, error) {
	resp := []*commonmodels.JobTask{}
	j.spec = &commonmodels.ZadigDeployJobSpec{}

	if err := commonmodels.IToi(j.job.Spec, j.spec); err != nil {
		return resp, err
	}
	j.setDefaultDeployContent()
	j.job.Spec = j.spec

	envName := strings.ReplaceAll(j.spec.Env, setting.FixedValueMark, "")
	product, err := commonrepo.NewProductColl().Find(&commonrepo.ProductFindOptions{Name: j.workflow.Project, EnvName: envName})
	if err != nil {
		return resp, fmt.Errorf("env %s not exists", envName)
	}

	project, err := templaterepo.NewProductColl().Find(j.workflow.Project)
	if err != nil {
		return resp, fmt.Errorf("failed to find project %s, err: %v", j.workflow.Project, err)
	}

	productServiceMap := product.GetServiceMap()

	if project.ProductFeature != nil && project.ProductFeature.CreateEnvType == setting.SourceFromExternal {
		productServices, err := commonrepo.NewServiceColl().ListExternalWorkloadsBy(j.workflow.Project, envName)
		if err != nil {
			return resp, fmt.Errorf("failed to list external workload, err: %v", err)
		}
		for _, service := range productServices {
			productServiceMap[service.ServiceName] = &commonmodels.ProductService{
				ServiceName: service.ServiceName,
				Containers:  service.Containers,
			}
		}
		servicesInExternalEnv, _ := commonrepo.NewServicesInExternalEnvColl().List(&commonrepo.ServicesInExternalEnvArgs{
			ProductName: j.workflow.Project,
			EnvName:     envName,
		})
		for _, service := range servicesInExternalEnv {
			productServiceMap[service.ServiceName] = &commonmodels.ProductService{
				ServiceName: service.ServiceName,
			}
		}
	}

	usedRenderset, err := commonrepo.NewRenderSetColl().Find(&commonrepo.RenderSetFindOption{
		ProductTmpl: product.ProductName,
		EnvName:     product.EnvName,
		IsDefault:   false,
		Revision:    product.Render.Revision,
		Name:        product.Render.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find renderset for %s/%s, err: %w", product.ProductName, product.EnvName, err)
	}

	// get deploy info from previous build job
	if j.spec.Source == config.SourceFromJob {
		// adapt to the front end, use the direct quoted job name
		if j.spec.OriginJobName != "" {
			j.spec.JobName = j.spec.OriginJobName
		}
		targets, err := j.getOriginReferedJobTargets(j.spec.JobName)
		if err != nil {
			return resp, fmt.Errorf("get origin refered job: %s targets failed, err: %v", j.spec.JobName, err)
		}
		// clear service and image list to prevent old data from remaining
		j.spec.ServiceAndImages = targets
	}

	serviceMap := map[string]*commonmodels.DeployService{}
	for _, service := range j.spec.Services {
		serviceMap[service.ServiceName] = service
	}

	templateProduct, err := templaterepo.NewProductColl().Find(j.workflow.Project)
	if err != nil {
		return resp, fmt.Errorf("cannot find product %s: %w", j.workflow.Project, err)
	}
	timeout := templateProduct.Timeout * 60

	if j.spec.DeployType == setting.K8SDeployType {
		deployServiceMap := map[string][]*commonmodels.ServiceAndImage{}
		for _, deploy := range j.spec.ServiceAndImages {
			deployServiceMap[deploy.ServiceName] = append(deployServiceMap[deploy.ServiceName], deploy)
		}
		for serviceName, deploys := range deployServiceMap {
			jobTaskSpec := &commonmodels.JobTaskDeploySpec{
				Env:                envName,
				SkipCheckRunStatus: j.spec.SkipCheckRunStatus,
				ServiceName:        serviceName,
				ServiceType:        setting.K8SDeployType,
				CreateEnvType:      project.ProductFeature.CreateEnvType,
				ClusterID:          product.ClusterID,
				Production:         j.spec.Production,
				DeployContents:     j.spec.DeployContents,
				Timeout:            timeout,
			}

			svcRenderVarMap := map[string]*commontypes.RenderVariableKV{}
			serviceRender := usedRenderset.GetServiceRenderMap()[serviceName]
			if serviceRender != nil {
				for _, varKV := range serviceRender.OverrideYaml.RenderVariableKVs {
					svcRenderVarMap[varKV.Key] = varKV
				}
			}

			for _, deploy := range deploys {
				// if external env, check service exists
				if project.ProductFeature.CreateEnvType == "external" {
					if err := checkServiceExsistsInEnv(productServiceMap, serviceName, envName); err != nil {
						return resp, err
					}
				}
				jobTaskSpec.ServiceAndImages = append(jobTaskSpec.ServiceAndImages, &commonmodels.DeployServiceModule{
					Image:         deploy.Image,
					ImageName:     deploy.ImageName,
					ServiceModule: deploy.ServiceModule,
				})
			}
			if project.ProductFeature.CreateEnvType != "external" {
				jobTaskSpec.DeployContents = j.spec.DeployContents
				jobTaskSpec.Production = j.spec.Production
				service := serviceMap[serviceName]
				if service != nil {
					jobTaskSpec.UpdateConfig = service.UpdateConfig
					jobTaskSpec.VariableConfigs = service.VariableConfigs
					if service.UpdateConfig {
						jobTaskSpec.VariableKVs = service.LatestVariableKVs
					} else {
						jobTaskSpec.VariableKVs = service.VariableKVs
					}

					// filter variables that used global variable
					filterdKV := []*commontypes.RenderVariableKV{}
					for _, jobKV := range jobTaskSpec.VariableKVs {
						svcKV, ok := svcRenderVarMap[jobKV.Key]
						if !ok {
							// deploy new variable
							filterdKV = append(filterdKV, jobKV)
							continue
						}
						// deploy existed variable
						if svcKV.UseGlobalVariable {
							continue
						}
						filterdKV = append(filterdKV, jobKV)
					}
					jobTaskSpec.VariableKVs = filterdKV
				}
				// if only deploy images, clear keyvals
				if onlyDeployImage(j.spec.DeployContents) {
					jobTaskSpec.VariableConfigs = []*commonmodels.DeplopyVariableConfig{}
					jobTaskSpec.VariableKVs = []*commontypes.RenderVariableKV{}
				}
			}
			jobTask := &commonmodels.JobTask{
				Name: jobNameFormat(serviceName + "-" + j.job.Name),
				Key:  strings.Join([]string{j.job.Name, serviceName}, "."),
				JobInfo: map[string]string{
					JobNameKey:     j.job.Name,
					"service_name": serviceName,
				},
				JobType: string(config.JobZadigDeploy),
				Spec:    jobTaskSpec,
			}
			resp = append(resp, jobTask)
		}
	}
	if j.spec.DeployType == setting.HelmDeployType {
		deployServiceMap := map[string][]*commonmodels.ServiceAndImage{}
		for _, deploy := range j.spec.ServiceAndImages {
			deployServiceMap[deploy.ServiceName] = append(deployServiceMap[deploy.ServiceName], deploy)
		}
		for serviceName, deploys := range deployServiceMap {
			var serviceRevision int64
			if pSvc, ok := productServiceMap[serviceName]; ok {
				serviceRevision = pSvc.Revision
			}

			revisionSvc, err := commonrepo.NewServiceColl().Find(&commonrepo.ServiceFindOption{
				ServiceName: serviceName,
				Revision:    serviceRevision,
				ProductName: product.ProductName,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to find service: %s with revision: %d, err: %s", serviceName, serviceRevision, err)
			}
			releaseName := util.GeneReleaseName(revisionSvc.GetReleaseNaming(), product.ProductName, product.Namespace, product.EnvName, serviceName)

			jobTaskSpec := &commonmodels.JobTaskHelmDeploySpec{
				Env:                envName,
				ServiceName:        serviceName,
				DeployContents:     j.spec.DeployContents,
				SkipCheckRunStatus: j.spec.SkipCheckRunStatus,
				ServiceType:        setting.HelmDeployType,
				ClusterID:          product.ClusterID,
				ReleaseName:        releaseName,
				Timeout:            timeout,
			}
			for _, deploy := range deploys {
				service := serviceMap[serviceName]
				if service != nil {
					jobTaskSpec.UpdateConfig = service.UpdateConfig
					jobTaskSpec.KeyVals = service.KeyVals
				}

				if err := checkServiceExsistsInEnv(productServiceMap, serviceName, envName); err != nil {
					return resp, err
				}
				jobTaskSpec.ImageAndModules = append(jobTaskSpec.ImageAndModules, &commonmodels.ImageAndServiceModule{
					ServiceModule: deploy.ServiceModule,
					Image:         deploy.Image,
				})
			}
			jobTask := &commonmodels.JobTask{
				Name: jobNameFormat(serviceName + "-" + j.job.Name),
				Key:  strings.Join([]string{j.job.Name, serviceName}, "."),
				JobInfo: map[string]string{
					JobNameKey:     j.job.Name,
					"service_name": serviceName,
				},
				JobType: string(config.JobZadigHelmDeploy),
				Spec:    jobTaskSpec,
			}
			resp = append(resp, jobTask)
		}
	}

	j.job.Spec = j.spec
	return resp, nil
}

func onlyDeployImage(deployContents []config.DeployContent) bool {
	return slices.Contains(deployContents, config.DeployImage) && len(deployContents) == 1
}

func checkServiceExsistsInEnv(serviceMap map[string]*commonmodels.ProductService, serviceName, env string) error {
	if _, ok := serviceMap[serviceName]; !ok {
		return fmt.Errorf("service %s not exists in env %s", serviceName, env)
	}
	return nil
}

func (j *DeployJob) LintJob() error {
	j.spec = &commonmodels.ZadigDeployJobSpec{}
	if err := commonmodels.IToiYaml(j.job.Spec, j.spec); err != nil {
		return err
	}
	if j.spec.Source != config.SourceFromJob {
		return nil
	}
	jobRankMap := getJobRankMap(j.workflow.Stages)
	buildJobRank, ok := jobRankMap[j.spec.JobName]
	if !ok || buildJobRank >= jobRankMap[j.job.Name] {
		return fmt.Errorf("can not quote job %s in job %s", j.spec.JobName, j.job.Name)
	}
	return nil
}
