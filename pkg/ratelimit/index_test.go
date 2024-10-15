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
