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
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha1 "sigs.k8s.io/gateway-api/apis/v1alpha1"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

type APIFactory struct {
	Name                 string
	Namespace            string
	DestinationSchema    string
	DestinationName      string
	DestinationNamespace string
	DestinationPort      *int32
	OASContent           *string
	HTTPPathMatch        *gatewayapiv1alpha1.HTTPPathMatch
}

func (a *APIFactory) API() *networkingv1beta1.API {
	return &networkingv1beta1.API{
		TypeMeta: metav1.TypeMeta{
			Kind:       networkingv1beta1.APIKind,
			APIVersion: networkingv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      a.Name,
			Namespace: a.Namespace,
		},
		Spec: networkingv1beta1.APISpec{
			Destination: networkingv1beta1.Destination{
				Schema: a.DestinationSchema,
				ServiceReference: v1.ServiceReference{
					Namespace: a.DestinationNamespace,
					Name:      a.DestinationName,
					Port:      a.DestinationPort,
					Path:      nil,
				},
			},
			Mappings: networkingv1beta1.APIMappings{
				OAS:           a.OASContent,
				HTTPPathMatch: a.HTTPPathMatch,
			},
		},
	}
}
