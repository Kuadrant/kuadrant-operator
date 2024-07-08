package mappers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

// PolicyToTargetRefMapper is an EventHandler that maps policies to types targeted by the policy,
// for a given type only
type PolicyToTargetRefMapper struct {
	opts       MapperOptions
	targetType kuadrantgatewayapi.GatewayAPIType
}

func NewPolicyToTargetRefMapper(targetType kuadrantgatewayapi.GatewayAPIType, o ...MapperOption) *PolicyToTargetRefMapper {
	return &PolicyToTargetRefMapper{
		targetType: targetType,
		opts:       Apply(o...),
	}
}

func (p *PolicyToTargetRefMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := p.opts.Logger.WithValues("source object", client.ObjectKeyFromObject(obj), "source kind", obj.GetObjectKind().GroupVersionKind().Kind)

	policy, ok := obj.(kuadrantgatewayapi.Policy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a Policy", obj), "cannot map")
		return []reconcile.Request{}
	}

	targetRefType := schema.GroupKind{
		Group: string(policy.GetTargetRef().Group),
		Kind:  string(policy.GetTargetRef().Kind),
	}

	mapperTargetType := schema.GroupKind{
		Group: p.targetType.GetGVK().Group,
		Kind:  p.targetType.GetGVK().Kind,
	}

	if targetRefType != mapperTargetType {
		logger.V(2).Info("target ref type does not match with the expected type")
		return []reconcile.Request{}
	}

	namespace := string(ptr.Deref(policy.GetTargetRef().Namespace, gatewayapiv1.Namespace(policy.GetNamespace())))
	key := client.ObjectKey{Name: string(policy.GetTargetRef().Name), Namespace: namespace}
	logger.V(1).Info("new request", "target key", key, "target kind", mapperTargetType.Kind)
	return []reconcile.Request{{NamespacedName: key}}
}
