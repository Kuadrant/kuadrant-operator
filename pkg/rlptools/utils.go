package rlptools

import (
	"fmt"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

func FullLimitName(rlp *kuadrantv1beta2.RateLimitPolicy, limitKey string) string {
	return fmt.Sprintf("%s/%s/%s", rlp.GetNamespace(), rlp.GetName(), limitKey)
}

// ReadLimitsFromRLP returns a list of Kuadrant limit objects that will be used as template
// for limitador configuration
func ReadLimitsFromRLP(rlp *kuadrantv1beta2.RateLimitPolicy) []Limit {
	limits := make([]Limit, 0)

	for limitKey, limit := range rlp.Spec.Limits {
		for rateIdx := range limit.Rates {
			maxValue, seconds := ConvertRateIntoSeconds(limit.Rates[rateIdx])
			limits = append(limits, Limit{
				MaxValue:   maxValue,
				Seconds:    seconds,
				Conditions: []string{fmt.Sprintf("%s == \"1\"", FullLimitName(rlp, limitKey))},
				Variables:  limit.CountersAsStringList(),
			})
		}
	}

	return limits
}

var timeUnitMap = map[kuadrantv1beta2.TimeUnit]int{
	kuadrantv1beta2.TimeUnit("second"): 1,
	kuadrantv1beta2.TimeUnit("minute"): 60,
	kuadrantv1beta2.TimeUnit("hour"):   60 * 60,
	kuadrantv1beta2.TimeUnit("day"):    60 * 60 * 24,
}

// ConvertRateIntoSeconds converts from RLP Rate API (limit, duration and unit)
// to Limitador's Limit format (maxValue, Seconds)
func ConvertRateIntoSeconds(rate kuadrantv1beta2.Rate) (maxValue int, seconds int) {
	maxValue = rate.Limit
	seconds = 0

	if tmpSecs, ok := timeUnitMap[rate.Unit]; ok && rate.Duration > 0 {
		seconds = tmpSecs * rate.Duration
	}

	if rate.Duration < 0 {
		seconds = 0
	}

	if rate.Limit < 0 {
		maxValue = 0
	}

	return
}
