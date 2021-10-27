/*
Copyright 2021 The KodeRover Authors.

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
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models/template"
	commonrepo "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/mongodb"
	commonservice "github.com/koderover/zadig/pkg/microservice/aslan/core/common/service"
)

type DefaultValuesResp struct {
	DefaultValues string `json:"defaultValues"`
}

func GetRenderCharts(productName, envName, serviceName string, log *zap.SugaredLogger) ([]*commonservice.RenderChartArg, error) {

	renderSetName := commonservice.GetProductEnvNamespace(envName, productName, "")

	opt := &commonrepo.RenderSetFindOption{
		Name: renderSetName,
	}
	rendersetObj, existed, err := commonrepo.NewRenderSetColl().FindRenderSet(opt)
	if err != nil {
		return nil, err
	}

	if !existed {
		return nil, nil
	}

	ret := make([]*commonservice.RenderChartArg, 0)

	matchedRenderChartModels := make([]*template.RenderChart, 0)
	if len(serviceName) == 0 {
		matchedRenderChartModels = rendersetObj.ChartInfos
	} else {
		serverList := strings.Split(serviceName, ",")
		stringSet := sets.NewString(serverList...)
		for _, singleChart := range rendersetObj.ChartInfos {
			if !stringSet.Has(singleChart.ServiceName) {
				continue
			}
			matchedRenderChartModels = append(matchedRenderChartModels, singleChart)
		}
	}

	for _, singleChart := range matchedRenderChartModels {
		rcaObj := new(commonservice.RenderChartArg)
		rcaObj.LoadFromRenderChartModel(singleChart)
		rcaObj.EnvName = envName
		ret = append(ret, rcaObj)
	}
	return ret, nil
}

func GetDefaultValues(productName, envName string, log *zap.SugaredLogger) (*DefaultValuesResp, error) {
	ret := &DefaultValuesResp{}

	productInfo, err := commonrepo.NewProductColl().Find(&commonrepo.ProductFindOptions{
		Name:    productName,
		EnvName: envName,
	})
	if err == mongo.ErrNoDocuments {
		return ret, nil
	}
	if err != nil {
		log.Errorf("faild to query product info, productName %s envName %s err %s", productName, envName, err)
		return nil, fmt.Errorf("faild to query product info, productName %s envName %s", productName, envName)
	}

	if productInfo.Render == nil {
		return nil, fmt.Errorf("invalid product, nil render data")
	}

	opt := &commonrepo.RenderSetFindOption{
		Name:     productInfo.Render.Name,
		Revision: productInfo.Render.Revision,
	}
	rendersetObj, existed, err := commonrepo.NewRenderSetColl().FindRenderSet(opt)
	if err != nil {
		log.Errorf("faild to query renderset info, envName %s err %s", productInfo.Render.Name, err)
		return nil, err
	}
	if !existed {
		return ret, nil
	}
	ret.DefaultValues = rendersetObj.DefaultValues
	return ret, nil
}
