//go:build unit

package rlptools

import (
	"reflect"
	"testing"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRateLimitIndexSet(t *testing.T) {
	t.Run("index rate limits to a key", func(subT *testing.T) {
		index := NewRateLimitIndex()

		key := RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-1", Namespace: "ns"}}
		index.Set(key, []limitadorv1alpha1.RateLimit{
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
		index := NewRateLimitIndex()

		index.Set(RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-1", Namespace: "ns"}}, []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 10, Seconds: 1},
			{Namespace: "ns/rlp-1", MaxValue: 100, Seconds: 60},
			{Namespace: "ns/rlp-1", MaxValue: 1000, Seconds: 1},
		})

		index.Set(RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-2", Namespace: "ns"}}, []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-2", MaxValue: 50, Seconds: 1},
		})

		key := RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-1", Namespace: "ns"}}
		rateLimits, found := index.Get(key)
		if !found {
			subT.Fatal("expected rate limits to be indexed to key but none found:", key)
		}
		expectedCount := 3
		if len(rateLimits) != expectedCount {
			subT.Fatal("expected:", expectedCount, "rate limits for key", key, ", returned:", len(rateLimits))
		}

		key = RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-2", Namespace: "ns"}}
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
		index := NewRateLimitIndex()

		index.Set(RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-1", Namespace: "ns"}}, []limitadorv1alpha1.RateLimit{
			{Namespace: "ns/rlp-1", MaxValue: 10, Seconds: 1},
			{Namespace: "ns/rlp-1", MaxValue: 100, Seconds: 60},
			{Namespace: "ns/rlp-1", MaxValue: 1000, Seconds: 1},
		})

		index.Set(RateLimitIndexKey{RateLimitPolicyKey: client.ObjectKey{Name: "rlp-1", Namespace: "ns"}}, []limitadorv1alpha1.RateLimit{
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
		idx := NewRateLimitIndex()

		idx.Set(RateLimitIndexKey{GatewayKey: client.ObjectKey{Name: "gwA", Namespace: "nsA"}}, []limitadorv1alpha1.RateLimit{})

		aggregatedRateLimits := idx.ToRateLimits()
		if len(aggregatedRateLimits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("add nil list of limits if a noop", func(subT *testing.T) {
		idx := NewRateLimitIndex()

		idx.Set(RateLimitIndexKey{GatewayKey: client.ObjectKey{Name: "gwA", Namespace: "nsA"}}, []limitadorv1alpha1.RateLimit{})

		aggregatedRateLimits := idx.ToRateLimits()
		if len(aggregatedRateLimits) != 0 {
			subT.Fatal("returns not empty")
		}
	})
}

func TestRateLimitIndexToRateLimits(t *testing.T) {
	t.Run("nil index return empty list", func(subT *testing.T) {
		idx := NewRateLimitIndex()

		limits := idx.ToRateLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})

	t.Run("empty index return empty list", func(subT *testing.T) {
		idx := NewRateLimitIndex()

		limits := idx.ToRateLimits()
		if limits == nil {
			subT.Fatal("returns nil")
		}
		if len(limits) != 0 {
			subT.Fatal("returns not empty")
		}
	})
}
