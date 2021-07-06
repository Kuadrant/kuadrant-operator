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
	"istio.io/api/networking/v1alpha3"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VirtualServiceFactory struct {
	ObjectName string
	Namespace  string
	Hosts      []string
	HTTPRoutes []*v1alpha3.HTTPRoute
}

func (v *VirtualServiceFactory) VirtualService() *istio.VirtualService {
	return &istio.VirtualService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VirtualService",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.ObjectName,
			Namespace: v.Namespace,
		},
		Spec: v1alpha3.VirtualService{
			Gateways: []string{"kuadrant-gateway"},
			Hosts:    v.Hosts,
			Http:     v.HTTPRoutes,
		},
	}
}
