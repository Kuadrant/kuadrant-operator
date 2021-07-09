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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayapiv1alpha1 "sigs.k8s.io/gateway-api/apis/v1alpha1"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	KuadrantDiscoveryLabel                   = "discovery.kuadrant.io/enabled"
	KuadrantDiscoveryAnnotationScheme        = "discovery.kuadrant.io/scheme"
	KuadrantDiscoveryAnnotationAPIName       = "discovery.kuadrant.io/api-name"
	KuadrantDiscoveryAnnotationTag           = "discovery.kuadrant.io/tag"
	KuadrantDiscoveryAnnotationPort          = "discovery.kuadrant.io/port"
	KuadrantDiscoveryAnnotationOASConfigMap  = "discovery.kuadrant.io/oas-configmap"
	KuadrantDiscoveryAnnotationMatchPath     = "discovery.kuadrant.io/matchpath"
	KuadrantDiscoveryAnnotationMatchPathType = "discovery.kuadrant.io/matchpath-type"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	*reconcilers.BaseReconciler
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
	log := r.Logger().WithValues("service", req.NamespacedName)
	log.Info("Reconciling kuadrant service")

	service := &corev1.Service{}
	err := r.Client().Get(ctx, req.NamespacedName, service)

	if err != nil && apierrors.IsNotFound(err) {
		log.Info("resource not found. Ignoring since object must have been deleted")
		//TODO(jmprusi): Handle deletion of the Service. I guess using an OwnerReference could work for now.
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	if log.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(service, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		log.V(1).Info(string(jsonData))
	}

	serviceLabels := service.GetLabels()
	if kuadrantEnabled, ok := serviceLabels[KuadrantDiscoveryLabel]; !ok || kuadrantEnabled != "true" {
		// this service used to be kuadrant protected, not anymore
		return r.handleDisabledService(ctx, service)
	}

	// Let's generate the API object based on the Service annotations
	//TODO(jmprusi): If the user changes the api-name annotation, there will be a dangling API object. Fix this.
	desiredAPI, err := r.APIFromAnnotations(ctx, service)
	if err != nil {
		//TODO(jmprusi): If annotations are incorrect, we need to push that back to the user.
		return ctrl.Result{}, err
	}

	err = r.ReconcileResource(ctx, &networkingv1beta1.API{}, desiredAPI, alwaysUpdateAPI)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) APIFromAnnotations(ctx context.Context, service *corev1.Service) (*networkingv1beta1.API, error) {
	//Supported Annotations for now:
	//discovery.kuadrant.io/scheme: "https"
	//discovery.kuadrant.io/api-name: "dogs-api"
	//discovery.kuadrant.io/tag: "production"
	//discovery.kuadrant.io/port: 80 / name
	//discovery.kuadrant.io/oas-configmap: configmapName
	//discovery.kuadrant.io/matchpath: /
	//discovery.kuadrant.io/matchpath-type: Prefix
	// Related to https://github.com/Kuadrant/kuadrant-controller/issues/28

	log := r.Logger().WithValues("service", client.ObjectKeyFromObject(service))

	var apiName, scheme, tagLabel, port string
	var ok bool

	// Let's do some basic validation and setting defaults.
	if scheme, ok = service.Annotations[KuadrantDiscoveryAnnotationScheme]; !ok {
		scheme = "http"
	}
	if apiName, ok = service.Annotations[KuadrantDiscoveryAnnotationAPIName]; !ok {
		apiName = service.GetName()
	}

	if tagLabel, ok = service.Annotations[KuadrantDiscoveryAnnotationTag]; ok {
		apiName = networkingv1beta1.APIObjectName(apiName, tagLabel)
	}

	var oasContentPtr *string
	var pathMatchPtr *gatewayapiv1alpha1.HTTPPathMatch
	if oasConfigmapName, ok := service.Annotations[KuadrantDiscoveryAnnotationOASConfigMap]; ok {
		oasContent, err := r.fetchOpenAPIFromConfigMap(
			ctx,
			types.NamespacedName{Name: oasConfigmapName, Namespace: service.Namespace},
		)
		log.V(1).Info("get OAS configmap", "objectKey", types.NamespacedName{Name: oasConfigmapName, Namespace: service.Namespace}, "error", err)
		if err != nil {
			return nil, err
		}
		oasContentPtr = &oasContent
	} else {
		// apply pathmatch
		defaultType := gatewayapiv1alpha1.PathMatchPrefix
		defaultValue := "/"
		pathMatchPtr = &gatewayapiv1alpha1.HTTPPathMatch{Type: &defaultType, Value: &defaultValue}
		if path, ok := service.Annotations[KuadrantDiscoveryAnnotationMatchPath]; ok {
			pathMatchPtr.Value = &path
		}
		if pathMatchTypeVal, ok := service.Annotations[KuadrantDiscoveryAnnotationMatchPathType]; ok {
			pathMatchType := gatewayapiv1alpha1.PathMatchType(pathMatchTypeVal)
			switch pathMatchType {
			case gatewayapiv1alpha1.PathMatchExact, gatewayapiv1alpha1.PathMatchPrefix, gatewayapiv1alpha1.PathMatchRegularExpression:
				pathMatchPtr.Type = &pathMatchType
			default:
				return nil, fmt.Errorf("annotation '%s' value %s is invalid", KuadrantDiscoveryAnnotationMatchPathType, pathMatchTypeVal)
			}
		}
	}

	var destinationPort int32

	// Let's find out the port, this annotation is a little bit more tricky.
	if port, ok = service.Annotations[KuadrantDiscoveryAnnotationPort]; ok {
		// check if the port is a number already.
		if num, err := strconv.ParseInt(port, 10, 32); err == nil {
			destinationPort = int32(num)
		} else {
			// As the port is name, resolv the port from the service
			for _, p := range service.Spec.Ports {
				if p.Name == port {
					destinationPort = p.Port
					break
				}
			}
		}
	} else {
		// As the annotation has not been set, let's check if the service has only one port, if that's the case,
		//default to it.
		if len(service.Spec.Ports) == 1 {
			destinationPort = service.Spec.Ports[0].Port
		}
	}
	// If we reach this point and the Port is still nil, this means bad news
	if destinationPort == 0 {
		return nil, fmt.Errorf("%s is missing or invalid", KuadrantDiscoveryAnnotationPort)
	}

	apiFactory := APIFactory{
		Name: apiName,
		// TODO(jmprusi): We will create the API object in the same namespace as the service to simplify the deletion,
		// review this later.
		Namespace:            service.Namespace,
		DestinationSchema:    scheme,
		DestinationName:      service.Name,
		DestinationNamespace: service.Namespace,
		DestinationPort:      &destinationPort,
		OASContent:           oasContentPtr,
		HTTPPathMatch:        pathMatchPtr,
	}

	desiredAPI := apiFactory.API()

	// Add "controlled" owner reference. Prevents other controller to adopt this resource
	err := controllerutil.SetControllerReference(service, desiredAPI, r.Scheme())

	return desiredAPI, err
}

func (r *ServiceReconciler) fetchOpenAPIFromConfigMap(ctx context.Context, cmKey types.NamespacedName) (string, error) {
	oasConfigmap := &corev1.ConfigMap{}
	err := r.Client().Get(ctx, cmKey, oasConfigmap)
	if err != nil {
		return "", err
	}
	// TODO(jmprusi): The openapispec.yaml data entry in the configmap is hardcoded, review this.
	if _, ok := oasConfigmap.Data["openapi.yaml"]; !ok {
		return "", errors.New("oas configmap is missing the openapi.yaml entry")
	}

	return oasConfigmap.Data["openapi.yaml"], nil
}

func (r *ServiceReconciler) handleDisabledService(ctx context.Context, service *corev1.Service) (ctrl.Result, error) {
	// This implementation assumes API resources are created in the same namespace as the service and there is an ownership relationship
	log := r.Logger().WithValues("service", client.ObjectKeyFromObject(service))
	log.V(1).Info("handleDisabledService")

	ownedAPI, err := r.getOwnedAPI(ctx, service)
	if err != nil || ownedAPI == nil {
		return ctrl.Result{}, err
	}

	// delete
	err = r.Client().Delete(ctx, ownedAPI)
	log.V(1).Info("handleDisabledService: deleting API", "api", client.ObjectKeyFromObject(ownedAPI), "error", err)
	return ctrl.Result{}, err
}

func (r *ServiceReconciler) getOwnedAPI(ctx context.Context, service *corev1.Service) (*networkingv1beta1.API, error) {
	log := r.Logger().WithValues("service", client.ObjectKeyFromObject(service))

	apiList := &networkingv1beta1.APIList{}
	err := r.Client().List(ctx, apiList, client.InNamespace(service.Namespace))
	log.V(1).Info("reading API list", "namespace", service.Namespace, "len(api)", len(apiList.Items), "error", err)
	if err != nil {
		return nil, err
	}

	for idx := range apiList.Items {
		if common.IsOwnedBy(&apiList.Items[idx], service) {
			return &apiList.Items[idx], nil
		}
	}

	return nil, nil
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
			if val, ok := e.Object.GetLabels()[KuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// In case the update object had the kuadrant label set to true, we need to reconcile it.
			if val, ok := e.ObjectOld.GetLabels()[KuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			// In case that service gets update by adding the label, and set to true, we need to reconcile it.
			if val, ok := e.ObjectNew.GetLabels()[KuadrantDiscoveryLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			_, ok := e.Object.GetLabels()[KuadrantDiscoveryLabel]
			return ok
		},
	}
}

func alwaysUpdateAPI(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*networkingv1beta1.API)
	if !ok {
		return false, fmt.Errorf("%T is not a *networkingv1beta1.API", existingObj)
	}
	desired, ok := desiredObj.(*networkingv1beta1.API)
	if !ok {
		return false, fmt.Errorf("%T is not a *networkingv1beta1.API", desiredObj)
	}

	existing.Spec = desired.Spec
	return true, nil
}
