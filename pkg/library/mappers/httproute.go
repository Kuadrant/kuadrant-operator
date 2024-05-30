package mappers

import (
	"context"
	"fmt"
	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func NewHTTPRouteEventMapper(o ...MapperOption) EventMapperTwo {
	return &httpRouteEventMapper{opts: Apply(o...)}
}

var _ EventMapperTwo = &httpRouteEventMapper{}

type httpRouteEventMapper struct {
	opts MapperOptions
}

func (m *httpRouteEventMapper) MapToPolicy(obj client.Object, policyGVK schema.GroupVersionKind) []reconcile.Request {
	logger := m.opts.Logger.WithValues("httproute", client.ObjectKeyFromObject(obj))
	ctx := context.Background()
	requests := make([]reconcile.Request, 0)
	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	gatewayKeys := kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(httpRoute)

	for _, gatewayKey := range gatewayKeys {
		gateway := &gatewayapiv1.Gateway{}
		err := m.opts.Client.Get(ctx, gatewayKey, gateway)
		if err != nil {
			logger.Info("cannot get gateway", "error", err)
			continue
		}

		routeList := &gatewayapiv1.HTTPRouteList{}
		fields := client.MatchingFields{kuadrantgatewayapi.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gateway).String()}
		if err = m.opts.Client.List(ctx, routeList, fields); err != nil {
			logger.Info("cannot list httproutes", "error", err)
			continue
		}
		policyList := &unstructured.UnstructuredList{}
		policyList.SetAPIVersion(policyGVK.Version)
		policyList.SetKind(policyGVK.Kind)
		if err = m.opts.Client.List(ctx, policyList, client.InNamespace(obj.GetNamespace())); err != nil {
			logger.V(1).Info("unable to list UnstructuredList of policies, %T", policyGVK)
			continue
		}

		var policies []kuadrantgatewayapi.Policy
		if err = policyList.EachListItem(func(obj runtime.Object) error {
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
			logger.Info("unable to list UnstructuredList of policies, %T", policyGVK)
			continue
		}
		if len(policies) == 0 {
			logger.Info("no kuadrant policy possibly affected by the gateway related event")
			continue
		}
		topology, err := kuadrantgatewayapi.NewTopology(
			kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gateway}),
			kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
			kuadrantgatewayapi.WithPolicies(policies),
			kuadrantgatewayapi.WithLogger(logger),
		)
		if err != nil {
			logger.Info("unable to build topology for gateway", "error", err)
			continue
		}
		index := kuadrantgatewayapi.NewTopologyIndexes(topology)
		data := utils.Map(index.PoliciesFromGateway(gateway), func(p kuadrantgatewayapi.Policy) reconcile.Request {
			policyKey := client.ObjectKeyFromObject(p)
			logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found", policyGVK.Kind, policyKey)
			return reconcile.Request{NamespacedName: policyKey}
		})
		requests = append(requests, data...)
	}

	if len(requests) != 0 {
		return requests
	}

	// This block is required when a HTTProute has being deleted
	var policy kuadrant.Referrer
	switch policyGVK.Kind {
	case "AuthPolicy":
		policy = &api.AuthPolicy{}
	case "DNSPolicy":
		policy = &v1alpha1.DNSPolicy{}
	case "TLSPolicy":
		policy = &v1alpha1.TLSPolicy{}
	case "RateLimitPolicy":
		policy = &api.RateLimitPolicy{}
	default:
		return requests
	}
	policyKey := kuadrant.DirectReferencesFromObject(httpRoute, policy)
	requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	return requests
}
