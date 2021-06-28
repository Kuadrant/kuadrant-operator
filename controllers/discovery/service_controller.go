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

package discovery

import (
	"context"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/types"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kuadrantDiscoveryLabel = "discovery.kuadrant.io/enabled"

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=services;configmaps,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Service object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("service", req.NamespacedName)
	service := corev1.Service{}
	err := r.Client.Get(ctx, req.NamespacedName, &service)

	if err != nil && errors.IsNotFound(err) {
		log.Info("resource not found. handling possible deletion.")
		//TODO(jmprusi): Handle deletion of the Service. I guess using an OwnerReference could work for now.
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciling", service.GetNamespace(), service.GetName())

	// Let's generate the API object based on the Service annotations
	//TODO(jmprusi): If the user changes the api-name annotation, there will be a dangling API object. Fix this.
	desiredAPI, err := r.APIFromAnnotations(ctx, service)
	if err != nil {
		//TODO(jmprusi): If annotations are incorrect, we need to push that back to the user.
		return ctrl.Result{}, err
	}

	currentAPI := v1beta1.API{}
	err = r.Client.Get(ctx, client.ObjectKeyFromObject(desiredAPI), &currentAPI)
	if err != nil && errors.IsNotFound(err) {
		//TODO(jmprusi): Set owner reference!
		err := r.Client.Create(ctx, desiredAPI)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}
	// TODO(jmprusi): Compare.. Update the API etc.
	desiredAPI.ResourceVersion = currentAPI.ResourceVersion
	err = r.Client.Update(ctx, desiredAPI)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) APIFromAnnotations(ctx context.Context, service corev1.Service) (*v1beta1.API, error) {

	//Supported Annotations for now:
	//discovery.kuadrant.io/scheme: "https"
	//discovery.kuadrant.io/api-name: "dogs-api"
	//discovery.kuadrant.io/tag: "production"
	//discovery.kuadrant.io/port: 80 / name
	//discovery.kuadrant.io/oas-configmap: configmapName
	// Related to https://github.com/Kuadrant/kuadrant-controller/issues/28

	var oasConfigmapName, apiName, scheme, tagLabel, port string
	var ok bool

	// Let's do some basic validation and setting defaults.
	if oasConfigmapName, ok = service.Annotations["discovery.kuadrant.io/oas-configmap"]; !ok {
		return nil, fmt.Errorf("discovery.kuadrant.io/oas-configmap annotation is missing or invalid")
	}
	if scheme, ok = service.Annotations["discovery.kuadrant.io/scheme"]; !ok {
		scheme = "http"
	}
	if apiName, ok = service.Annotations["discovery.kuadrant.io/api-name"]; !ok {
		apiName = service.GetName()
	}
	if tagLabel, ok = service.Annotations["discovery.kuadrant.io/tag"]; !ok {
		return nil, fmt.Errorf("discovery.kuadrant.io/tagLabel annotation is missing or invalid")
	}

	Tag := v1beta1.Tag{
		Name: tagLabel,
		Destination: v1beta1.Destination{
			Schema: scheme,
			ServiceReference: &v1.ServiceReference{
				Namespace: service.Namespace,
				Name:      service.Name,
				Path:      nil,
			},
		},
	}

	// Let's find out the port, this annotation is a little bit more tricky.
	if port, ok = service.Annotations["discovery.kuadrant.io/port"]; ok {
		// check if the port is a number already.
		if num, err := strconv.Atoi(port); err == nil {
			int32num := int32(num)
			Tag.Destination.Port = &int32num
		} else {
			// As the port is name, resolv the port from the service
			for _, p := range service.Spec.Ports {
				if p.Name == port {
					Tag.Destination.Port = &p.Port
					break
				}
			}
		}
	} else {
		// As the annotation has not been set, let's check if the service has only on port, if that's the case,
		//default to it.
		if len(service.Spec.Ports) == 1 {
			Tag.Destination.Port = &service.Spec.Ports[0].Port
		}
	}
	// If we reach this point and the Port is still nil, this means bad news
	if Tag.Destination.Port == nil {
		return nil, fmt.Errorf("discovery.kuadrant.io/port is missing or invalid")
	}

	// Ok, let's get the configmap referenced by the annotation.
	oasConfigmap := corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      oasConfigmapName,
	}, &oasConfigmap)
	if err != nil {
		return nil, err
	}
	// TODO(jmprusi): The openapispec.yaml data entry in the configmap is hardcoded, review this.
	if _, ok := oasConfigmap.Data["openapi.yaml"]; !ok {
		return nil, fmt.Errorf("oas configmap is missing the openapispec.yaml entry")
	}

	Tag.APIDefinition = v1beta1.APIDefinition{
		OAS: oasConfigmap.Data["openapi.yaml"],
	}

	// TODO(jmprusi): We will create the API object in the same namespace as the service to simplify the deletion,
	// review this later.
	desiredAPI := v1beta1.API{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiName,
			Namespace: service.Namespace,
		},
		Spec: v1beta1.APISpec{
			TAGs: nil,
		},
	}

	desiredAPI.Spec.TAGs = append(desiredAPI.Spec.TAGs, Tag)
	return &desiredAPI, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(onlyLabeledServices()).
		For(&corev1.Service{}).
		Complete(r)
}

func onlyLabeledServices() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Lets filter for only Services that have the kuadrant label and are enabled.
			if val, ok := e.Object.GetLabels()[kuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// In case the update object had the kuadrant label set to true, we need to reconcile it.
			if val, ok := e.ObjectOld.GetLabels()[kuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			// In case that service gets update by adding the label, and set to true, we need to reconcile it.
			if val, ok := e.ObjectNew.GetLabels()[kuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			_, ok := e.Object.GetLabels()[kuadrantDiscoveryLabel]
			return ok
		},
	}
}
