package mappers

import (
	"context"
	"fmt"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type PolicyToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewPolicyToKuadrantEventMapper(o ...MapperOption) *PolicyToKuadrantEventMapper {
	return &PolicyToKuadrantEventMapper{opts: Apply(o...)}
}

func (m *PolicyToKuadrantEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	policy, ok := obj.(kuadrant.Policy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a kuadrant.Policy", obj), "cannot map")
		return []reconcile.Request{}
	}

	kuadrantNamespace, err := kuadrant.GetKuadrantNamespaceFromPolicyTargetRef(ctx, m.opts.Client, policy)
	if err != nil {
		logger.Error(err, "cannot get kuadrant namespace")
		return []reconcile.Request{}
	}
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	err = m.opts.Client.List(ctx, kuadrantList, &client.ListOptions{Namespace: kuadrantNamespace})
	if err != nil {
		logger.Error(err, "cannot list kuadrants")
		return []reconcile.Request{}
	}
	if len(kuadrantList.Items) == 0 {
		logger.Error(err, "kuadrant does not exist in expected namespace")
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(&kuadrantList.Items[0])}}
}
