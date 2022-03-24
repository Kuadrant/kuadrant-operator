package istio

import (
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
)

type PathMatchType string

// PathMatchType constants.
const (
	PathMatchExact             PathMatchType = "Exact"
	PathMatchPrefix            PathMatchType = "Prefix"
	PathMatchRegularExpression PathMatchType = "RegularExpression"
)

func StringMatch(path string, matchType PathMatchType) *istioapinetworkingv1alpha3.StringMatch {
	switch matchType {
	case PathMatchExact:
		return &istioapinetworkingv1alpha3.StringMatch{
			MatchType: &istioapinetworkingv1alpha3.StringMatch_Exact{Exact: path},
		}
	case PathMatchPrefix:
		return &istioapinetworkingv1alpha3.StringMatch{
			MatchType: &istioapinetworkingv1alpha3.StringMatch_Prefix{Prefix: path},
		}
	default:
		return &istioapinetworkingv1alpha3.StringMatch{
			MatchType: &istioapinetworkingv1alpha3.StringMatch_Regex{Regex: path},
		}
	}
}
