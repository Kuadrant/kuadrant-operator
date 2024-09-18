package multicluster

import (
	"fmt"

	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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

func (t *GatewayTarget) GetGeo() v1alpha1.GeoCode {
	if t.LoadBalancing != nil {
		return v1alpha1.GeoCode(t.LoadBalancing.Geo)
	}
	return v1alpha1.DefaultGeo
}

func (t *GatewayTarget) IsDefaultGeo() bool {
	if t.LoadBalancing != nil {
		return t.LoadBalancing.DefaultGeo
	}
	return false
}

func (t *GatewayTarget) GetWeight() int {
	if t.LoadBalancing != nil {
		return t.LoadBalancing.Weight
	}
	return v1alpha1.DefaultWeight
}

func (t *GatewayTarget) setClusterGatewayTargets(clusterGateways []ClusterGateway) error {
	cgTargets := []ClusterGatewayTarget{}
	for _, cg := range clusterGateways {
		cgt, err := NewClusterGatewayTarget(cg, t.GetGeo(), t.GetWeight())
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
	Geo    v1alpha1.GeoCode
	Weight int
}

func NewClusterGatewayTarget(cg ClusterGateway, geoCode v1alpha1.GeoCode, weight int) (ClusterGatewayTarget, error) {
	target := ClusterGatewayTarget{
		ClusterGateway: &cg,
		Geo:            geoCode,
		Weight:         weight,
	}
	return target, nil
}

func (t *ClusterGatewayTarget) GetGeo() v1alpha1.GeoCode {
	return t.Geo
}

func (t *ClusterGatewayTarget) GetWeight() int {
	return t.Weight
}

func (t *ClusterGatewayTarget) GetName() string {
	return t.ClusterName
}

func (t *ClusterGatewayTarget) GetShortCode() string {
	return common.ToBase36HashLen(t.GetName(), utils.ClusterIDLength)
}
