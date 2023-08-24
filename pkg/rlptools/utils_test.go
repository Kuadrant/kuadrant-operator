//go:build unit

package rlptools

import (
	"reflect"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
					Rates: []kuadrantv1beta2.Rate{
						{
							Limit:    5,
							Duration: 10,
							Unit:     kuadrantv1beta2.TimeUnit("second"),
						},
					},
				},
				"l2": {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
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
			Limits: map[string]kuadrantv1beta2.Limit{
				"l1": {
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

func TestLimitNameToLimitadorIdentifier(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *regexp.Regexp
	}{
		{
			name:     "prepends the limitador limit identifier prefix",
			input:    "foo",
			expected: regexp.MustCompile(`^limit\.foo.+`),
		},
		{
			name:     "sanitizes invalid chars",
			input:    "my/limit-0",
			expected: regexp.MustCompile(`^limit\.my_limit_0.+$`),
		},
		{
			name:     "sanitizes the dot char (.) even though it is a valid char in limitador identifiers",
			input:    "my.limit",
			expected: regexp.MustCompile(`^limit\.my_limit.+$`),
		},
		{
			name:     "appends a hash of the original name to avoid breaking uniqueness",
			input:    "foo",
			expected: regexp.MustCompile(`^.+__2c26b46b$`),
		},
		{
			name:     "empty string",
			input:    "",
			expected: regexp.MustCompile(`^limit.__e3b0c442$`),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			identifier := LimitNameToLimitadorIdentifier(tc.input)
			if !tc.expected.MatchString(identifier) {
				subT.Errorf("identifier does not match, expected(%s), got (%s)", tc.expected, identifier)
			}
		})
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
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
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
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
				},
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   3,
					Seconds:    3600,
					Conditions: []string{`limit.l2__8a1cee43 == "1"`},
					Variables:  []string{},
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
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
				},
				{
					Namespace:  "testNS/rlpA",
					MaxValue:   3,
					Seconds:    60,
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{},
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
					Conditions: []string{`limit.l1__2804bad6 == "1"`},
					Variables:  []string{"request.path"},
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
				if _, found := common.Find(tc.expected, func(expectedRateLimit limitadorv1alpha1.RateLimit) bool {
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

func TestRemoveRLPLabelsFromLimitadorList(t *testing.T) {
	policyKey := client.ObjectKey{Name: "test-RLP", Namespace: "test"}

	type args struct {
		limitadorList limitadorv1alpha1.LimitadorList
		policyKey     client.ObjectKey
	}
	tests := []struct {
		name    string
		args    args
		want    limitadorv1alpha1.LimitadorList
		wantErr bool
	}{
		{
			name:    "LimitadorList is empty",
			wantErr: false,
			want:    limitadorv1alpha1.LimitadorList{},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{},
				policyKey:     policyKey,
			},
		},
		{
			name:    "LimitadorList has one entry with no labels",
			wantErr: false,
			want:    limitadorv1alpha1.LimitadorList{},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{
					Items: []limitadorv1alpha1.Limitador{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:        "test1",
								Namespace:   "test",
								Annotations: map[string]string{"other": "label"},
							},
						},
					},
				},
				policyKey: policyKey,
			},
		},
		{
			name:    "LimitadorList is has two entries, second entry with RLP labels",
			wantErr: false,
			want: limitadorv1alpha1.LimitadorList{
				Items: []limitadorv1alpha1.Limitador{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "test2",
							Namespace:   "test",
							Annotations: map[string]string{"other": "label"},
						},
					},
				},
			},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{
					Items: []limitadorv1alpha1.Limitador{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:        "test1",
								Namespace:   "test",
								Annotations: map[string]string{"other": "label"},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test2",
								Namespace: "test",
								Annotations: map[string]string{
									"other": "label",
									common.RateLimitPoliciesBackRefAnnotation: "[{\"Name\": \"test-RLP\", \"Namespace\": \"test\"}]",
								},
							},
						},
					},
				},
				policyKey: policyKey,
			},
		},
		{
			name:    "LimitadorList is has three entries, first and second with RLP labels",
			wantErr: false,
			want: limitadorv1alpha1.LimitadorList{
				Items: []limitadorv1alpha1.Limitador{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "test1",
							Namespace:   "test",
							Annotations: map[string]string{"other": "label"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "test3",
							Namespace:   "test",
							Annotations: map[string]string{"other": "label"},
						},
					},
				},
			},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{
					Items: []limitadorv1alpha1.Limitador{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test1",
								Namespace: "test",
								Annotations: map[string]string{
									"other": "label",
									common.RateLimitPoliciesBackRefAnnotation: "[{\"Name\": \"test-RLP\", \"Namespace\": \"test\"}]",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:        "test2",
								Namespace:   "test",
								Annotations: map[string]string{"other": "label"},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test3",
								Namespace: "test",
								Annotations: map[string]string{
									"other": "label",
									common.RateLimitPoliciesBackRefAnnotation: "[{\"Name\": \"test-RLP\", \"Namespace\": \"test\"}]",
								},
							},
						},
					},
				},
				policyKey: policyKey,
			},
		},
		{
			name:    "LimitadorList, limitador CR had many RLP attached",
			wantErr: false,
			want: limitadorv1alpha1.LimitadorList{
				Items: []limitadorv1alpha1.Limitador{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test1",
							Namespace: "test",
							Annotations: map[string]string{
								"other": "label",
								common.RateLimitPoliciesBackRefAnnotation: "[{\"Namespace\":\"test\",\"Name\":\"other-RLP\"}]",
							},
						},
					},
				},
			},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{
					Items: []limitadorv1alpha1.Limitador{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test1",
								Namespace: "test",
								Annotations: map[string]string{
									"other": "label",
									common.RateLimitPoliciesBackRefAnnotation: "[{\"Name\": \"other-RLP\", \"Namespace\": \"test\"}, {\"Name\": \"test-RLP\", \"Namespace\": \"test\"}]",
								},
							},
						},
					},
				},
				policyKey: policyKey,
			},
		},
		{
			name:    "LimitadorList, get unmarshal error",
			wantErr: true,
			want:    limitadorv1alpha1.LimitadorList{},
			args: args{
				limitadorList: limitadorv1alpha1.LimitadorList{
					Items: []limitadorv1alpha1.Limitador{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test1",
								Namespace: "test",
								Annotations: map[string]string{
									"other": "label",
									common.RateLimitPoliciesBackRefAnnotation: "[{Name: other-RLP, Namespace: test}]",
								},
							},
						},
					},
				},
				policyKey: policyKey,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RemoveRLPLabelsFromLimitadorList(tt.args.limitadorList, tt.args.policyKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveRLPLabelsFromLimitadorList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RemoveRLPLabelsFromLimitadorList() got = %v, want %v", got, tt.want)
			}
		})
	}
}
