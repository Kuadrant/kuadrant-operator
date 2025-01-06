//go:build unit

package ratelimit

import (
	"reflect"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

func TestIndexSet(t *testing.T) {
	t.Run("index rate limits to a key", func(subT *testing.T) {
		index := NewIndex()

		index.Set("foo", []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 10, Seconds: 1},
			{Namespace: "ns/rlp-1", MaxValue: 100, Seconds: 60},
			{Namespace: "ns/rlp-1", MaxValue: 1000, Seconds: 1},
		})

		aggregatedRateLimits := index.ToRateLimits()
		expectedCount := 3
		if len(aggregatedRateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits, returned:", len(aggregatedRateLimits))
		}
	})

	t.Run("index rate limits to different keys", func(subT *testing.T) {
		index := NewIndex()

		index.Set("foo", []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 10, Seconds: 1},
			{Namespace: "ns/rlp-1", MaxValue: 100, Seconds: 60},
			{Namespace: "ns/rlp-1", MaxValue: 1000, Seconds: 1},
		})

		index.Set("bar", []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-2", MaxValue: 50, Seconds: 1},
		})

		key := "foo"
		rateLimits, found := index.Get(key)
		if !found {
			subT.Fatal("expected rate limits to be indexed to key but none found:", key)
		}
		expectedCount := 3
		if len(rateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits for key", key, ", returned:", len(rateLimits))
		}

		key = "bar"
		rateLimits, found = index.Get(key)
		if !found {
			subT.Fatal("expected rate limits to be indexed to key but none found:", key)
		}
		expectedCount = 1
		if len(rateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits for key", key, ", returned:", len(rateLimits))
		}

		aggregatedRateLimits := index.ToRateLimits()
		expectedCount = 4
		if len(aggregatedRateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits in total, returned:", len(aggregatedRateLimits))
		}
	})

	t.Run("reset rate limits for an existing key", func(subT *testing.T) {
		index := NewIndex()

		index.Set("foo", []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 10, Seconds: 1},
			{Namespace: "ns/rlp-1", MaxValue: 100, Seconds: 60},
			{Namespace: "ns/rlp-1", MaxValue: 1000, Seconds: 1},
		})

		index.Set("foo", []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 500, Seconds: 3600},
		})

		aggregatedRateLimits := index.ToRateLimits()
		expectedCount := 1
		if len(aggregatedRateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits, returned:", len(aggregatedRateLimits))
		}
		if !reflect.DeepEqual(aggregatedRateLimits[0], limitadorv1alpha1.RateLimit{Namespace: "ns/rlp-1", MaxValue: 500, Seconds: 3600}) {
			subT.Fatal("expected rate limit to be equal to the last one set")
		}
	})

	t.Run("add an empty list of limits if a noop", func(subT *testing.T) {
		idx := NewIndex()

		idx.Set("foo", []limitadorv1alpha1.RateLimit{})

		aggregatedRateLimits := idx.ToRateLimits()
		if len(aggregatedRateLimits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("add nil list of limits if a noop", func(subT *testing.T) {
		idx := NewIndex()

		idx.Set("foo", []limitadorv1alpha1.RateLimit{})

		aggregatedRateLimits := idx.ToRateLimits()
		if len(aggregatedRateLimits) != 0 {
			subT.Fatal("returns not empty")
		}
	})
}

func TestIndexToRateLimits(t *testing.T) {
	t.Run("nil index return empty list", func(subT *testing.T) {
		idx := NewIndex()

		limits := idx.ToRateLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("empty index return empty list", func(subT *testing.T) {
		idx := NewIndex()

		limits := idx.ToRateLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})
}

func TestEqualsTo(t *testing.T) {
	global_l0 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l0",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l1 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l1",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l2 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l2",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l3 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l3",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l4 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l4",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l5 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l5",
		Seconds:    10,
		Variables:  []string{},
	}
	global_l6 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._global___3f2bfd8b == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l6",
		Seconds:    10,
		Variables:  []string{},
	}

	httproute_l0 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._httproute_level__ac417cac == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l0",
		Seconds:    10,
		Variables:  []string{},
	}
	httproute_l1 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._httproute_level__e4abd750 == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l1",
		Seconds:    10,
		Variables:  []string{},
	}
	httproute_l5 := limitadorv1alpha1.RateLimit{
		Conditions: []string{"limit._httproute_level__e1d71177 == \"1\""},
		MaxValue:   3,
		Namespace:  "default/test-3-gw0-l5",
		Seconds:    10,
		Variables:  []string{},
	}

	t.Run("basic compare are the same", func(subT *testing.T) {
		limit1 := LimitadorRateLimits{
			httproute_l5,
		}

		limit2 := LimitadorRateLimits{
			httproute_l5,
		}

		if !limit1.EqualTo(limit2) {
			subT.Fatal("limit one is not equal to limit one")
		}
	})

	t.Run("global vs two routes", func(subT *testing.T) {
		existing := LimitadorRateLimits{
			global_l5,
			global_l1,
			global_l6,
			global_l3,
			global_l0,
			global_l4,
			global_l2,
		}

		desired := LimitadorRateLimits{
			httproute_l1,
			httproute_l0,
			global_l5,
			global_l4,
			global_l3,
			global_l6,
		}

		if existing.EqualTo(desired) {
			subT.Fatal("existing limit should not be the same as desired limit")
		}
	})

	t.Run("5 global & 2 routes vs 5 global & 2 routes, order differ", func(subT *testing.T) {
		existing := LimitadorRateLimits{
			httproute_l1,
			httproute_l0,
			global_l2,
			global_l5,
			global_l4,
			global_l3,
			global_l6,
		}

		desired := LimitadorRateLimits{
			global_l6,
			global_l5,
			global_l2,
			httproute_l1,
			global_l3,
			httproute_l0,
			global_l4,
		}

		if !existing.EqualTo(desired) {
			subT.Fatal("existing limit should be the same desired limit, list are only in different orders")
		}
	})
}
