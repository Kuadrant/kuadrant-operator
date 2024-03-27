package gatewayapi

import (
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type Policy interface {
	client.Object
	GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference
}

type PolicyByCreationTimestamp []Policy

func (a PolicyByCreationTimestamp) Len() int      { return len(a) }
func (a PolicyByCreationTimestamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PolicyByCreationTimestamp) Less(i, j int) bool {
	p1Time := ptr.To(a[i].GetCreationTimestamp())
	p2Time := ptr.To(a[j].GetCreationTimestamp())
	if !p1Time.Equal(p2Time) {
		return ptr.To(a[i].GetCreationTimestamp()).Before(ptr.To(a[j].GetCreationTimestamp()))
	}

	//  The policy appearing first in alphabetical order by "{namespace}/{name}".
	return client.ObjectKeyFromObject(a[i]).String() < client.ObjectKeyFromObject(a[j]).String()
}
