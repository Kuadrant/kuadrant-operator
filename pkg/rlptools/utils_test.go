//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func testRLP_1Limit_1Rate(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    5,
								Duration: 10,
								Unit:     "second",
							},
						},
					},
				},
			},
		},
	}
}

func testRLP_2Limits_1Rate(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    5,
								Duration: 10,
								Unit:     "second",
							},
						},
					},
					"l2": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    3,
								Duration: 1,
								Unit:     "hour",
							},
						},
					},
				},
			},
		},
	}
}

func testRLP_1Limit_2Rates(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": {
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    5,
								Duration: 10,
								Unit:     "second",
							},
							{
								Limit:    3,
								Duration: 1,
								Unit:     "minute",
							},
						},
					},
				},
			},
		},
	}
}

func testRLP_1Limit_1Rate_1Counter(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
	return &kuadrantv1beta2.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantv1beta2.RateLimitPolicySpec{
			RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
				Limits: map[string]kuadrantv1beta2.Limit{
					"l1": {
						Counters: []kuadrantv1beta2.ContextSelector{
							"request.path",
						},
						Rates: []kuadrantv1beta2.Rate{
							{
								Limit:    5,
								Duration: 10,
								Unit:     "second",
							},
						},
					},
				},
			},
		},
	}
}

func TestLimitadorRateLimitsFromRLP(t *testing.T) {
	testCases := []struct {
		name     string
		rlp      *kuadrantv1beta2.RateLimitPolicy
		expected []limitadorv1alpha1.RateLimit
	}{
		{
			name: "basic: 1 limit, 1 rate",
			rlp:  testRLP_1Limit_1Rate("testNS", "rlpA"),
			expected: []limitadorv1alpha1.RateLimit{
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`limit.l1__65f19ee8 == "1"`},
					Variables:  []string{},
					Name:       "testNS/rlpA",
				},
			},
		},
		{
			name: "multiple limits: 2 limits with 1 rate each",
			rlp:  testRLP_2Limits_1Rate("testNS", "rlpA"),
			expected: []limitadorv1alpha1.RateLimit{
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`limit.l1__65f19ee8 == "1"`},
					Variables:  []string{},
					Name:       "testNS/rlpA",
				},
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   3,
					Seconds:    3600,
					Conditions: []string{`limit.l2__3e871d60 == "1"`},
					Variables:  []string{},
					Name:       "testNS/rlpA",
				},
			},
		},
		{
			name: "multiple rates: 1 limit with 2 rates",
			rlp:  testRLP_1Limit_2Rates("testNS", "rlpA"),
			expected: []limitadorv1alpha1.RateLimit{
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`limit.l1__65f19ee8 == "1"`},
					Variables:  []string{},
					Name:       "testNS/rlpA",
				},
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   3,
					Seconds:    60,
					Conditions: []string{`limit.l1__65f19ee8 == "1"`},
					Variables:  []string{},
					Name:       "testNS/rlpA",
				},
			},
		},
		{
			name: "basic: 1 limit, 1 rate",
			rlp:  testRLP_1Limit_1Rate_1Counter("testNS", "rlpA"),
			expected: []limitadorv1alpha1.RateLimit{
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`limit.l1__65f19ee8 == "1"`},
					Variables:  []string{"request.path"},
					Name:       "testNS/rlpA",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			rateLimits := LimitadorRateLimitsFromRLP(tc.rlp)
			// Instead of sorting to compare, check len and then iterate
			if len(rateLimits) != len(tc.expected) {
				subT.Errorf("expected limits len (%d), got (%d)", len(tc.expected), len(rateLimits))
			}
			// When both slices have equal length, items can be checked one by one.
			for _, rl := range rateLimits {
				if _, found := utils.Find(tc.expected, func(expectedRateLimit limitadorv1alpha1.RateLimit) bool {
					return reflect.DeepEqual(rl, expectedRateLimit)
				}); !found {
					subT.Errorf("returned rate limit (%+v) not within expected ones, expected: %v", rl, tc.expected)
				}
			}
		})
	}
}

func TestConvertRateIntoSeconds(t *testing.T) {
	testCases := []struct {
		name             string
		rate             kuadrantv1beta2.Rate
		expectedMaxValue int
		expectedSeconds  int
	}{
		{
			name: "seconds",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2,
		},
		{
			name: "minutes",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("minute"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60,
		},
		{
			name: "hours",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("hour"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60 * 60,
		},
		{
			name: "day",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("day"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60 * 60 * 24,
		},
		{
			name: "negative limit",
			rate: kuadrantv1beta2.Rate{
				Limit: -5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("second"),
			},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name: "negative duration",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: -2, Unit: kuadrantv1beta2.TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name: "limit  is 0",
			rate: kuadrantv1beta2.Rate{
				Limit: 0, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("second"),
			},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name: "rate is 0",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 0, Unit: kuadrantv1beta2.TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name: "unexpected time unit",
			rate: kuadrantv1beta2.Rate{
				Limit: 5, Duration: 2, Unit: kuadrantv1beta2.TimeUnit("unknown"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			maxValue, seconds := rateToSeconds(tc.rate)
			if maxValue != tc.expectedMaxValue {
				subT.Errorf("maxValue does not match, expected(%d), got (%d)", tc.expectedMaxValue, maxValue)
			}
			if seconds != tc.expectedSeconds {
				subT.Errorf("seconds does not match, expected(%d), got (%d)", tc.expectedSeconds, seconds)
			}
		})
	}
}
