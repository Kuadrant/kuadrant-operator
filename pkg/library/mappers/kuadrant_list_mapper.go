package mappers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

type KuadrantListEventMapper struct {
	opts MapperOptions
}

func NewKuadrantListEventMapper(o ...MapperOption) *KuadrantListEventMapper {
	return &KuadrantListEventMapper{opts: Apply(o...)}
}

func (m *KuadrantListEventMapper) Map(ctx context.Context, _ client.Object) []reconcile.Request {
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	err := m.opts.Client.List(ctx, kuadrantList)
	if err != nil {
		m.opts.Logger.Error(err, "cannot list kuadrants")
		return []reconcile.Request{}
	}
	if len(kuadrantList.Items) == 0 {
		m.opts.Logger.Error(err, "kuadrant does not exist in expected namespace")
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(&kuadrantList.Items[0])}}
}
