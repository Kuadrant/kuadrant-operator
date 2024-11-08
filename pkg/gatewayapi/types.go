package gatewayapi

import (
	"fmt"
	"sort"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/utils"
)

type Policy interface {
	client.Object
	GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference
	GetStatus() PolicyStatus
}

type PolicyStatus interface {
	GetConditions() []metav1.Condition
}

// HTTPRouteMatchConfig stores any config associated to an HTTPRouteRule
type HTTPRouteMatchConfig struct {
	Hostname          string
	HTTPRouteMatch    gatewayapiv1.HTTPRouteMatch
	CreationTimestamp metav1.Time
	Namespace         string
	Name              string
	Config            any
}

// SortableHTTPRouteMatchConfigs is a slice of HTTPRouteMatch that implements sort.Interface
type SortableHTTPRouteMatchConfigs []HTTPRouteMatchConfig

func (c SortableHTTPRouteMatchConfigs) Len() int      { return len(c) }
func (c SortableHTTPRouteMatchConfigs) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c SortableHTTPRouteMatchConfigs) Less(i, j int) bool {
	// Hostname
	if c[i].Hostname != c[j].Hostname {
		return utils.CompareHostnamesSpecificity(c[i].Hostname, c[j].Hostname)
	}

	// HTTPRouteMatch (https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteRule)
	// HTTPRouteMatch - based on path match type (Exact > RegularExpression > PathPrefix)
	if c[i].HTTPRouteMatch.Path != nil && c[i].HTTPRouteMatch.Path.Type != nil && *c[i].HTTPRouteMatch.Path.Type == gatewayapiv1.PathMatchExact {
		if c[j].HTTPRouteMatch.Path != nil && c[j].HTTPRouteMatch.Path.Type != nil {
			switch *c[j].HTTPRouteMatch.Path.Type {
			case gatewayapiv1.PathMatchRegularExpression, gatewayapiv1.PathMatchPathPrefix:
				return true
			}
		}
	}
	if c[i].HTTPRouteMatch.Path != nil && c[i].HTTPRouteMatch.Path.Type != nil && *c[i].HTTPRouteMatch.Path.Type == gatewayapiv1.PathMatchRegularExpression {
		if c[j].HTTPRouteMatch.Path != nil && c[j].HTTPRouteMatch.Path.Type != nil {
			switch *c[j].HTTPRouteMatch.Path.Type {
			case gatewayapiv1.PathMatchExact:
				return false
			case gatewayapiv1.PathMatchPathPrefix:
				return true
			}
		}
	}
	if c[i].HTTPRouteMatch.Path != nil && c[i].HTTPRouteMatch.Path.Type != nil && *c[i].HTTPRouteMatch.Path.Type == gatewayapiv1.PathMatchPathPrefix {
		if c[j].HTTPRouteMatch.Path != nil && c[j].HTTPRouteMatch.Path.Type != nil {
			switch *c[j].HTTPRouteMatch.Path.Type {
			case gatewayapiv1.PathMatchExact, gatewayapiv1.PathMatchRegularExpression:
				return false
			}
		}
	}

	// HTTPRouteMatch - based on number of characters in a matching path
	pCountI := pathMatchCount(c[i].HTTPRouteMatch.Path)
	pCountJ := pathMatchCount(c[j].HTTPRouteMatch.Path)
	if pCountI != pCountJ {
		return pCountI > pCountJ
	}

	// HTTPRouteMatch - based on method match type
	hasMethodI := c[i].HTTPRouteMatch.Method != nil
	hasMethodJ := c[j].HTTPRouteMatch.Method != nil
	if hasMethodI != hasMethodJ {
		return !hasMethodI
	}

	// HTTPRouteMatch - based on the number of header match type
	hCountI := len(c[i].HTTPRouteMatch.Headers)
	hCountJ := len(c[j].HTTPRouteMatch.Headers)
	if hCountI != hCountJ {
		return hCountI > hCountJ
	}

	// HTTPRouteMatch - based on the number of query param match type
	qCountI := len(c[i].HTTPRouteMatch.QueryParams)
	qCountJ := len(c[j].HTTPRouteMatch.QueryParams)
	if qCountI != qCountJ {
		return qCountI > qCountJ
	}

	// Creation Timestamp
	p1Time := ptr.To(c[i].CreationTimestamp)
	p2Time := ptr.To(c[j].CreationTimestamp)
	if !p1Time.Equal(p2Time) {
		return p1Time.Before(p2Time)
	}

	// Lexicographically by "{namespace}/{name}"
	return fmt.Sprintf("%s/%s", c[i].Namespace, c[i].Name) < fmt.Sprintf("%s/%s", c[j].Namespace, c[j].Name)
}

func pathMatchCount(pathMatch *gatewayapiv1.HTTPPathMatch) int {
	if pathMatch != nil && pathMatch.Value != nil {
		return len(*pathMatch.Value)
	}
	return 0
}

type GrouppedHTTPRouteMatchConfigs map[string]SortableHTTPRouteMatchConfigs

func (g *GrouppedHTTPRouteMatchConfigs) Add(key string, configs ...HTTPRouteMatchConfig) {
	for _, config := range configs {
		(*g)[key] = append((*g)[key], config)
	}
}

func (g *GrouppedHTTPRouteMatchConfigs) Sorted() GrouppedHTTPRouteMatchConfigs {
	if g == nil {
		return nil
	}
	return lo.MapValues(*g, func(configs SortableHTTPRouteMatchConfigs, _ string) SortableHTTPRouteMatchConfigs {
		sortedConfigs := make(SortableHTTPRouteMatchConfigs, len(configs))
		copy(sortedConfigs, configs)
		sort.Sort(sortedConfigs)
		return sortedConfigs
	})
}
