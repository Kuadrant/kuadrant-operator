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

package reconcilers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

type TargetRefReconciler struct {
	*BaseReconciler
}

// blank assignment to verify that BaseReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &TargetRefReconciler{}

func (r *TargetRefReconciler) Reconcile(context.Context, ctrl.Request) (ctrl.Result, error) {
	return reconcile.Result{}, nil
}

func (r *TargetRefReconciler) FetchValidGateway(ctx context.Context, key client.ObjectKey) (*gatewayapiv1alpha2.Gateway, error) {
	logger, _ := logr.FromContext(ctx)

	gw := &gatewayapiv1alpha2.Gateway{}
	err := r.Client().Get(ctx, key, gw)
	logger.V(1).Info("FetchValidGateway", "gateway", key, "err", err)
	if err != nil {
		return nil, err
	}

	if meta.IsStatusConditionFalse(gw.Status.Conditions, "Ready") {
		return nil, fmt.Errorf("FetchValidGateway: gateway (%v) not ready", key)
	}

	return gw, nil
}

func (r *TargetRefReconciler) FetchValidHTTPRoute(ctx context.Context, key client.ObjectKey) (*gatewayapiv1alpha2.HTTPRoute, error) {
	logger, _ := logr.FromContext(ctx)

	httpRoute := &gatewayapiv1alpha2.HTTPRoute{}
	err := r.Client().Get(ctx, key, httpRoute)
	logger.V(1).Info("FetchValidHTTPRoute", "httpRoute", key, "err", err)
	if err != nil {
		return nil, err
	}

	// Check HTTProute parents (gateways) in the status object
	// if any of the current parent gateways reports not "Admitted", return error
	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		routeParentStatus := func(pRef gatewayapiv1alpha2.ParentRef) *gatewayapiv1alpha2.RouteParentStatus {
			for idx := range httpRoute.Status.RouteStatus.Parents {
				if reflect.DeepEqual(pRef, httpRoute.Status.RouteStatus.Parents[idx].ParentRef) {
					return &httpRoute.Status.RouteStatus.Parents[idx]
				}
			}

			return nil
		}(parentRef)

		if routeParentStatus == nil {
			continue
		}

		if meta.IsStatusConditionFalse(routeParentStatus.Conditions, "Accepted") {
			return nil, fmt.Errorf("FetchValidHTTPRoute: httproute (%v) not accepted", key)
		}
	}

	return httpRoute, nil
}

// FetchValidTargetRef fetches the target reference object and checks the status is valid
func (r *TargetRefReconciler) FetchValidTargetRef(ctx context.Context, targetRef gatewayapiv1alpha2.PolicyTargetReference, defaultNs string) (client.Object, error) {
	tmpNS := defaultNs
	if targetRef.Namespace != nil {
		tmpNS = string(*targetRef.Namespace)
	}

	objKey := client.ObjectKey{Name: string(targetRef.Name), Namespace: tmpNS}

	if common.IsTargetRefHTTPRoute(targetRef) {
		return r.FetchValidHTTPRoute(ctx, objKey)
	} else if common.IsTargetRefGateway(targetRef) {
		return r.FetchValidGateway(ctx, objKey)
	}

	return nil, fmt.Errorf("FetchValidTargetRef: targetRef (%v) to unknown network resource", targetRef)
}

// TargetedGatewayKeys returns the list of gateways that are being referenced from the target.
func (r *TargetRefReconciler) TargetedGatewayKeys(ctx context.Context, targetRef gatewayapiv1alpha2.PolicyTargetReference, defaultNs string) ([]client.ObjectKey, error) {
	gwKeys := make([]client.ObjectKey, 0)

	if common.IsTargetRefHTTPRoute(targetRef) {
		tmpNS := defaultNs
		if targetRef.Namespace != nil {
			tmpNS = string(*targetRef.Namespace)
		}
		objKey := client.ObjectKey{Name: string(targetRef.Name), Namespace: tmpNS}
		httpRoute, err := r.FetchValidHTTPRoute(ctx, objKey)
		if err != nil {
			return nil, err
		}

		for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
			gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: httpRoute.Namespace}
			if parentRef.Namespace != nil {
				gwKey.Namespace = string(*parentRef.Namespace)
			}
			gwKeys = append(gwKeys, gwKey)
		}
	} else if common.IsTargetRefGateway(targetRef) {
		gwKey := client.ObjectKey{Name: string(targetRef.Name), Namespace: defaultNs}
		if targetRef.Namespace != nil {
			gwKey.Namespace = string(*targetRef.Namespace)
		}
		gwKeys = []client.ObjectKey{gwKey}
	}

	return gwKeys, nil
}

func (r *TargetRefReconciler) TargetHostnames(ctx context.Context, targetRef gatewayapiv1alpha2.PolicyTargetReference, defaultNs string) ([]string, error) {
	targetObj, err := r.FetchValidTargetRef(ctx, targetRef, defaultNs)
	if err != nil {
		return nil, err
	}

	netResourceHosts := make([]string, 0)
	switch netResource := targetObj.(type) {
	case *gatewayapiv1alpha2.HTTPRoute:
		for _, hostname := range netResource.Spec.Hostnames {
			netResourceHosts = append(netResourceHosts, string(hostname))
		}
	case *gatewayapiv1alpha2.Gateway:
		for idx := range netResource.Spec.Listeners {
			if netResource.Spec.Listeners[idx].Hostname != nil {
				netResourceHosts = append(netResourceHosts, string(*netResource.Spec.Listeners[idx].Hostname))
			}
		}
	}

	if len(netResourceHosts) == 0 {
		netResourceHosts = append(netResourceHosts, string("*"))
	}

	return netResourceHosts, nil
}

// ReconcileTargetBackReference adds policy key in annotations of the target object
func (r *TargetRefReconciler) ReconcileTargetBackReference(ctx context.Context,
	policyKey client.ObjectKey, targetRef gatewayapiv1alpha2.PolicyTargetReference,
	defaultNs string, annotationName string) error {
	logger, _ := logr.FromContext(ctx)
	targetObj, err := r.FetchValidTargetRef(ctx, targetRef, defaultNs)
	if err != nil {
		// The object should also exist
		return err
	}

	targetObjKey := client.ObjectKeyFromObject(targetObj)
	targetObjType := targetObj.GetObjectKind().GroupVersionKind()

	// Reconcile the back reference:
	objAnnotations := targetObj.GetAnnotations()
	if objAnnotations == nil {
		objAnnotations = map[string]string{}
	}

	val, ok := objAnnotations[annotationName]
	if ok {
		if val != policyKey.String() {
			return fmt.Errorf("the %s target %s is already referenced by policy %s",
				targetObjType, targetObjKey, policyKey.String())
		}
	} else {
		objAnnotations[annotationName] = policyKey.String()
		targetObj.SetAnnotations(objAnnotations)
		err := r.UpdateResource(ctx, targetObj)
		logger.V(1).Info("ReconcileTargetBackReference: update target object",
			"type", targetObjType, "name", targetObjKey, "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *TargetRefReconciler) DeleteTargetBackReference(ctx context.Context,
	policyKey client.ObjectKey, targetRef gatewayapiv1alpha2.PolicyTargetReference,
	defaultNs string, annotationName string) error {
	logger, _ := logr.FromContext(ctx)

	targetObj, err := r.FetchValidTargetRef(ctx, targetRef, defaultNs)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	targetObjKey := client.ObjectKeyFromObject(targetObj)
	targetObjType := targetObj.GetObjectKind().GroupVersionKind()

	// Reconcile the back reference:
	objAnnotations := targetObj.GetAnnotations()
	if objAnnotations == nil {
		objAnnotations = map[string]string{}
	}

	if _, ok := objAnnotations[annotationName]; ok {
		delete(objAnnotations, annotationName)
		targetObj.SetAnnotations(objAnnotations)
		err := r.UpdateResource(ctx, targetObj)
		logger.V(1).Info("DeleteTargetBackReference: update network resource",
			"type", targetObjType, "name", targetObjKey, "err", err)
		if err != nil {
			return err
		}
	}
	return nil
}
