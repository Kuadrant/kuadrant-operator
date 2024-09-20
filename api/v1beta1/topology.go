package v1beta1

import (
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	KuadrantResource  = GroupVersion.WithResource("kuadrants")
	KuadrantKind      = schema.GroupKind{Group: GroupVersion.Group, Kind: "Kuadrant"}
	AuthorinoResource = authorinov1beta1.GroupVersion.WithResource("authorinos")
	AuthorinoKind     = schema.GroupKind{Group: authorinov1beta1.GroupVersion.Group, Kind: "Authorino"}
)

var _ machinery.Object = &Kuadrant{}

func (p *Kuadrant) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

func LinkKuadrantToGatewayClasses(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantKind), controller.ObjectAs[*Kuadrant])

	return machinery.LinkFunc{
		From: KuadrantKind,
		To:   schema.GroupKind{Group: gwapiv1.GroupVersion.Group, Kind: "GatewayClass"},
		Func: func(_ machinery.Object) []machinery.Object {
			parents := make([]machinery.Object, len(kuadrants))
			for _, parent := range kuadrants {
				parents = append(parents, parent)
			}
			return parents
		},
	}
}

func LinkKuadrantToAuthorino(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantKind,
		To:   AuthorinoKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(kuadrant machinery.Object, _ int) bool {
				return kuadrant.GetNamespace() == child.GetNamespace()
			})
		},
	}
}
