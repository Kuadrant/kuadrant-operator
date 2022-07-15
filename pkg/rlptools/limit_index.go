package rlptools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

type LimitsByDomain map[string][]apimv1alpha1.Limit

func (l LimitsByDomain) String() string {
	jsonData, _ := json.MarshalIndent(l, "", "  ")
	return string(jsonData)
}

type LimitList []apimv1alpha1.Limit

func (l LimitList) Len() int {
	return len(l)
}

func (l LimitList) Less(i, j int) bool {
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

func (l LimitList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func SameLimitList(a, b []apimv1alpha1.Limit) bool {
	if len(a) != len(b) {
		return false
	}

	aCopy := make([]apimv1alpha1.Limit, len(a))
	bCopy := make([]apimv1alpha1.Limit, len(b))

	copy(aCopy, a)
	copy(bCopy, b)

	sort.Sort(LimitList(aCopy))
	sort.Sort(LimitList(bCopy))

	return reflect.DeepEqual(aCopy, bCopy)
}

func (l LimitsByDomain) Equals(other LimitsByDomain) bool {
	if other == nil {
		return false
	}

	if len(l) != len(other) {
		return false
	}

	for domain := range l {
		if _, ok := other[domain]; !ok {
			return false
		}

		if !SameLimitList(l[domain], other[domain]) {
			return false
		}
	}

	return true
}

// LimitIndex allows manage Limitador CR limits based on gateways and domains
// gateways and domains are encoded in the namespace field of the limit
// limit namespace format: "{gateway}#{domain}"
type LimitIndex struct {
	logger        logr.Logger
	gatewayLimits map[client.ObjectKey]LimitsByDomain
}

func (l *LimitIndex) ToLimits() []limitadorv1alpha1.RateLimit {
	limits := make([]limitadorv1alpha1.RateLimit, 0)

	for gwKey, limitsByDomain := range l.gatewayLimits {
		for domain, limitList := range limitsByDomain {
			for idx := range limitList {
				limits = append(limits, limitadorv1alpha1.RateLimit{
					Namespace:  common.MarshallNamespace(gwKey, domain),
					MaxValue:   limitList[idx].MaxValue,
					Seconds:    limitList[idx].Seconds,
					Variables:  limitList[idx].Variables,
					Conditions: limitList[idx].Conditions,
				})
			}
		}
	}

	return limits
}

func (l *LimitIndex) DeleteGateway(gwKey client.ObjectKey) {
	delete(l.gatewayLimits, gwKey)
}

func (l *LimitIndex) AddGatewayLimits(gwKey client.ObjectKey, gwLimits LimitsByDomain) {
	for domain, limitList := range gwLimits {
		for idx := range limitList {
			l.AddLimit(gwKey, domain, &limitList[idx])
		}
	}
}

// AddLimit adds one new limit to the index structure
func (l *LimitIndex) AddLimit(gwKey client.ObjectKey, domain string, limit *apimv1alpha1.Limit) {
	if _, ok := l.gatewayLimits[gwKey]; !ok {
		l.gatewayLimits[gwKey] = make(LimitsByDomain)
	}

	l.gatewayLimits[gwKey][domain] = append(l.gatewayLimits[gwKey][domain], *limit)
}

// AddLimitFromRateLimit adds one new limit to the index structure
func (l *LimitIndex) AddLimitFromRateLimit(limit *limitadorv1alpha1.RateLimit) {
	if limit == nil {
		return
	}

	gwKey, domain, err := common.UnMarshallLimitNamespace(limit.Namespace)
	if err != nil {
		l.logger.V(1).Info("cannot unmarshall limit namespace",
			"namespace", limit.Namespace,
			"error", err)
		return
	}

	l.AddLimit(gwKey, domain, apimv1alpha1.LimitFromLimitadorRateLimit(limit))
}

func (l *LimitIndex) Equals(other *LimitIndex) bool {
	// reflect.DeepEqual does not work well with slices when order does not matter

	if other == nil {
		return false
	}

	if len(l.gatewayLimits) != len(other.gatewayLimits) {
		return false
	}

	for gwKey := range l.gatewayLimits {
		if _, ok := other.gatewayLimits[gwKey]; !ok {
			return false
		}

		if !l.gatewayLimits[gwKey].Equals(other.gatewayLimits[gwKey]) {
			return false
		}
	}

	return true
}

func NewLimitadorIndex(limitador *limitadorv1alpha1.Limitador, logger logr.Logger) *LimitIndex {
	limitIdx := &LimitIndex{
		logger:        logger,
		gatewayLimits: make(map[client.ObjectKey]LimitsByDomain),
	}

	if limitador == nil {
		return limitIdx
	}

	for idx := range limitador.Spec.Limits {
		limitIdx.AddLimitFromRateLimit(&limitador.Spec.Limits[idx])
	}

	return limitIdx
}
