package ratelimit

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/elliotchance/orderedmap/v2"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// NewIndex builds an index to manage sets of rate limits, organized by key
func NewIndex() *Index {
	return &Index{OrderedMap: *orderedmap.NewOrderedMap[string, LimitadorRateLimits]()}
}

// Index stores LimitadorRateLimitss by key
type Index struct {
	sync.RWMutex
	orderedmap.OrderedMap[string, LimitadorRateLimits]
}

func (l *Index) Set(key string, rateLimits LimitadorRateLimits) {
	if len(rateLimits) == 0 {
		return
	}
	l.Lock()
	defer l.Unlock()
	l.OrderedMap.Set(key, rateLimits)
}

func (l *Index) ToRateLimits() LimitadorRateLimits {
	l.RLock()
	defer l.RUnlock()
	limitadorRateLimits := make(LimitadorRateLimits, 0)
	for rlSet := l.Front(); rlSet != nil; rlSet = rlSet.Next() {
		limitadorRateLimits = append(limitadorRateLimits, rlSet.Value...)
	}
	return limitadorRateLimits
}

type LimitadorRateLimits []limitadorv1alpha1.RateLimit

func (l LimitadorRateLimits) Len() int {
	return len(l)
}

func (l LimitadorRateLimits) Less(i, j int) bool {
	if l[i].MaxValue != l[j].MaxValue {
		return l[i].MaxValue > l[j].MaxValue
	}

	if l[i].Seconds != l[j].Seconds {
		return l[i].Seconds > l[j].Seconds
	}

	// Conditions

	if len(l[i].Conditions) != len(l[j].Conditions) {
		return len(l[i].Conditions) > len(l[j].Conditions)
	}

	for idx, condI := range l[i].Conditions {
		condJ := l[j].Conditions[idx]
		switch strings.Compare(condI, condJ) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	// Variables

	if len(l[i].Variables) != len(l[j].Variables) {
		return len(l[i].Variables) > len(l[j].Variables)
	}

	for idx, condI := range l[i].Variables {
		condJ := l[j].Variables[idx]
		switch strings.Compare(condI, condJ) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	// they are equal. Return whatever
	return true
}

func (l LimitadorRateLimits) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l LimitadorRateLimits) EqualTo(other LimitadorRateLimits) bool {
	if len(l) != len(other) {
		return false
	}

	aCopy := make(LimitadorRateLimits, len(l))
	bCopy := make(LimitadorRateLimits, len(other))

	copy(aCopy, l)
	copy(bCopy, other)

	// two limits with reordered conditions/variables are effectively the same
	// For comparison purposes, nil equals the empty array for conditions and variables
	for idx := range aCopy {
		aCopy[idx].Conditions = utils.GetEmptySliceIfNil(aCopy[idx].Conditions)
		sort.Strings(aCopy[idx].Conditions)

		aCopy[idx].Variables = utils.GetEmptySliceIfNil(aCopy[idx].Variables)
		sort.Strings(aCopy[idx].Variables)
	}

	for idx := range bCopy {
		bCopy[idx].Conditions = utils.GetEmptySliceIfNil(bCopy[idx].Conditions)
		sort.Strings(bCopy[idx].Conditions)

		bCopy[idx].Variables = utils.GetEmptySliceIfNil(bCopy[idx].Variables)
		sort.Strings(bCopy[idx].Variables)
	}

	sort.Sort(aCopy)
	sort.Sort(bCopy)

	return reflect.DeepEqual(aCopy, bCopy)
}
