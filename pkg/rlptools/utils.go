package rlptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"unicode"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

const (
	LimitadorRateLimitIdentitiferPrefix = "limit."
)

func LimitNameToLimitadorIdentifier(uniqueLimitName string) string {
	identifier := LimitadorRateLimitIdentitiferPrefix

	// sanitize chars that are not allowed in limitador identifiers
	for _, c := range uniqueLimitName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			identifier += string(c)
		} else {
			identifier += "_"
		}
	}

	// to avoid breaking the uniqueness of the limit name after sanitization, we add a hash of the original name
	hash := sha256.Sum256([]byte(uniqueLimitName))
	identifier += "__" + hex.EncodeToString(hash[:4])

	return identifier
}

// LimitadorRateLimitsFromRLP converts rate limits from a Kuadrant RateLimitPolicy into a list of Limitador rate limit
// objects
func LimitadorRateLimitsFromRLP(rlp *kuadrantv1beta2.RateLimitPolicy) []limitadorv1alpha1.RateLimit {
	limitsNamespace := LimitsNamespaceFromRLP(rlp)

	rateLimits := make([]limitadorv1alpha1.RateLimit, 0)
	for limitKey, limit := range rlp.Spec.Limits {
		limitIdentifier := LimitNameToLimitadorIdentifier(limitKey)
		for _, rate := range limit.Rates {
			maxValue, seconds := rateToSeconds(rate)
			rateLimits = append(rateLimits, limitadorv1alpha1.RateLimit{
				Namespace:  limitsNamespace,
				MaxValue:   maxValue,
				Seconds:    seconds,
				Conditions: []string{fmt.Sprintf("%s == \"1\"", limitIdentifier)},
				Variables:  common.GetEmptySliceIfNil(limit.CountersAsStringList()),
			})
		}
	}
	return rateLimits
}

func LimitsNamespaceFromRLP(rlp *kuadrantv1beta2.RateLimitPolicy) string {
	return fmt.Sprintf("%s/%s", rlp.GetNamespace(), rlp.GetName())
}

var timeUnitMap = map[kuadrantv1beta2.TimeUnit]int{
	kuadrantv1beta2.TimeUnit("second"): 1,
	kuadrantv1beta2.TimeUnit("minute"): 60,
	kuadrantv1beta2.TimeUnit("hour"):   60 * 60,
	kuadrantv1beta2.TimeUnit("day"):    60 * 60 * 24,
}

// rateToSeconds converts from RLP Rate API (limit, duration and unit)
// to Limitador's Limit format (maxValue, Seconds)
func rateToSeconds(rate kuadrantv1beta2.Rate) (maxValue int, seconds int) {
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

func RemoveRLPLabelsFromLimitadorList(limitadorList limitadorv1alpha1.LimitadorList, policyKey client.ObjectKey) (limitadorv1alpha1.LimitadorList, error) {
	var updateList limitadorv1alpha1.LimitadorList
	for index := range limitadorList.Items {
		limitador := limitadorList.Items[index]
		objAnnotations := limitador.GetAnnotations()
		val, ok := objAnnotations[common.RateLimitPoliciesBackRefAnnotation]
		if !ok {
			continue
		}

		var refs []client.ObjectKey
		err := json.Unmarshal([]byte(val), &refs)
		if err != nil {
			return updateList, err
		}
		refID := common.FindObjectKey(refs, policyKey)
		if refID != len(refs) {
			// remove index
			refs = append(refs[:refID], refs[refID+1:]...)

			if len(refs) > 0 {
				serialized, err := json.Marshal(refs)
				if err != nil {
					return updateList, err
				}
				objAnnotations[common.RateLimitPoliciesBackRefAnnotation] = string(serialized)
			} else {
				delete(objAnnotations, common.RateLimitPoliciesBackRefAnnotation)
			}

			limitador.SetAnnotations(objAnnotations)
			updateList.Items = append(updateList.Items, limitador)
		}
	}
	return updateList, nil
}
