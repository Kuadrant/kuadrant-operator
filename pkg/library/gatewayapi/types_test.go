//go:build unit

package gatewayapi

import (
	"reflect"
	"sort"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
}

var (
	_ GatewayAPIPolicy = &TestPolicy{}
)

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.TargetRef
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

func TestPolicyByCreationTimestamp(t *testing.T) {
	testCases := []struct {
		name           string
		policies       []GatewayAPIPolicy
		sortedPolicies []GatewayAPIPolicy
	}{
		{
			name:           "nil input",
			policies:       nil,
			sortedPolicies: nil,
		},
		{
			name:           "empty slices",
			policies:       make([]GatewayAPIPolicy, 0),
			sortedPolicies: make([]GatewayAPIPolicy, 0),
		},
		{
			name: "by creation date",
			policies: []GatewayAPIPolicy{
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
			sortedPolicies: []GatewayAPIPolicy{
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("ccc", time.Date(2020, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
		},
		{
			name: "by name when creation date are equal",
			policies: []GatewayAPIPolicy{
				createTestPolicy("ccc", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("bbb", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
				createTestPolicy("aaa", time.Date(2000, time.November, 10, 23, 0, 0, 0, time.UTC)),
			},
			sortedPolicies: []GatewayAPIPolicy{
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

func createTestPolicy(name string, creationTime time.Time) *TestPolicy {
	return &TestPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "testnamespace",
			Name:              name,
			CreationTimestamp: metav1.Time{creationTime},
		},
	}
}
