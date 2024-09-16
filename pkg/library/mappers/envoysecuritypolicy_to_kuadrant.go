package mappers

import (
	"context"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type SecurityPolicyToKuadrantEventMapper struct {
	opts MapperOptions
}

func NewSecurityPolicyToKuadrantEventMapper(o ...MapperOption) *SecurityPolicyToKuadrantEventMapper {
	return &SecurityPolicyToKuadrantEventMapper{opts: Apply(o...)}
}

func (m *SecurityPolicyToKuadrantEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	esp, ok := obj.(*egv1alpha1.SecurityPolicy)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a *egv1alpha1.SecurityPolicy", obj), "cannot map")
		return []reconcile.Request{}
	}

	if !kuadrant.IsKuadrantManaged(esp) {
		logger.V(1).Info("SecurityPolicy is not kuadrant managed", "securitypolicy", esp)
		return []reconcile.Request{}
	}

	kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(esp)
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
