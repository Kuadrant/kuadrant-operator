//go:build unit

package gatewayapi

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var (
	_ Policy       = &TestPolicy{}
	_ PolicyStatus = &FakePolicyStatus{}
)

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
	Status    FakePolicyStatus                         `json:"status"`
}

func (p *TestPolicy) Kind() string {
	return "FakePolicy"
}

func (p *TestPolicy) List(ctx context.Context, c client.Client, namespace string) []Policy {
	return nil
}

func (p *TestPolicy) BackReferenceAnnotationName() string {
	return ""
}

func (p *TestPolicy) DirectReferenceAnnotationName() string {
	return ""
}

func (p *TestPolicy) PolicyClass() PolicyClass {
	return DirectPolicy
}

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.TargetRef
}

func (p *TestPolicy) GetStatus() PolicyStatus {
	return &p.Status
}

func (p *TestPolicy) DeepCopyObject() runtime.Object {
	if c := p.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (p *TestPolicy) DeepCopy() *TestPolicy {
	if p == nil {
		return nil
	}
	out := new(TestPolicy)
	p.DeepCopyInto(out)
	return out
}

func (p *TestPolicy) DeepCopyInto(out *TestPolicy) {
	*out = *p
	out.TypeMeta = p.TypeMeta
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.TargetRef.DeepCopyInto(&out.TargetRef)
}

type FakePolicyStatus struct {
	Conditions []metav1.Condition
}

func (s *FakePolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

func TestPolicyByCreationTimestamp(t *testing.T) {
	testCases := []struct {
		name           string
		policies       []Policy
		sortedPolicies []Policy
	}{
		{
			name:           "nil input",
			policies:       nil,
			sortedPolicies: nil,
		},
		{
			name:           "empty slices",
			policies:       make([]Policy, 0),
			sortedPolicies: make([]Policy, 0),
		},
		{
			name: "by creation date",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
		},
		{
			name: "by name when creation date are equal",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("ccc", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			sort.Sort(PolicyByCreationTimestamp(tc.policies))
			if !reflect.DeepEqual(tc.policies, tc.sortedPolicies) {
				subT.Errorf("expected=%v; got=%v", tc.sortedPolicies, tc.policies)
			}
		})
	}
}

func TestPolicyByTargetRef(t *testing.T) {
	testCases := []struct {
		name           string
		policies       []Policy
		sortedPolicies []Policy
	}{
		{
			name:           "nil input",
			policies:       nil,
			sortedPolicies: nil,
		},
		{
			name:           "empty slices",
			policies:       make([]Policy, 0),
			sortedPolicies: make([]Policy, 0),
		},
		{
			name: "by kind, and creation date",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute")),
				createTestPolicy("aaa", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("ddd", time.Date(2005, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service")),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute")),
				createTestPolicy("ddd", time.Date(2005, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service")),
			},
		},
		{
			name: "by kind, and then name when creation date equal",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("bbb", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute")),
				createTestPolicy("aaa", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("ddd", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service")),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway")),
				createTestPolicy("bbb", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute")),
				createTestPolicy("ddd", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			sort.Sort(PolicyByTargetRefKindAndCreationTimeStamp(tc.policies))
			if !reflect.DeepEqual(tc.policies, tc.sortedPolicies) {
				subT.Errorf("expected=%v; got=%v", tc.sortedPolicies, tc.policies)
			}
		})
	}
}

func TestPolicyByTargetRefAndStatus(t *testing.T) {
	testCases := []struct {
		name           string
		policies       []Policy
		sortedPolicies []Policy
	}{
		{
			name:           "nil input",
			policies:       nil,
			sortedPolicies: nil,
		},
		{
			name:           "empty slices",
			policies:       make([]Policy, 0),
			sortedPolicies: make([]Policy, 0),
		},
		{
			name: "by kind, accepted status, and creation date",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("aaa", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("ddd", time.Date(2005, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("fff", time.Date(2003, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("eee", time.Date(2008, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("fff", time.Date(2003, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("eee", time.Date(2008, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("ddd", time.Date(2005, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service"), withAcceptedStatus(metav1.ConditionTrue)),
			},
		},
		{
			name: "by kind, accepted status, creation date and then name when creation date equal",
			policies: []Policy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("bbb", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("aaa", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("ddd", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("eee", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("fff", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("ggg", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
			},
			sortedPolicies: []Policy{
				createTestPolicy("aaa", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Gateway"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("eee", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("fff", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionTrue)),
				createTestPolicy("bbb", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("ggg", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("HTTPRoute"), withAcceptedStatus(metav1.ConditionFalse)),
				createTestPolicy("ddd", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC), withTargetRefKind("Service"), withAcceptedStatus(metav1.ConditionTrue)),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			sort.Sort(PolicyByTargetRefKindAndAcceptedStatus(tc.policies))
			if !reflect.DeepEqual(tc.policies, tc.sortedPolicies) {
				subT.Errorf("expected=%v; got=%v; diff=%v", tc.sortedPolicies, tc.policies, cmp.Diff(tc.policies, tc.sortedPolicies))
			}
		})
	}
}

func createTestPolicy(name string, creationTime time.Time, mutateFn ...func(p *TestPolicy)) *TestPolicy {
	p := &TestPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "testnamespace",
			Name:              name,
			CreationTimestamp: metav1.Time{Time: creationTime},
		},
	}
	for _, fn := range mutateFn {
		fn(p)
	}

	return p
}

func withTargetRefKind(targetRefKind string) func(p *TestPolicy) {
	return func(p *TestPolicy) {
		p.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{Kind: gatewayapiv1.Kind(targetRefKind)}
	}
}

func withAcceptedStatus(status metav1.ConditionStatus) func(p *TestPolicy) {
	return func(p *TestPolicy) {
		meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{Type: string(gatewayapiv1alpha2.PolicyConditionAccepted), Status: status, LastTransitionTime: metav1.Time{Time: time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC)}})
	}
}
