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
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

// APIProductAPIEventMapper is an EventHandler that maps API object events
// to APIProduct events.
type APIProductAPIEventMapper struct {
	K8sClient client.Client
	Logger    logr.Logger
}

func (a *APIProductAPIEventMapper) Map(obj client.Object) []reconcile.Request {
	a.Logger.V(1).Info("Processing object", "Name", obj.GetName(), "Namespace", obj.GetNamespace())

	apiProductList := &networkingv1beta1.APIProductList{}
	// all namespaces
	// filter by API UID
	err := a.K8sClient.List(context.Background(), apiProductList, client.HasLabels{apiLabelKey(string(obj.GetUID()))})
	if err != nil {
		a.Logger.Error(err, "reading apiproduct list")
		return nil
	}

	requests := []reconcile.Request{}
	for idx := range apiProductList.Items {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      apiProductList.Items[idx].GetName(),
			Namespace: apiProductList.Items[idx].GetNamespace(),
		}})
	}

	return requests
}
