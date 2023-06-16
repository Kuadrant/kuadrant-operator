//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rlp_1limit_1rate(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": kuadrantv1beta2.Limit{
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    5,
							Duration: 10,
							Unit:     kuadrantv1beta2.TimeUnit("second"),
						},
					},
				},
			},
		},
	}
}

func rlp_2limit_1rate(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": kuadrantv1beta2.Limit{
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    5,
							Duration: 10,
							Unit:     kuadrantv1beta2.TimeUnit("second"),
						},
					},
				},
				"l2": kuadrantv1beta2.Limit{
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    3,
							Duration: 1,
							Unit:     kuadrantv1beta2.TimeUnit("hour"),
						},
					},
				},
			},
		},
	}
}

func rlp_1limit_2rate(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": kuadrantv1beta2.Limit{
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    5,
							Duration: 10,
							Unit:     kuadrantv1beta2.TimeUnit("second"),
						},
						{
							Limit:    3,
							Duration: 1,
							Unit:     kuadrantv1beta2.TimeUnit("minute"),
						},
					},
				},
			},
		},
	}
}

func rlp_1limit_1rate_1counter(ns, name string) *kuadrantv1beta2.RateLimitPolicy {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": kuadrantv1beta2.Limit{
					Counters: []kuadrantv1beta2.ContextSelector{
						kuadrantv1beta2.ContextSelector("request.path"),
					},
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    5,
							Duration: 10,
							Unit:     kuadrantv1beta2.TimeUnit("second"),
						},
					},
				},
			},
		},
	}
}

func TestReadLimitsFromRLP(t *testing.T) {
	testCases := []struct {
		name           string
		rlp            *kuadrantv1beta2.RateLimitPolicy
		expectedLimits []Limit
	}{
		{
			"basic: 1 limit, 1 rate", rlp_1limit_1rate("testNS", "rlpA"), []Limit{
				{
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`testNS/rlpA/l1 == "1"`},
					Variables:  nil,
				},
			},
		},
		{
			"multiple limits: 2 limits with 1 rate each", rlp_2limit_1rate("testNS", "rlpA"), []Limit{
				{
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`testNS/rlpA/l1 == "1"`},
					Variables:  nil,
				},
				{
					MaxValue:   3,
					Seconds:    3600,
					Conditions: []string{`testNS/rlpA/l2 == "1"`},
					Variables:  nil,
				},
			},
		},
		{
			"multiple rates: 1 limit with 2 rates", rlp_1limit_2rate("testNS", "rlpA"), []Limit{
				{
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`testNS/rlpA/l1 == "1"`},
					Variables:  nil,
				},
				{
					MaxValue:   3,
					Seconds:    60,
					Conditions: []string{`testNS/rlpA/l1 == "1"`},
					Variables:  nil,
				},
			},
		},
		{
			"counters: 1 limit with 1 rate and 1 counter", rlp_1limit_1rate_1counter("testNS", "rlpA"), []Limit{
				{
					MaxValue:   5,
					Seconds:    10,
					Conditions: []string{`testNS/rlpA/l1 == "1"`},
					Variables:  []string{"request.path"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			limits := ReadLimitsFromRLP(tc.rlp)
			// Instead of sorting to compare, check len and then iterate
			if len(limits) != len(tc.expectedLimits) {
				subT.Errorf("expected limits len (%d), got (%d)", len(tc.expectedLimits), len(limits))
			}
			// When both slices have equal length, items can be checked one by one.
			for idx := range limits {
				_, ok := common.Find(tc.expectedLimits, func(expectedLimit Limit) bool {
					return reflect.DeepEqual(limits[idx], expectedLimit)
				})

				if !ok {
					subT.Errorf("returned limit (%+v) not in expected ones", limits[idx])
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
			maxValue, seconds := ConvertRateIntoSeconds(tc.rate)
			if maxValue != tc.expectedMaxValue {
				subT.Errorf("maxValue does not match, expected(%d), got (%d)", tc.expectedMaxValue, maxValue)
			}
			if seconds != tc.expectedSeconds {
				subT.Errorf("seconds does not match, expected(%d), got (%d)", tc.expectedSeconds, seconds)
			}
		})
	}
}
