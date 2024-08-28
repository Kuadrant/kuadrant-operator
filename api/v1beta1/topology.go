package v1beta1

import (
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	KuadrantResource = GroupVersion.WithResource("kuadrants")
	KuadrantKind     = schema.GroupKind{Group: GroupVersion.Group, Kind: "Kuadrant"}
)

var _ machinery.Object = &Kuadrant{}

func (p *Kuadrant) GetURL() string {
	return machinery.UrlFromObject(p)
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

func LinkKuadrantToTopologyConfigMap(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(KuadrantKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: KuadrantKind,
		To:   schema.GroupKind{Group: corev1.SchemeGroupVersion.Group, Kind: "ConfigMap"},
		Func: func(child machinery.Object) []machinery.Object {
			o := child.(*controller.RuntimeObject)
			cm := o.Object.(*corev1.ConfigMap)
			if _, found := cm.Labels["kuadrant.io/topology"]; found && cm.Name == "topology" {
				return kuadrants
			}
			return nil
		},
	}
}
