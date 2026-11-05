//go:build unit

package controllers

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func TestLimitadorLimitsModifiedPolicies(t *testing.T) {
	owner := func(policy machinery.Policy) limitOwner {
		o, ok := newLimitOwner(policy)
		if !ok {
			t.Fatalf("%s is not a rate limiting policy", policy.GetLocator())
		}
		return o
	}
	rlp := func(name string) limitOwner {
		return owner(&kuadrantv1.RateLimitPolicy{
			TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1.RateLimitPolicyGroupKind.Kind, APIVersion: kuadrantv1.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		})
	}
	policyA := rlp("a")
	policyB := rlp("b")
	policyT := owner(&kuadrantv1alpha1.TokenRateLimitPolicy{
		TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1alpha1.TokenRateLimitPolicyGroupKind.Kind, APIVersion: kuadrantv1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "t"},
	})
	deletedPolicy := rlp("deleted")
	livePolicies := []limitOwner{policyA, policyB, policyT}

	limit := func(owner limitOwner, limitName, limitsNamespace string, maxValue int) limitadorv1alpha1.RateLimit {
		return limitadorv1alpha1.RateLimit{
			Name:       limitName,
			Namespace:  limitsNamespace,
			MaxValue:   maxValue,
			Seconds:    60,
			Conditions: []string{fmt.Sprintf(`descriptors[0]["%s"] == "1"`, owner.identifier(owner.key, limitName))},
			Variables:  []string{},
		}
	}

	type ownedLimit struct {
		owner limitOwner
		limit limitadorv1alpha1.RateLimit
	}

	testCases := []struct {
		name     string
		existing []limitadorv1alpha1.RateLimit
		desired  []ownedLimit
		expected []string
	}{
		{
			name:     "no change",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{},
		},
		{
			name:     "rate of one policy changed",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 20)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyA.locator},
		},
		{
			name:     "new policy",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10)},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyB.locator},
		},
		{
			name:     "policy limit applied to another route",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 10)}, {policyA, limit(policyA, "l1", "ns/route-c", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyA.locator},
		},
		{
			name: "stale rate left under the same limit",
			existing: []limitadorv1alpha1.RateLimit{
				limit(policyA, "l1", "ns/route-a", 10),
				func() limitadorv1alpha1.RateLimit {
					l := limit(policyA, "l1", "ns/route-a", 100)
					l.Seconds = 3600
					return l
				}(),
				limit(policyB, "l1", "ns/route-b", 10),
			},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyA.locator},
		},
		{
			name:     "limit dropped by a policy that keeps others",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyA, "l2", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyA, limit(policyA, "l1", "ns/route-a", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyA.locator},
		},
		{
			name:     "rates of the only limit of a policy cleared",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyA.locator},
		},
		{
			name:     "rates of the only token limit of a policy cleared",
			existing: []limitadorv1alpha1.RateLimit{limit(policyT, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyT.locator},
		},
		{
			name:     "token limit dropped by a policy that keeps others",
			existing: []limitadorv1alpha1.RateLimit{limit(policyT, "l1", "ns/route-a", 10), limit(policyT, "l2", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyT, limit(policyT, "l1", "ns/route-a", 10)}, {policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{policyT.locator},
		},
		{
			name:     "limits of a deleted policy removed",
			existing: []limitadorv1alpha1.RateLimit{limit(deletedPolicy, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10)},
			desired:  []ownedLimit{{policyB, limit(policyB, "l1", "ns/route-b", 10)}},
			expected: []string{},
		},
		{
			name:     "several policies changed",
			existing: []limitadorv1alpha1.RateLimit{limit(policyA, "l1", "ns/route-a", 10), limit(policyB, "l1", "ns/route-b", 10), limit(deletedPolicy, "l1", "ns/route-d", 10)},
			desired:  []ownedLimit{{policyB, limit(policyB, "l1", "ns/route-b", 20)}, {policyA, limit(policyA, "l1", "ns/route-a", 20)}, {policyT, limit(policyT, "l1", "ns/route-t", 10)}},
			expected: []string{policyA.locator, policyB.locator, policyT.locator},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			owners := map[string]string{}
			desired := make([]limitadorv1alpha1.RateLimit, 0, len(tc.desired))
			for _, d := range tc.desired {
				addLimitOwner(owners, []limitadorv1alpha1.RateLimit{d.limit}, d.owner.locator)
				desired = append(desired, d.limit)
			}

			if got := limitadorLimitsModifiedPolicies(tc.existing, desired, owners, livePolicies); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestNewLimitOwner(t *testing.T) {
	objectMeta := metav1.ObjectMeta{Namespace: "ns", Name: "p"}
	policyKey := k8stypes.NamespacedName{Namespace: "ns", Name: "p"}

	testCases := []struct {
		name               string
		policy             machinery.Policy
		expectedIdentifier string
	}{
		{
			name: "rate limit policy",
			policy: &kuadrantv1.RateLimitPolicy{
				TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1.RateLimitPolicyGroupKind.Kind, APIVersion: kuadrantv1.GroupVersion.String()},
				ObjectMeta: objectMeta,
			},
			expectedIdentifier: LimitNameToLimitadorIdentifier(policyKey, "l1"),
		},
		{
			name: "token rate limit policy",
			policy: &kuadrantv1alpha1.TokenRateLimitPolicy{
				TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1alpha1.TokenRateLimitPolicyGroupKind.Kind, APIVersion: kuadrantv1alpha1.GroupVersion.String()},
				ObjectMeta: objectMeta,
			},
			expectedIdentifier: TokenLimitNameToLimitadorIdentifier(policyKey, "l1"),
		},
		{
			name: "auth policy",
			policy: &kuadrantv1.AuthPolicy{
				TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1.AuthPolicyGroupKind.Kind, APIVersion: kuadrantv1.GroupVersion.String()},
				ObjectMeta: objectMeta,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			owner, ok := newLimitOwner(tc.policy)
			if ok != (tc.expectedIdentifier != "") {
				t.Fatalf("expected limit owner %t, got %t", tc.expectedIdentifier != "", ok)
			}
			if !ok {
				return
			}
			if owner.locator != tc.policy.GetLocator() {
				t.Errorf("expected locator %s, got %s", tc.policy.GetLocator(), owner.locator)
			}
			if got := owner.identifier(owner.key, "l1"); got != tc.expectedIdentifier {
				t.Errorf("expected identifier %s, got %s", tc.expectedIdentifier, got)
			}
		})
	}
}

func TestIsLimitadorLimitsModified(t *testing.T) {
	policy := &kuadrantv1.RateLimitPolicy{
		TypeMeta:   metav1.TypeMeta{Kind: kuadrantv1.RateLimitPolicyGroupKind.Kind, APIVersion: kuadrantv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "a"},
	}

	testCases := []struct {
		name     string
		state    func() *sync.Map
		expected bool
	}{
		{
			name:     "limits not reconciled in this pass",
			state:    func() *sync.Map { return &sync.Map{} },
			expected: false,
		},
		{
			name: "limits of the policy modified",
			state: func() *sync.Map {
				state := &sync.Map{}
				state.Store(StateLimitadorLimitsModified, []string{"ratelimitpolicy.kuadrant.io:ns/c", policy.GetLocator()})
				return state
			},
			expected: true,
		},
		{
			name: "limits of another policy modified",
			state: func() *sync.Map {
				state := &sync.Map{}
				state.Store(StateLimitadorLimitsModified, []string{"ratelimitpolicy.kuadrant.io:ns/b"})
				return state
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLimitadorLimitsModified(tc.state(), policy); got != tc.expected {
				t.Errorf("expected %t, got %t", tc.expected, got)
			}
		})
	}
}
