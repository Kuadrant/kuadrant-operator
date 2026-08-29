//go:build unit

package gatewayapi

import (
	"sort"
	"testing"
	"time"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestCanonicalHTTPRouteMatchKeyStableForReorderedHeadersAndQueries(t *testing.T) {
	pathType := gatewayapiv1.PathMatchPathPrefix
	headerType := gatewayapiv1.HeaderMatchExact
	queryType := gatewayapiv1.QueryParamMatchExact

	base := gatewayapiv1.HTTPRouteMatch{
		Path: &gatewayapiv1.HTTPPathMatch{
			Type:  &pathType,
			Value: ptr.To("/v1/chat/completions"),
		},
		Headers: []gatewayapiv1.HTTPHeaderMatch{
			{Name: "x-b", Type: &headerType, Value: "2"},
			{Name: "x-a", Type: &headerType, Value: "1"},
		},
		QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
			{Name: "q-b", Type: &queryType, Value: "2"},
			{Name: "q-a", Type: &queryType, Value: "1"},
		},
	}

	reordered := gatewayapiv1.HTTPRouteMatch{
		Path: base.Path,
		Headers: []gatewayapiv1.HTTPHeaderMatch{
			{Name: "x-a", Type: &headerType, Value: "1"},
			{Name: "x-b", Type: &headerType, Value: "2"},
		},
		QueryParams: []gatewayapiv1.HTTPQueryParamMatch{
			{Name: "q-a", Type: &queryType, Value: "1"},
			{Name: "q-b", Type: &queryType, Value: "2"},
		},
	}

	assert.Equal(t, canonicalHTTPRouteMatchKey(base), canonicalHTTPRouteMatchKey(reordered))
}

func TestSortableHTTPRouteMatchConfigsDeterministicTieBreak(t *testing.T) {
	pathType := gatewayapiv1.PathMatchPathPrefix
	headerType := gatewayapiv1.HeaderMatchExact
	now := metav1.NewTime(time.Now())

	a := HTTPRouteMatchConfig{
		Hostname:          "example.com",
		CreationTimestamp: now,
		Namespace:         "ns",
		Name:              "a",
		HTTPRouteMatch: gatewayapiv1.HTTPRouteMatch{
			Path: &gatewayapiv1.HTTPPathMatch{
				Type:  &pathType,
				Value: ptr.To("/v1/chat/completions"),
			},
			Headers: []gatewayapiv1.HTTPHeaderMatch{
				{Name: "x-b", Type: &headerType, Value: "1"},
			},
		},
	}
	b := HTTPRouteMatchConfig{
		Hostname:          "example.com",
		CreationTimestamp: now,
		Namespace:         "ns",
		Name:              "b",
		HTTPRouteMatch: gatewayapiv1.HTTPRouteMatch{
			Path: &gatewayapiv1.HTTPPathMatch{
				Type:  &pathType,
				Value: ptr.To("/v1/chat/completions"),
			},
			Headers: []gatewayapiv1.HTTPHeaderMatch{
				{Name: "x-a", Type: &headerType, Value: "1"},
			},
		},
	}

	configs := SortableHTTPRouteMatchConfigs{b, a}
	sort.Sort(configs)

	assert.Equal(t, configs[0].Name, "b")
	assert.Equal(t, configs[1].Name, "a")
}
