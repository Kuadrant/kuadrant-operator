package gatewayapi

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type PolicyClass int

const (
	DirectPolicy PolicyClass = iota
	InheritedPolicy
)

type Policy interface {
	client.Object
	PolicyClass() PolicyClass
	GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference
	GetStatus() PolicyStatus
}

type PolicyStatus interface {
	GetConditions() []metav1.Condition
}

type PolicyByCreationTimestamp []Policy

func (a PolicyByCreationTimestamp) Len() int      { return len(a) }
func (a PolicyByCreationTimestamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PolicyByCreationTimestamp) Less(i, j int) bool {
	p1Time := ptr.To(a[i].GetCreationTimestamp())
	p2Time := ptr.To(a[j].GetCreationTimestamp())
	if !p1Time.Equal(p2Time) {
		return p1Time.Before(p2Time)
	}

	//  The policy appearing first in alphabetical order by "{namespace}/{name}".
	return client.ObjectKeyFromObject(a[i]).String() < client.ObjectKeyFromObject(a[j]).String()
}

type PolicyByTargetRefKindAndCreationTimeStamp []Policy

func (a PolicyByTargetRefKindAndCreationTimeStamp) Len() int      { return len(a) }
func (a PolicyByTargetRefKindAndCreationTimeStamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PolicyByTargetRefKindAndCreationTimeStamp) Less(i, j int) bool {
	targetRef1 := a[i].GetTargetRef()
	targetRef2 := a[j].GetTargetRef()

	// Compare kind first
	if targetRef1.Kind != targetRef2.Kind {
		if targetRef1.Kind == "Gateway" {
			return true
		} else if targetRef2.Kind == "HTTPRoute" {
			return false
		}
		return targetRef1.Kind < targetRef2.Kind
	}

	// Then compare timestamp
	p1Time := ptr.To(a[i].GetCreationTimestamp())
	p2Time := ptr.To(a[j].GetCreationTimestamp())
	if !p1Time.Equal(p2Time) {
		return p1Time.Before(p2Time)
	}

	//  The policy appearing first in alphabetical order by "{namespace}/{name}".
	return client.ObjectKeyFromObject(a[i]).String() < client.ObjectKeyFromObject(a[j]).String()
}

type PolicyByTargetRefKindAndAcceptedStatus []Policy

func (a PolicyByTargetRefKindAndAcceptedStatus) Len() int      { return len(a) }
func (a PolicyByTargetRefKindAndAcceptedStatus) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PolicyByTargetRefKindAndAcceptedStatus) Less(i, j int) bool {
	targetRef1 := a[i].GetTargetRef()
	targetRef2 := a[j].GetTargetRef()

	// Compare kind first
	if targetRef1.Kind != targetRef2.Kind {
		if targetRef1.Kind == "Gateway" {
			return true
		} else if targetRef2.Kind == "HTTPRoute" {
			return false
		}
		return targetRef1.Kind < targetRef2.Kind
	}

	// Compare by accepted condition
	p1Status := meta.IsStatusConditionTrue(a[i].GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	p2Status := meta.IsStatusConditionTrue(a[j].GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	if p1Status != p2Status {
		return p1Status
	}

	// Compare by creation timestamp
	p1Time := ptr.To(a[i].GetCreationTimestamp())
	p2Time := ptr.To(a[j].GetCreationTimestamp())
	if !p1Time.Equal(p2Time) {
		return p1Time.Before(p2Time)
	}

	//  The policy appearing first in alphabetical order by "{namespace}/{name}".
	return client.ObjectKeyFromObject(a[i]).String() < client.ObjectKeyFromObject(a[j]).String()
}
