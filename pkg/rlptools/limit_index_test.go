//go:build unit

package rlptools

import (
	"os"
	"reflect"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestLimitIndexEquals(t *testing.T) {
	logger := log.NewLogger(
		log.WriteTo(os.Stdout),
		log.SetLevel(log.DebugLevel),
	)
	t.Run("nil indexes are equal", func(subT *testing.T) {
		idxA := NewLimitadorIndex(nil, logger)
		idxB := NewLimitadorIndex(nil, logger)

		if !idxA.Equals(idxB) {
			subT.Fatal("nil indexes are not equal")
		}
	})
	t.Run("empty indexes are equal", func(subT *testing.T) {
		idxA := NewLimitadorIndex(emptyLimitador(), logger)
		idxB := NewLimitadorIndex(emptyLimitador(), logger)

		if !idxA.Equals(idxB) {
			subT.Fatal("nil indexes are not equal")
		}
	})

	// Rate limit order does not matter
	// check the order does not matter when limit differ in
	// maxValue, seconds, namespace, conditions, variables
	testCases := []struct {
		name      string
		limitador *limitadorv1alpha1.Limitador
	}{
		{
			"rate limit order does not matter: diff maxvalue",
			limitadorWithMultipleLimitsMaxValue(),
		},
		{
			"rate limit order does not matter: diff seconds",
			limitadorWithMultipleLimitsSeconds(),
		},
		{
			"rate limit order does not matter: diff namespace",
			limitadorWithMultipleLimitsNamespace(),
		},
		{
			"rate limit order does not matter: diff conditions",
			limitadorWithMultipleLimitsConditions(),
		},
		{
			"rate limit order does not matter: diff variables",
			limitadorWithMultipleLimitsVariables(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			limitadorA := tc.limitador
			idxA := NewLimitadorIndex(limitadorA, logger)
			reversedLimitadorA := *limitadorA
			reversedLimitadorA.Spec.Limits = common.ReverseSlice(limitadorA.Spec.Limits)

			idxB := NewLimitadorIndex(&reversedLimitadorA, logger)

			if !idxA.Equals(idxB) {
				subT.Fatal("indexes with reversed limits are not equal")
			}
		})
	}

	t.Run("limit conditions order does not matter", func(subT *testing.T) {
		limitadorA := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: []string{"a", "b"},
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  make([]string, 0),
					},
				},
			},
		}
		idxA := NewLimitadorIndex(limitadorA, logger)

		limitadorB := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "nsB"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: []string{"b", "a"}, // reverse order regarding limitadorA
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  make([]string, 0),
					},
				},
			},
		}
		idxB := NewLimitadorIndex(limitadorB, logger)

		if !idxA.Equals(idxB) {
			subT.Fatal("indexes with limits with reversed conditions are not equal")
		}
	})

	t.Run("limit variables order does not matter", func(subT *testing.T) {
		limitadorA := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: make([]string, 0),
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  []string{"a", "b"},
					},
				},
			},
		}
		idxA := NewLimitadorIndex(limitadorA, logger)

		limitadorB := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "nsB"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: make([]string, 0),
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  []string{"b", "a"}, // reverse order regarding limitadorA
					},
				},
			},
		}
		idxB := NewLimitadorIndex(limitadorB, logger)

		if !idxA.Equals(idxB) {
			subT.Fatal("indexes with limits with reversed variables are not equal")
		}
	})

	t.Run("nil or empty array does not matter", func(subT *testing.T) {
		limitadorWithNilVariablesAndConditions := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: nil,
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  nil,
					},
				},
			},
		}
		idxA := NewLimitadorIndex(limitadorWithNilVariablesAndConditions, logger)

		limitadorWithEmptyVariablesAndConditions := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: []string{},
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  []string{},
					},
				},
			},
		}
		idxB := NewLimitadorIndex(limitadorWithEmptyVariablesAndConditions, logger)

		if !idxA.Equals(idxB) {
			subT.Fatal("indexes with nil and empty arrays variables and conditions are not equal")
		}
	})
}

func TestLimitIndexToLimits(t *testing.T) {
	logger := log.NewLogger(
		log.WriteTo(os.Stdout),
		log.SetLevel(log.DebugLevel),
	)

	t.Run("nil index return empty list", func(subT *testing.T) {
		idx := NewLimitadorIndex(nil, logger)

		limits := idx.ToLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("empty index return empty list", func(subT *testing.T) {
		idx := NewLimitadorIndex(emptyLimitador(), logger)

		limits := idx.ToLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("converting one limit index", func(subT *testing.T) {
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: []string{"c_a", "c_b", "c_c"},
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  []string{"v_a", "v_b", "v_c"},
					},
				},
			},
		}
		idx := NewLimitadorIndex(limitador, logger)

		limits := idx.ToLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 1 {
			subT.Fatal("it should return one limit")
		}

		if !reflect.DeepEqual(limits[0], limitador.Spec.Limits[0]) {
			subT.Fatal("limit does not match")
		}
	})
	t.Run("converting limits with nil variables", func(subT *testing.T) {
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: []string{"c_a", "c_b", "c_c"},
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  nil,
					},
				},
			},
		}
		idx := NewLimitadorIndex(limitador, logger)

		limits := idx.ToLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 1 {
			subT.Fatal("it should return one limit")
		}

		expectedLimit := limitador.Spec.Limits[0].DeepCopy()
		// expected limitador limit should not have nil variables
		expectedLimit.Variables = make([]string, 0)
		if !reflect.DeepEqual(limits[0], *expectedLimit) {
			subT.Fatal("limit does not match")
		}
	})

	t.Run("converting limits with nil conditions", func(subT *testing.T) {
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						Conditions: nil,
						MaxValue:   1,
						Namespace:  limitadorNamespaceA(),
						Seconds:    1,
						Variables:  []string{"v_a", "v_b", "v_c"},
					},
				},
			},
		}
		idx := NewLimitadorIndex(limitador, logger)

		limits := idx.ToLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 1 {
			subT.Fatal("it should return one limit")
		}

		expectedLimit := limitador.Spec.Limits[0].DeepCopy()
		// expected limitador limit should not have nil conditions
		expectedLimit.Conditions = make([]string, 0)
		if !reflect.DeepEqual(limits[0], *expectedLimit) {
			subT.Fatal("limit does not match")
		}
	})
}

func TestLimitIndexAddLimit(t *testing.T) {
	logger := log.NewLogger(
		log.WriteTo(os.Stdout),
		log.SetLevel(log.DebugLevel),
	)

	var (
		gwKey      = client.ObjectKey{Name: "gwA", Namespace: "nsA"}
		domain     = "a.com"
		maxValue   = 2
		seconds    = 19
		conditions = []string{"a", "b"}
		variables  = []string{"c", "d"}
	)

	limit := &kuadrantv1beta1.Limit{
		Conditions: conditions, MaxValue: maxValue, Seconds: seconds, Variables: variables,
	}

	idx := NewLimitadorIndex(emptyLimitador(), logger)
	idx.AddLimit(gwKey, domain, limit)
	limits := idx.ToLimits()
	if limits == nil {
		t.Fatal("returns nil")
	}
	if len(limits) != 1 {
		t.Fatal("it should return one limit")
	}

	expectedLimit := limitadorv1alpha1.RateLimit{
		Conditions: conditions,
		MaxValue:   maxValue,
		Namespace:  common.MarshallNamespace(gwKey, domain),
		Seconds:    seconds,
		Variables:  variables,
	}
	if !reflect.DeepEqual(limits[0], expectedLimit) {
		t.Fatal("limit does not match")
	}
}

func TestLimitIndexDeleteGateway(t *testing.T) {
	logger := log.NewLogger(
		log.WriteTo(os.Stdout),
		log.SetLevel(log.DebugLevel),
	)

	t.Run("delete gateway in nil index is noop", func(subT *testing.T) {
		idx := NewLimitadorIndex(nil, logger)

		gwKey := client.ObjectKey{Name: "gwA", Namespace: "nsA"}
		idx.DeleteGateway(gwKey)

		limits := idx.ToLimits()
		if limits == nil {
			t.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("delete gateway in empty index is noop", func(subT *testing.T) {
		idx := NewLimitadorIndex(emptyLimitador(), logger)

		gwKey := client.ObjectKey{Name: "gwA", Namespace: "nsA"}
		idx.DeleteGateway(gwKey)

		limits := idx.ToLimits()
		if limits == nil {
			t.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("deleting missing gateway is noop", func(subT *testing.T) {
		var (
			domain = "a.com"
			gwKeyA = client.ObjectKey{Name: "gwA", Namespace: "nsA"}
			gwKeyB = client.ObjectKey{Name: "gwB", Namespace: "nsB"}
		)
		// has one limit with gwKeyA
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						MaxValue:  1,
						Namespace: common.MarshallNamespace(gwKeyA, domain),
						Seconds:   1,
					},
				},
			},
		}

		idx := NewLimitadorIndex(limitador, logger)

		// delete some gateway that does not exist in the index
		idx.DeleteGateway(gwKeyB)

		limits := idx.ToLimits()
		if limits == nil {
			t.Fatal("returns nil")
		}
		if len(limits) != 1 {
			subT.Fatal("it was expected to be one limit and none was deleted")
		}
	})

	t.Run("index has one limit with the given gw key", func(subT *testing.T) {
		var (
			domain = "a.com"
			gwKeyA = client.ObjectKey{Name: "gwA", Namespace: "nsA"}
		)
		// has one limit with gwKeyA
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						MaxValue:  1,
						Namespace: common.MarshallNamespace(gwKeyA, domain),
						Seconds:   1,
					},
				},
			},
		}

		idx := NewLimitadorIndex(limitador, logger)

		idx.DeleteGateway(gwKeyA)

		limits := idx.ToLimits()
		if limits == nil {
			t.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("it was expected to be no limit")
		}
	})

	t.Run("delete gateway does not delete more than necessary", func(subT *testing.T) {
		var (
			domain = "a.com"
			gwKeyA = client.ObjectKey{Name: "gwA", Namespace: "nsA"}
			gwKeyB = client.ObjectKey{Name: "gwB", Namespace: "nsB"}
		)
		// has three limits: two from gwKeyA and one from gwKeyB
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind: "Limitador", APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Limits: []limitadorv1alpha1.RateLimit{
					{
						MaxValue:  1,
						Namespace: common.MarshallNamespace(gwKeyA, domain),
						Seconds:   1,
					},
					{
						MaxValue:  2,
						Namespace: common.MarshallNamespace(gwKeyA, domain),
						Seconds:   2,
					},
					{
						MaxValue:  1,
						Namespace: common.MarshallNamespace(gwKeyB, domain),
						Seconds:   1,
					},
				},
			},
		}

		idx := NewLimitadorIndex(limitador, logger)

		idx.DeleteGateway(gwKeyA)

		limits := idx.ToLimits()
		if limits == nil {
			t.Fatal("returns nil")
		}
		if len(limits) != 1 {
			subT.Fatal("it was expected to be one limit")
		}
	})
}

func limitadorNamespaceA() string {
	return common.MarshallNamespace(client.ObjectKey{Name: "gwA", Namespace: "nsA"}, "a.com")
}

func limitadorNamespaceB() string {
	return common.MarshallNamespace(client.ObjectKey{Name: "gwB", Namespace: "nsB"}, "b.com")
}

func emptyLimitador() *limitadorv1alpha1.Limitador {
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: nil,
		},
	}
}

func limitadorWithMultipleLimitsMaxValue() *limitadorv1alpha1.Limitador {
	// limits differ in maxValue
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: []limitadorv1alpha1.RateLimit{
				{
					Conditions: nil,
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  nil,
				},
				{
					Conditions: nil,
					MaxValue:   2,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  nil,
				},
			},
		},
	}
}

func limitadorWithMultipleLimitsSeconds() *limitadorv1alpha1.Limitador {
	// limits differ in seconds
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: []limitadorv1alpha1.RateLimit{
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  make([]string, 0),
				},
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    2,
					Variables:  make([]string, 0),
				},
			},
		},
	}
}

func limitadorWithMultipleLimitsNamespace() *limitadorv1alpha1.Limitador {
	// limits differ in namespace
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: []limitadorv1alpha1.RateLimit{
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  make([]string, 0),
				},
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceB(),
					Seconds:    1,
					Variables:  make([]string, 0),
				},
			},
		},
	}
}

func limitadorWithMultipleLimitsConditions() *limitadorv1alpha1.Limitador {
	// limits differ in conditions
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: []limitadorv1alpha1.RateLimit{
				{
					Conditions: []string{"a"},
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  make([]string, 0),
				},
				{
					Conditions: []string{"b"},
					MaxValue:   1,
					Namespace:  limitadorNamespaceB(),
					Seconds:    1,
					Variables:  make([]string, 0),
				},
			},
		},
	}
}

func limitadorWithMultipleLimitsVariables() *limitadorv1alpha1.Limitador {
	// limits differ in variables
	return &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "nsA"},
		Spec: limitadorv1alpha1.LimitadorSpec{
			Limits: []limitadorv1alpha1.RateLimit{
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceA(),
					Seconds:    1,
					Variables:  []string{"a"},
				},
				{
					Conditions: make([]string, 0),
					MaxValue:   1,
					Namespace:  limitadorNamespaceB(),
					Seconds:    1,
					Variables:  []string{"b"},
				},
			},
		},
	}
}
