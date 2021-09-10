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

package istioprovider

import (
	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type EnvoyFilterFactory struct {
	ObjectName string
	Namespace  string
	Patches    []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
}

func (v *EnvoyFilterFactory) EnvoyFilter() *istionetworkingv1alpha3.EnvoyFilter {
	return &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.ObjectName,
			Namespace: v.Namespace,
		},
		Spec: istioapiv1alpha3.EnvoyFilter{
			WorkloadSelector: &istioapiv1alpha3.WorkloadSelector{
				Labels: map[string]string{"istio": "kuadrant-system"},
			},
			ConfigPatches: v.Patches,
		},
	}
}
