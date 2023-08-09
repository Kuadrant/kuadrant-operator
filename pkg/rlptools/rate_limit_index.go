package rlptools

import (
	"reflect"
	"sort"
	"strings"

	orderedmap "github.com/elliotchance/orderedmap/v2"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type RateLimitIndexKey = client.ObjectKey

// NewRateLimitIndex builds an index to manage sets of rate limits, organized by key
func NewRateLimitIndex() *RateLimitIndex {
	return &RateLimitIndex{*orderedmap.NewOrderedMap[RateLimitIndexKey, RateLimitList]()}
}

// RateLimitIndex stores RateLimitLists by key
type RateLimitIndex struct {
	orderedmap.OrderedMap[RateLimitIndexKey, RateLimitList]
}

func (l *RateLimitIndex) Set(key RateLimitIndexKey, rateLimits RateLimitList) {
	if len(rateLimits) == 0 {
		return
	}
	l.OrderedMap.Set(key, rateLimits)
}

func (l *RateLimitIndex) ToRateLimits() RateLimitList {
	limitadorRateLimits := make(RateLimitList, 0)
	for rlSet := l.Front(); rlSet != nil; rlSet = rlSet.Next() {
		for _, rl := range rlSet.Value {
			limitadorRateLimits = append(limitadorRateLimits, rl)
		}
	}
	return limitadorRateLimits
}

type RateLimitList []limitadorv1alpha1.RateLimit

func (l RateLimitList) Len() int {
	return len(l)
}

func (l RateLimitList) Less(i, j int) bool {
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

func (l RateLimitList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func Equal(a, b RateLimitList) bool {
	if len(a) != len(b) {
		return false
	}

	aCopy := make(RateLimitList, len(a))
	bCopy := make(RateLimitList, len(b))

	copy(aCopy, a)
	copy(bCopy, b)

	// two limits with reordered conditions/variables are effectively the same
	// For comparison purposes, nil equals the empty array for conditions and variables
	for idx := range aCopy {
		aCopy[idx].Conditions = common.GetEmptySliceIfNil(aCopy[idx].Conditions)
		sort.Strings(aCopy[idx].Conditions)

		aCopy[idx].Variables = common.GetEmptySliceIfNil(aCopy[idx].Variables)
		sort.Strings(aCopy[idx].Variables)
	}

	for idx := range bCopy {
		bCopy[idx].Conditions = common.GetEmptySliceIfNil(bCopy[idx].Conditions)
		sort.Strings(bCopy[idx].Conditions)

		bCopy[idx].Variables = common.GetEmptySliceIfNil(bCopy[idx].Variables)
		sort.Strings(bCopy[idx].Variables)
	}

	sort.Sort(RateLimitList(aCopy))
	sort.Sort(RateLimitList(bCopy))

	return reflect.DeepEqual(aCopy, bCopy)
}
