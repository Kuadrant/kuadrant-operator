package mappers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// TODO: Clean this up
type EventMapperTwo interface {
	MapToPolicy(client.Object, schema.GroupVersionKind) []reconcile.Request
}

func NewGatewayEventMapper(o ...MapperOption) EventMapperTwo {
	return &gatewayEventMapper{opts: Apply(o...)}
}

var _ EventMapperTwo = &gatewayEventMapper{}

type gatewayEventMapper struct {
	opts MapperOptions
}

func (m *gatewayEventMapper) MapToPolicy(obj client.Object, policyGVK schema.GroupVersionKind) []reconcile.Request {
	logger := m.opts.Logger.WithValues("gateway", client.ObjectKeyFromObject(obj))
	ctx := context.Background()
	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.V(1).Info(fmt.Sprintf("%T is not type gateway, unable to map policies to gateway", obj))
		return []reconcile.Request{}
	}
	routeList := &gatewayapiv1.HTTPRouteList{}
	fields := client.MatchingFields{kuadrantgatewayapi.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gateway).String()}
	if err := m.opts.Client.List(ctx, routeList, fields); err != nil {
		logger.V(1).Error(err, "unable to list HTTPRoutes")
		return []reconcile.Request{}
	}

	policyList := &unstructured.UnstructuredList{}
	policyList.SetAPIVersion(policyGVK.Version)
	policyList.SetKind(policyGVK.Kind)
	if err := m.opts.Client.List(ctx, policyList, client.InNamespace(obj.GetNamespace())); err != nil {
		logger.V(1).Info("unable to list UnstructuredList of policies, %T", policyGVK)
		return []reconcile.Request{}
	}

	var policies []kuadrantgatewayapi.Policy
	if err := policyList.EachListItem(func(obj runtime.Object) error {
		objBytes, err := json.Marshal(obj)
		if err != nil {
			return err
		}

		switch obj.GetObjectKind().GroupVersionKind().Kind {
		case "AuthPolicy":
			policy := &api.AuthPolicy{}
			err = json.Unmarshal(objBytes, policy)
			if err != nil {
				return err
			}
			policies = append(policies, policy)
		case "DNSPolicy":
			policy := &v1alpha1.DNSPolicy{}
			err = json.Unmarshal(objBytes, policy)
			if err != nil {
				return err
			}
			policies = append(policies, policy)
		case "TLSPolicy":
			policy := &v1alpha1.TLSPolicy{}
			err = json.Unmarshal(objBytes, policy)
			if err != nil {
				return err
			}
			policies = append(policies, policy)
		case "RateLimitPolicy":
			policy := &api.RateLimitPolicy{}
			err = json.Unmarshal(objBytes, policy)
			if err != nil {
				return err
			}
			policies = append(policies, policy)
		default:
			return fmt.Errorf("unknown policy kind: %s", obj.GetObjectKind().GroupVersionKind().Kind)
		}
		return nil
	}); err != nil {
		logger.V(1).Error(err, "unable to map unstructured List of policies")
		return []reconcile.Request{}
	}

	if len(policies) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
		return []reconcile.Request{}
	}

	topology, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gateway}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		logger.V(1).Error(err, "unable to build topology for gateway")
		return []reconcile.Request{}
	}

	index := kuadrantgatewayapi.NewTopologyIndexes(topology)
	return utils.Map(index.PoliciesFromGateway(gateway), func(p kuadrantgatewayapi.Policy) reconcile.Request {
		policyKey := client.ObjectKeyFromObject(p)
		logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found", policyGVK.Kind, policyKey)
		return reconcile.Request{NamespacedName: policyKey}
	})
}
