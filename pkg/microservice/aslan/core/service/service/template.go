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

package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	commonrepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb"
	"github.com/koderover/zadig/pkg/microservice/aslan/core/common/service/notify"
	commontypes "github.com/koderover/zadig/pkg/microservice/aslan/core/common/types"
	commonutil "github.com/koderover/zadig/pkg/microservice/aslan/core/common/util"
	"github.com/koderover/zadig/pkg/setting"
	"github.com/koderover/zadig/pkg/tool/log"
)

func geneCreateFromDetail(templateId string, variableYaml string) *commonmodels.CreateFromYamlTemplate {
	//vs := make([]*commonmodels.Variable, 0, len(variables))
	//for _, kv := range variables {
	//	vs = append(vs, &commonmodels.Variable{
	//		Key:   kv.Key,
	//		Value: kv.Value,
	//	})
	//}
	return &commonmodels.CreateFromYamlTemplate{
		TemplateID: templateId,
		//Variables:    vs,
		VariableYaml: variableYaml,
	}
}

func OpenAPILoadServiceFromYamlTemplate(username string, req *OpenAPILoadServiceFromYamlTemplateReq, force bool, logger *zap.SugaredLogger) error {
	template, err := commonrepo.NewYamlTemplateColl().GetByName(req.TemplateName)
	if err != nil {
		logger.Errorf("Failed to find template of name: %s, err: %w", req.TemplateName, err)
		return err
	}

	mergedYaml, mergedKVs, err := commonutil.MergeServiceVariableKVsAndKVInput(template.ServiceVariableKVs, req.VariableYaml)
	if err != nil {
		return fmt.Errorf("failed to merge variable yaml, err: %w", err)
	}

	loadArgs := &LoadServiceFromYamlTemplateReq{
		ProjectName:        req.ProjectKey,
		ServiceName:        req.ServiceName,
		TemplateID:         template.ID.Hex(),
		AutoSync:           req.AutoSync,
		VariableYaml:       mergedYaml,
		ServiceVariableKVs: mergedKVs,
	}

	return LoadServiceFromYamlTemplate(username, loadArgs, force, logger)
}

func LoadServiceFromYamlTemplate(username string, req *LoadServiceFromYamlTemplateReq, force bool, logger *zap.SugaredLogger) error {
	projectName, serviceName, templateID, autoSync := req.ProjectName, req.ServiceName, req.TemplateID, req.AutoSync
	template, err := commonrepo.NewYamlTemplateColl().GetById(templateID)
	if err != nil {
		logger.Errorf("Failed to find template of ID: %s, the error is: %s", templateID, err)
		return err
	}

	renderedYaml := renderSystemVars(template.Content, projectName, serviceName)
	fullRenderedYaml, err := commonutil.RenderK8sSvcYamlStrict(template.Content, projectName, serviceName, template.VariableYaml, req.VariableYaml)
	if err != nil {
		return err
	}

	service := &commonmodels.Service{
		ServiceName:        serviceName,
		Type:               setting.K8SDeployType,
		ProductName:        projectName,
		Source:             setting.ServiceSourceTemplate,
		Yaml:               renderedYaml,
		RenderedYaml:       fullRenderedYaml,
		Visibility:         setting.PrivateVisibility,
		TemplateID:         templateID,
		AutoSync:           autoSync,
		VariableYaml:       req.VariableYaml,
		ServiceVariableKVs: req.ServiceVariableKVs,
		CreateFrom:         geneCreateFromDetail(templateID, req.VariableYaml),
	}
	_, err = CreateServiceTemplate(username, service, force, logger)
	if err != nil {
		logger.Errorf("Failed to create service template from template ID: %s, the error is: %s", templateID, err)
	}
	return err
}

func ReloadServiceFromYamlTemplate(username string, req *LoadServiceFromYamlTemplateReq, logger *zap.SugaredLogger) error {
	projectName, serviceName, templateID, autoSync := req.ProjectName, req.ServiceName, req.TemplateID, req.AutoSync
	service, err := commonrepo.NewServiceColl().Find(&commonrepo.ServiceFindOption{
		ServiceName: serviceName,
		ProductName: projectName,
	})
	if err != nil {
		logger.Errorf("Cannot find service of name [%s] from project [%s], the error is: %s", serviceName, projectName, err)
		return err
	}
	if service.Source != setting.ServiceSourceTemplate {
		return errors.New("service is not created from template")
	}
	if service.TemplateID == "" {
		return fmt.Errorf("failed to find template id for service: %s", serviceName)
	}
	template, err := commonrepo.NewYamlTemplateColl().GetById(templateID)
	if err != nil {
		logger.Errorf("Failed to find template of ID: %s, the error is: %s", templateID, err)
		return err
	}

	service.AutoSync = autoSync
	service.TemplateID = templateID
	return reloadServiceFromYamlTemplateImpl(username, projectName, template, service, req.VariableYaml, req.ServiceVariableKVs)
}

func PreviewServiceFromYamlTemplate(req *LoadServiceFromYamlTemplateReq, logger *zap.SugaredLogger) (string, error) {
	yamlTemplate, err := commonrepo.NewYamlTemplateColl().GetById(req.TemplateID)
	if err != nil {
		return "", fmt.Errorf("failed to preview service, err: %s", err)
	}

	//templateVariableYaml, err := commomtemplate.GetTemplateVariableYaml(yamlTemplate.Variables, yamlTemplate.VariableYaml)
	//if err != nil {
	//	return "", fmt.Errorf("failed to get variable yaml from yaml template")
	//}
	//templateVariableYaml := yamlTemplate.VariableYaml
	return commonutil.RenderK8sSvcYaml(yamlTemplate.Content, req.ProjectName, req.ServiceName, yamlTemplate.VariableYaml, req.VariableYaml)
}

func renderSystemVars(originYaml, productName, serviceName string) string {
	originYaml = strings.ReplaceAll(originYaml, setting.TemplateVariableProduct, productName)
	originYaml = strings.ReplaceAll(originYaml, setting.TemplateVariableService, serviceName)
	return originYaml
}

// SyncServiceFromTemplate syncs services from (yaml|chart)template
func SyncServiceFromTemplate(userName, source, templateId, templateName string, logger *zap.SugaredLogger) error {
	if source == setting.ServiceSourceTemplate {
		return syncServicesFromYamlTemplate(userName, templateId, logger)
	} else {
		return syncServicesFromChartTemplate(userName, templateName, logger)
	}
}

func syncServicesFromYamlTemplate(userName, templateId string, logger *zap.SugaredLogger) error {
	serviceList, err := commonrepo.NewServiceColl().GetYamlTemplateReference(templateId)
	if err != nil {
		return err
	}
	servicesByProject := make(map[string][]*commonmodels.Service)
	for _, service := range serviceList {
		if !service.AutoSync {
			continue
		}
		servicesByProject[service.ProductName] = append(servicesByProject[service.ProductName], service)
	}
	yamlTemplate, err := commonrepo.NewYamlTemplateColl().GetById(templateId)
	if err != nil {
		return fmt.Errorf("failed to find yaml template: %s, err: %s", templateId, err)
	}
	for _, services := range servicesByProject {
		go func(pServices []*commonmodels.Service) {
			for _, service := range pServices {
				err := reloadServiceFromYamlTemplate(userName, service.ProductName, yamlTemplate, service)
				if err != nil {
					logger.Error(err)
					title := fmt.Sprintf("从模板更新 [%s] 的 [%s] 服务失败", service.ProductName, service.ServiceName)
					notify.SendErrorMessage(userName, title, "", err, logger)
				}
			}
		}(services)
	}
	return nil
}

func syncServicesFromChartTemplate(userName, templateName string, logger *zap.SugaredLogger) error {
	chartTemplate, err := prepareChartTemplateData(templateName, log.SugaredLogger())
	if err != nil {
		return err
	}

	serviceList, err := commonrepo.NewServiceColl().ListMaxRevisionServicesByChartTemplate(templateName)
	if err != nil {
		return err
	}
	servicesByProject := make(map[string][]*commonmodels.Service)
	for _, service := range serviceList {
		if !service.AutoSync {
			continue
		}
		servicesByProject[service.ProductName] = append(servicesByProject[service.ProductName], service)
	}

	for _, services := range servicesByProject {
		go func(pService []*commonmodels.Service) {
			for _, service := range pService {
				err := reloadServiceFromChartTemplate(service, chartTemplate)
				if err != nil {
					logger.Errorf("failed to reload service %s/%s from chart template, err: %s", service.ProductName, service.ServiceName, err)
					title := fmt.Sprintf("从模板更新 [%s] 的 [%s] 服务失败", service.ProductName, service.ServiceName)
					notify.SendErrorMessage(userName, title, "", err, logger)
				}
			}
		}(services)
	}
	return nil
}

func reloadServiceFromChartTemplate(service *commonmodels.Service, chartTemplate *ChartTemplateData) error {
	variable, customYaml, err := buildChartTemplateVariables(service, chartTemplate.TemplateData)
	if err != nil {
		return err
	}

	templateArgs := &CreateFromChartTemplate{
		TemplateName: chartTemplate.TemplateName,
		ValuesYAML:   customYaml,
		Variables:    variable,
	}
	args := &HelmServiceCreationArgs{
		HelmLoadSource: HelmLoadSource{},
		Name:           service.ServiceName,
		CreatedBy:      "system",
		ValuesData:     nil,
		CreationDetail: service.CreateFrom,
		AutoSync:       service.AutoSync,
	}
	ret, err := createOrUpdateHelmServiceFromChartTemplate(templateArgs, chartTemplate, service.ProductName, args, true, log.SugaredLogger())
	if err != nil {
		return err
	}
	if len(ret.FailedServices) == 1 {
		return errors.New(ret.FailedServices[0].Error)
	}
	return nil
}

func buildYamlTemplateVariables(service *commonmodels.Service, template *commonmodels.YamlTemplate) (string, []*commontypes.ServiceVariableKV, error) {
	variableYaml, serviceVariableKVs, err := commontypes.MergeServiceVariableKVsIfNotExist(service.ServiceVariableKVs, template.ServiceVariableKVs)
	if err != nil {
		return "", nil, fmt.Errorf("failed to merge service variable kvs")
	}

	return variableYaml, serviceVariableKVs, nil
}

func buildChartTemplateVariables(service *commonmodels.Service, template *commonmodels.Chart) ([]*Variable, string, error) {
	variables := make([]*Variable, 0)
	variableMap := make(map[string]*Variable)

	for _, v := range template.ChartVariables {
		kv := &Variable{
			Key:   v.Key,
			Value: v.Value,
		}
		variableMap[v.Key] = kv
		variables = append(variables, kv)
	}

	customYaml := ""
	if service.CreateFrom != nil {
		bs, err := json.Marshal(service.CreateFrom)
		if err != nil {
			log.Errorf("failed to marshal creation data: %s", err)
			return variables, "", err
		}
		creation := &commonmodels.CreateFromChartTemplate{}
		err = json.Unmarshal(bs, creation)
		if err != nil {
			log.Errorf("failed to unmarshal creation data: %s", err)
			return variables, "", err
		}
		for _, kv := range creation.Variables {
			if tkv, ok := variableMap[kv.Key]; ok {
				tkv.Value = kv.Value
			}
		}
		if creation.YamlData != nil {
			customYaml = creation.YamlData.YamlContent
		}
		vbs := make([]*commonmodels.Variable, 0)
		for _, kv := range variables {
			vbs = append(vbs, &commonmodels.Variable{
				Key:   kv.Key,
				Value: kv.Value,
			})
		}
		creation.Variables = vbs
		service.CreateFrom = creation
	}
	return variables, customYaml, nil
}

func reloadServiceFromYamlTemplateImpl(userName, projectName string, template *commonmodels.YamlTemplate, service *commonmodels.Service, variableYaml string, serviceVariableKVs []*commontypes.ServiceVariableKV) error {
	renderedYaml := renderSystemVars(template.Content, projectName, service.ServiceName)
	fullRenderedYaml, err := commonutil.RenderK8sSvcYamlStrict(template.Content, projectName, service.ServiceName, template.VariableYaml, variableYaml)
	if err != nil {
		return err
	}

	svc := &commonmodels.Service{
		ServiceName:        service.ServiceName,
		Type:               setting.K8SDeployType,
		ProductName:        projectName,
		Source:             setting.ServiceSourceTemplate,
		Yaml:               renderedYaml,
		RenderedYaml:       fullRenderedYaml,
		Visibility:         setting.PrivateVisibility,
		VariableYaml:       variableYaml,
		ServiceVariableKVs: serviceVariableKVs,
		TemplateID:         service.TemplateID,
		CreateFrom:         geneCreateFromDetail(service.TemplateID, variableYaml),
		AutoSync:           service.AutoSync,
	}
	_, err = CreateServiceTemplate(userName, svc, true, log.SugaredLogger())
	if err != nil {
		return fmt.Errorf("failed to reload service template from template ID: %s, error : %s", service.TemplateID, err)
	}
	return nil
}

func reloadServiceFromYamlTemplate(userName, projectName string, template *commonmodels.YamlTemplate, service *commonmodels.Service) error {
	// merge service variable and yaml variable
	variableYaml, kvs, err := buildYamlTemplateVariables(service, template)
	if err != nil {
		return err
	}

	return reloadServiceFromYamlTemplateImpl(userName, projectName, template, service, variableYaml, kvs)
}
