package mappers

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewHTTPRouteToPolicyEventMapper(policyType kuadrantgatewayapi.PolicyType, o ...MapperOption) *HTTPRouteToPolicyEventMapper {
	return &HTTPRouteToPolicyEventMapper{
		policyType: policyType,
		opts:       Apply(o...),
	}
}

type HTTPRouteToPolicyEventMapper struct {
	opts       MapperOptions
	policyType kuadrantgatewayapi.PolicyType
}

func (h *HTTPRouteToPolicyEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := h.opts.Logger.WithValues("httproute", client.ObjectKeyFromObject(obj))

	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	// trigger on event on attached policies
	policies, err := h.policyType.GetList(ctx, h.opts.Client)
	logger.V(1).Info("list policies", "#items", len(policies), "err", err)
	if err != nil {
		logger.V(1).Error(err, "unable to list policies")
		return []reconcile.Request{}
	}

	attachedPolicies := utils.Filter(policies, func(p kuadrantgatewayapi.Policy) bool {
		group := string(p.GetTargetRef().Group)
		kind := string(p.GetTargetRef().Kind)
		name := string(p.GetTargetRef().Name)
		namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

		return group == gatewayapiv1.GroupVersion.Group &&
			kind == "HTTPRoute" &&
			name == httpRoute.GetName() &&
			namespace == gatewayapiv1.Namespace(httpRoute.GetNamespace())
	})

	if len(attachedPolicies) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the httproute related event")
	}

	return utils.Map(attachedPolicies, func(p kuadrantgatewayapi.Policy) reconcile.Request {
		policyKey := client.ObjectKeyFromObject(p)
		logger.V(1).Info("new request", "policy key", policyKey)
		return reconcile.Request{NamespacedName: policyKey}
	})
}
