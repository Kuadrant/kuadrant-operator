package multicluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	LabelLBAttributeGeoCode = "kuadrant.io/lb-attribute-geo-code"
)

// GatewayTarget represents a Gateway that is placed on multiple clusters (ClusterGateway).
type GatewayTarget struct {
	Gateway               *gatewayapiv1.Gateway
	ClusterGatewayTargets []ClusterGatewayTarget
	LoadBalancing         *v1alpha1.LoadBalancingSpec
}

func NewGatewayTarget(gateway *gatewayapiv1.Gateway, clusterGateways []ClusterGateway, loadBalancing *v1alpha1.LoadBalancingSpec) (*GatewayTarget, error) {
	mcg := &GatewayTarget{Gateway: gateway, LoadBalancing: loadBalancing}
	err := mcg.setClusterGatewayTargets(clusterGateways)
	return mcg, err
}

func (t *GatewayTarget) GetName() string {
	return fmt.Sprintf("%s-%s", t.Gateway.Name, t.Gateway.Namespace)
}

func (t *GatewayTarget) GetShortCode() string {
	return common.ToBase36HashLen(t.GetName(), utils.ClusterIDLength)
}

// GroupTargetsByGeo groups targets based on Geo Code.
func (t *GatewayTarget) GroupTargetsByGeo() map[v1alpha1.GeoCode][]ClusterGatewayTarget {
	geoTargets := make(map[v1alpha1.GeoCode][]ClusterGatewayTarget)
	for _, target := range t.ClusterGatewayTargets {
		geoTargets[target.GetGeo()] = append(geoTargets[target.GetGeo()], target)
	}
	return geoTargets
}

func (t *GatewayTarget) GetDefaultGeo() v1alpha1.GeoCode {
	if t.LoadBalancing != nil && t.LoadBalancing.Geo != nil {
		return v1alpha1.GeoCode(t.LoadBalancing.Geo.DefaultGeo)
	}
	return v1alpha1.DefaultGeo
}

func (t *GatewayTarget) GetDefaultWeight() int {
	if t.LoadBalancing != nil && t.LoadBalancing.Weighted != nil {
		return int(t.LoadBalancing.Weighted.DefaultWeight)
	}
	return int(v1alpha1.DefaultWeight)
}

func (t *GatewayTarget) setClusterGatewayTargets(clusterGateways []ClusterGateway) error {
	cgTargets := []ClusterGatewayTarget{}
	for _, cg := range clusterGateways {
		var customWeights []*v1alpha1.CustomWeight
		if t.LoadBalancing != nil && t.LoadBalancing.Weighted != nil {
			customWeights = t.LoadBalancing.Weighted.Custom
		}
		cgt, err := NewClusterGatewayTarget(cg, t.GetDefaultGeo(), t.GetDefaultWeight(), customWeights)
		if err != nil {
			return err
		}
		cgTargets = append(cgTargets, cgt)
	}
	t.ClusterGatewayTargets = cgTargets
	return nil
}

// ClusterGatewayTarget represents a cluster Gateway with geo and weighting info calculated
type ClusterGatewayTarget struct {
	*ClusterGateway
	Geo    *v1alpha1.GeoCode
	Weight *int
}

func NewClusterGatewayTarget(cg ClusterGateway, defaultGeoCode v1alpha1.GeoCode, defaultWeight int, customWeights []*v1alpha1.CustomWeight) (ClusterGatewayTarget, error) {
	target := ClusterGatewayTarget{
		ClusterGateway: &cg,
	}
	target.setGeo(defaultGeoCode)
	err := target.setWeight(defaultWeight, customWeights)
	if err != nil {
		return ClusterGatewayTarget{}, err
	}
	return target, nil
}

func (t *ClusterGatewayTarget) GetGeo() v1alpha1.GeoCode {
	return *t.Geo
}

func (t *ClusterGatewayTarget) GetWeight() int {
	return *t.Weight
}

func (t *ClusterGatewayTarget) GetName() string {
	return t.ClusterName
}

func (t *ClusterGatewayTarget) GetShortCode() string {
	return common.ToBase36HashLen(t.GetName(), utils.ClusterIDLength)
}

func (t *ClusterGatewayTarget) setGeo(defaultGeo v1alpha1.GeoCode) {
	geoCode := defaultGeo
	if geoCode == v1alpha1.DefaultGeo {
		t.Geo = &geoCode
		return
	}
	if gc, ok := t.GetLabels()[LabelLBAttributeGeoCode]; ok {
		geoCode = v1alpha1.GeoCode(gc)
	}
	t.Geo = &geoCode
}

func (t *ClusterGatewayTarget) setWeight(defaultWeight int, customWeights []*v1alpha1.CustomWeight) error {
	weight := defaultWeight
	for k := range customWeights {
		cw := customWeights[k]
		selector, err := metav1.LabelSelectorAsSelector(cw.Selector)
		if err != nil {
			return err
		}
		if selector.Matches(labels.Set(t.GetLabels())) {
			customWeight := int(cw.Weight)
			weight = customWeight
			break
		}
	}
	t.Weight = &weight
	return nil
}
