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

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type TargetRefReconciler struct {
	client.Client
}

// FetchAcceptedGatewayHTTPRoutes returns the list of HTTPRoutes that have been accepted as children of a gateway.
func (r *TargetRefReconciler) FetchAcceptedGatewayHTTPRoutes(ctx context.Context, gateway *gatewayapiv1.Gateway) (routes []gatewayapiv1.HTTPRoute) {
	gwKey := client.ObjectKeyFromObject(gateway)
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("FetchAcceptedGatewayHTTPRoutes").WithValues("gateway", gwKey)

	routeList := &gatewayapiv1.HTTPRouteList{}
	err := r.Client.List(ctx, routeList)
	if err != nil {
		logger.V(1).Info("failed to list httproutes", "err", err)
		return
	}

	for idx := range routeList.Items {
		route := routeList.Items[idx]
		if utils.Index(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(&route), func(parentGatewayKey client.ObjectKey) bool { return parentGatewayKey == gwKey }) >= 0 {
			logger.V(1).Info("found route attached to gateway", "httproute", client.ObjectKeyFromObject(&route))
			routes = append(routes, route)
			continue
		}
		logger.V(1).Info("skipping route, not attached to gateway", "httproute", client.ObjectKeyFromObject(&route))
	}

	return
}

// TargetedGatewayKeys returns the list of gateways that are being referenced from the target.
func (r *TargetRefReconciler) TargetedGatewayKeys(_ context.Context, targetNetworkObject client.Object) []client.ObjectKey {
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		gwKeys := make([]client.ObjectKey, 0)
		for _, parentRef := range obj.Spec.CommonRouteSpec.ParentRefs {
			gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: obj.Namespace}
			if parentRef.Namespace != nil {
				gwKey.Namespace = string(*parentRef.Namespace)
			}
			gwKeys = append(gwKeys, gwKey)
		}
		return gwKeys

	case *gatewayapiv1.Gateway:
		return []client.ObjectKey{client.ObjectKeyFromObject(targetNetworkObject)}

	// If the targetNetworkObject is nil, we don't fail; instead, we return an empty slice of gateway keys.
	// This is for supporting a smooth cleanup in cases where the network object has been deleted already
	default:
		return []client.ObjectKey{}
	}
}

// ReconcileTargetBackReference reconciles policy key in annotations of the target object
func (r *TargetRefReconciler) ReconcileTargetBackReference(ctx context.Context, p kuadrant.Policy, targetNetworkObject client.Object, annotationName string) error {
	logger, _ := logr.FromContext(ctx)

	policyKey := client.ObjectKeyFromObject(p)
	targetNetworkObjectKey := client.ObjectKeyFromObject(targetNetworkObject)
	targetNetworkObjectKind := targetNetworkObject.GetObjectKind().GroupVersionKind()

	// Step 1 Build list of network objects in the same namespace as the policy
	// Step 2 Remove the direct back reference annotation to the current policy from any network object not being currently referenced
	// Step 3 Check direct back ref annotation from the current target network object
	//   Step 3.1 if it does not exit -> create it
	//   Step 3.2 if it already exits and the reference is the current policy -> nothing to do
	//   Step 3.3 if it already exits and the reference is not the current policy -> return err

	// Step 1
	gwList := &gatewayapiv1.GatewayList{}
	err := r.Client.List(ctx, gwList, client.InNamespace(p.GetNamespace()))
	logger.V(1).Info("ReconcileTargetBackReference: list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	err = r.Client.List(ctx, routeList, client.InNamespace(p.GetNamespace()))
	logger.V(1).Info("ReconcileTargetBackReference: list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return err
	}

	networkObjectList := utils.Map(gwList.Items, func(g gatewayapiv1.Gateway) client.Object { return &g })
	networkObjectList = append(networkObjectList, utils.Map(routeList.Items, func(g gatewayapiv1.HTTPRoute) client.Object { return &g })...)
	// remove currently targeted network resource from the list
	networkObjectList = utils.Filter(networkObjectList, func(obj client.Object) bool {
		return targetNetworkObjectKey != client.ObjectKeyFromObject(obj)
	})

	// Step 2
	for _, networkObject := range networkObjectList {
		annotations := networkObject.GetAnnotations()
		if val, ok := annotations[annotationName]; ok && val == policyKey.String() {
			delete(annotations, annotationName)
			networkObject.SetAnnotations(annotations)
			err := r.Client.Update(ctx, networkObject)
			logger.V(1).Info("ReconcileTargetBackReference: update network resource",
				"kind", networkObject.GetObjectKind().GroupVersionKind(),
				"name", client.ObjectKeyFromObject(networkObject), "err", err)
			if err != nil {
				return err
			}
		}
	}

	// Step 3
	objAnnotations := utils.ReadAnnotationsFromObject(targetNetworkObject)

	if val, ok := objAnnotations[annotationName]; ok {
		if val != policyKey.String() {
			// Step  3.3
			return kuadrant.NewErrConflict(p.Kind(), val, fmt.Errorf("the %s target %s is already referenced by policy %s", targetNetworkObjectKind, targetNetworkObjectKey, val))
		}
		// Step  3.2
		// NO OP
	} else {
		// Step  3.1
		objAnnotations[annotationName] = policyKey.String()
		targetNetworkObject.SetAnnotations(objAnnotations)
		err := r.Client.Update(ctx, targetNetworkObject)
		logger.V(1).Info("ReconcileTargetBackReference: update target object", "kind", targetNetworkObjectKind, "name", targetNetworkObjectKey, "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *TargetRefReconciler) DeleteTargetBackReference(ctx context.Context, targetNetworkObject client.Object, annotationName string) error {
	logger, _ := logr.FromContext(ctx)

	targetNetworkObjectKey := client.ObjectKeyFromObject(targetNetworkObject)
	targetNetworkObjectKind := targetNetworkObject.GetObjectKind().GroupVersionKind()

	// Reconcile the back reference:
	objAnnotations := utils.ReadAnnotationsFromObject(targetNetworkObject)

	if _, ok := objAnnotations[annotationName]; ok {
		delete(objAnnotations, annotationName)
		targetNetworkObject.SetAnnotations(objAnnotations)
		err := r.Client.Update(ctx, targetNetworkObject)
		logger.V(1).Info("DeleteTargetBackReference: update network resource", "kind", targetNetworkObjectKind, "name", targetNetworkObjectKey, "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

// ReconcileGatewayPolicyReferences updates the annotations in the Gateway resources that list to all the policies
// that directly or indirectly target the gateway, based upon a pre-computed gateway diff object
func (r *TargetRefReconciler) ReconcileGatewayPolicyReferences(ctx context.Context, policy client.Object, gwDiffObj *GatewayDiffs) error {
	logger, _ := logr.FromContext(ctx)

	// delete the policy from the annotations of the gateways no longer target by the policy
	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		if gw.DeletePolicy(client.ObjectKeyFromObject(policy)) {
			err := r.Client.Update(ctx, gw.Gateway)
			logger.V(1).Info("ReconcileGatewayPolicyReferences: update gateway", "gateway with invalid policy ref", gw.Key(), "err", err)
			if err != nil {
				return err
			}
		}
	}

	// add the policy to the annotations of the gateways target by the policy
	for _, gw := range gwDiffObj.GatewaysMissingPolicyRef {
		if gw.AddPolicy(client.ObjectKeyFromObject(policy)) {
			err := r.Client.Update(ctx, gw.Gateway)
			logger.V(1).Info("ReconcileGatewayPolicyReferences: update gateway", "gateway missing policy ref", gw.Key(), "err", err)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
