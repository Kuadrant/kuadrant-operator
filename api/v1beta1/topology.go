package v1beta1

import (
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	KuadrantResource = GroupVersion.WithResource("kuadrants")
	KuadrantKind     = schema.GroupKind{Group: GroupVersion.Group, Kind: "Kuadrant"}
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
