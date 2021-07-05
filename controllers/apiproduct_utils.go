/*
Copyright 2021 Red Hat, Inc.

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

package controllers

import (
	"fmt"
	"reflect"
	"strings"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

const (
	KuadrantAPILabelPrefix = "api.kuadrant.io/"
	KuadrantAPILabelValue  = "true"
)

func apiLabelKey(uid string) string {
	return fmt.Sprintf("%s%s", KuadrantAPILabelPrefix, uid)
}

func replaceAPILabels(apip *networkingv1beta1.APIProduct, desiredAPIUIDs []string) bool {
	existingLabels := apip.GetLabels()

	if existingLabels == nil {
		existingLabels = map[string]string{}
	}

	existingAPILabels := map[string]string{}

	// existing API UIDs not included in desiredAPIUIDs are deleted
	for k := range existingLabels {
		if strings.HasPrefix(k, KuadrantAPILabelPrefix) {
			existingAPILabels[k] = KuadrantAPILabelValue
			// it is safe to remove keys while looping in range
			delete(existingLabels, k)
		}
	}

	desiredAPILabels := map[string]string{}
	for _, uid := range desiredAPIUIDs {
		desiredAPILabels[apiLabelKey(uid)] = KuadrantAPILabelValue
		existingLabels[apiLabelKey(uid)] = KuadrantAPILabelValue
	}

	apip.SetLabels(existingLabels)

	return !reflect.DeepEqual(existingAPILabels, desiredAPILabels)
}
