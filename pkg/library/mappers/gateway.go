package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func NewGatewayEventMapper(o ...MapperOption) EventMapper {
	return &gatewayEventMapper{opts: Apply(o...)}
}

var _ EventMapper = &gatewayEventMapper{}

type gatewayEventMapper struct {
	opts MapperOptions
}

func (m *gatewayEventMapper) MapToPolicy(obj client.Object, policyKind kuadrant.Referrer) []reconcile.Request {
	logger := m.opts.Logger.WithValues("gateway", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range kuadrant.BackReferencesFromObject(gateway, policyKind) {
		logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found", policyKind.Kind(), policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	if len(requests) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
	}

	return requests
}

func MapToPolicyAP(ctx context.Context, apiClient client.Client, obj client.Object, policyKind kuadrant.Referrer) []reconcile.Request {
	// TODO: logger removed as function is not part of gatewayEventMapper interface.
	//logger := m.opts.Logger.WithValues("gateway", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		//logger.Info("cannot map gateway related event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range kuadrant.BackReferencesFromObject(gateway, policyKind) {
		//logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found", policyKind.Kind(), policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	if len(requests) == 0 {
		authPolices := &api.AuthPolicyList{}
		err := apiClient.List(ctx, authPolices, client.InNamespace(obj.GetNamespace()))
		if err != nil {
			return requests
			// TODO: add some logging or something.
		}
		//logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
		for idx, authPolicy := range authPolices.Items {
			for _, cond := range authPolicy.Status.Conditions {
				if cond.Type == string(gatewayapiv1alpha2.PolicyConditionAccepted) && cond.Reason == string(gatewayapiv1alpha2.PolicyReasonTargetNotFound) {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&authPolices.Items[idx])})
				}
			}
		}
	}

	return requests
}
