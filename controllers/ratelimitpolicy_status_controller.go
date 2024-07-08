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
	"slices"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// RateLimitPolicyStatusReconciler reconciles a RateLimitPolicy status subresource
type RateLimitPolicyStatusReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitPolicyStatusReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("ratelimitpolicy", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("reconciling ratelimitpolicy status")
	ctx := logr.NewContext(eventCtx, logger)

	rlp := &kuadrantv1beta2.RateLimitPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, rlp); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(rlp, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	// Ignore deleted gatewayAPI objects, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if rlp.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	// reconcile ratelimitpolicy status
	err := r.reconcileStatus(ctx, rlp)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Failed to update status: resource might just be outdated", "error", err)
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	logger.Info("RateLimitPolicy reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyStatusReconciler) reconcileStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	// Get all the gateways
	gwList := &gatewayapiv1.GatewayList{}
	err = r.Client().List(ctx, gwList)
	logger.V(1).Info("list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return err
	}

	// Get all the routes
	routeList := &gatewayapiv1.HTTPRouteList{}
	err = r.Client().List(ctx, routeList)
	logger.V(1).Info("list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return err
	}

	topology, err := kuadrantgatewayapi.NewBasicTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies([]kuadrantgatewayapi.Policy{rlp}),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		logger.V(1).Error(err, "unable to build topology")
		return err
	}

	newStatus := r.calculateStatus(ctx, rlp, topology)
	err = r.ReconcileResourceStatus(ctx, client.ObjectKeyFromObject(rlp), &kuadrantv1beta2.RateLimitPolicy{}, newStatus)
	if err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyStatusReconciler) calculateStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) *kuadrantv1beta2.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta2.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(rlp.Status.Conditions),
		ObservedGeneration: rlp.Status.ObservedGeneration,
	}

	acceptedCond := r.acceptedCondition(ctx, rlp, topology)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
	}

	return newStatus
}

func (r *RateLimitPolicyStatusReconciler) acceptedCondition(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) *metav1.Condition {
	validations := []func(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error{
		r.validatePolicy,
		r.checkTargetReference,
		r.validatePolicyHostnames,
		r.checkDirectReferences,
	}

	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", rlp.Kind()),
	}

	for _, validation := range validations {
		err := validation(ctx, rlp, topology)
		if err != nil {
			// Wrap error into a PolicyError if it is not this type
			var policyErr kuadrant.PolicyError
			if !errors.As(err, &policyErr) {
				policyErr = kuadrant.NewErrUnknown(rlp.Kind(), err)
			}

			cond.Status = metav1.ConditionFalse
			cond.Message = policyErr.Error()
			cond.Reason = string(policyErr.Reason())

			return cond
		}
	}

	return cond
}

func (r *RateLimitPolicyStatusReconciler) validatePolicy(_ context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, _ *kuadrantgatewayapi.Topology) error {
	if err := rlp.Validate(); err != nil {
		return kuadrant.NewErrInvalid(rlp.Kind(), err)
	}

	return nil
}

func (r *RateLimitPolicyStatusReconciler) checkTargetReference(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	targetRef := policyNode.TargetRef()
	if targetRef == nil {
		return kuadrant.NewErrTargetNotFound(
			rlp.Kind(),
			rlp.GetTargetRef(),
			errors.New("not found in gateway api topology"),
		)
	}

	return nil
}

func (r *RateLimitPolicyStatusReconciler) validatePolicyHostnames(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	var targetNetworkObject client.Object

	if gNode := policyNode.TargetRef().GetGatewayNode(); gNode != nil {
		targetNetworkObject = gNode.Gateway
	} else if rNode := policyNode.TargetRef().GetRouteNode(); rNode != nil {
		targetNetworkObject = rNode.HTTPRoute
	}

	if err := kuadrant.ValidateHierarchicalRules(rlp, targetNetworkObject); err != nil {
		return kuadrant.NewErrInvalid(rlp.Kind(), err)
	}

	return nil
}

// checkDirectReference returns error only when the target reference has a policy reference set
// and the reference does not belong to the provided policy
func (r *RateLimitPolicyStatusReconciler) checkDirectReferences(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	var targetNetworkObject client.Object

	if gNode := policyNode.TargetRef().GetGatewayNode(); gNode != nil {
		targetNetworkObject = gNode.Gateway
	} else if rNode := policyNode.TargetRef().GetRouteNode(); rNode != nil {
		targetNetworkObject = rNode.HTTPRoute
	}

	key := kuadrant.GetPolicyReference(targetNetworkObject, kuadrantv1beta2.RateLimitPolicyGVK)

	if key != nil && client.ObjectKeyFromObject(rlp) != *key {
		return kuadrant.NewErrConflict(
			kuadrantv1beta2.RateLimitPolicyGVK.Kind,
			key.String(),
			fmt.Errorf("the %s target %s is already referenced by policy %s",
				targetNetworkObject.GetObjectKind().GroupVersionKind(),
				client.ObjectKeyFromObject(targetNetworkObject),
				key.String()),
		)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Ratelimitpolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	gatewayToPolicyEventMapper := mappers.NewGatewayToPolicyEventMapper(
		kuadrantv1beta2.NewRateLimitPolicyType(),
		mappers.WithLogger(r.Logger().WithName("gateway.mapper")),
		mappers.WithClient(r.Client()),
	)

	routeToPolicyEventMapper := mappers.NewHTTPRouteToPolicyEventMapper(
		kuadrantv1beta2.NewRateLimitPolicyType(),
		mappers.WithLogger(r.Logger().WithName("httproute.mapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta2.RateLimitPolicy{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(routeToPolicyEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayToPolicyEventMapper.Map),
		).
		Complete(r)
}
