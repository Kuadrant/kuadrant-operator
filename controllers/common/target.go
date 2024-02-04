package common

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/martinlindhe/base36"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const (
	LabelLBAttributeGeoCode = "kuadrant.io/lb-attribute-geo-code"
)

// MultiClusterGatewayTarget represents a Gateway that is placed on multiple clusters (ClusterGateway).
type MultiClusterGatewayTarget struct {
	Gateway               *gatewayapiv1.Gateway
	ClusterGatewayTargets []ClusterGatewayTarget
	LoadBalancing         *v1alpha1.LoadBalancingSpec
}

func NewMultiClusterGatewayTarget(gateway *gatewayapiv1.Gateway, clusterGateways []ClusterGateway, loadBalancing *v1alpha1.LoadBalancingSpec) (*MultiClusterGatewayTarget, error) {
	mcg := &MultiClusterGatewayTarget{Gateway: gateway, LoadBalancing: loadBalancing}
	err := mcg.setClusterGatewayTargets(clusterGateways)
	return mcg, err
}

func (t *MultiClusterGatewayTarget) GetName() string {
	return fmt.Sprintf("%s-%s", t.Gateway.Name, t.Gateway.Namespace)
}

func (t *MultiClusterGatewayTarget) GetShortCode() string {
	return ToBase36hash(t.GetName())
}

// GroupTargetsByGeo groups targets based on Geo Code.
func (t *MultiClusterGatewayTarget) GroupTargetsByGeo() map[v1alpha1.GeoCode][]ClusterGatewayTarget {
	geoTargets := make(map[v1alpha1.GeoCode][]ClusterGatewayTarget)
	for _, target := range t.ClusterGatewayTargets {
		geoTargets[target.GetGeo()] = append(geoTargets[target.GetGeo()], target)
	}
	return geoTargets
}

func (t *MultiClusterGatewayTarget) GetDefaultGeo() v1alpha1.GeoCode {
	if t.LoadBalancing != nil && t.LoadBalancing.Geo != nil {
		return v1alpha1.GeoCode(t.LoadBalancing.Geo.DefaultGeo)
	}
	return v1alpha1.DefaultGeo
}

func (t *MultiClusterGatewayTarget) GetDefaultWeight() int {
	if t.LoadBalancing != nil && t.LoadBalancing.Weighted != nil {
		return int(t.LoadBalancing.Weighted.DefaultWeight)
	}
	return int(v1alpha1.DefaultWeight)
}

func (t *MultiClusterGatewayTarget) setClusterGatewayTargets(clusterGateways []ClusterGateway) error {
	var cgTargets []ClusterGatewayTarget
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
	return ToBase36hash(t.GetName())
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

func (t *MultiClusterGatewayTarget) RemoveUnhealthyGatewayAddresses(probes []*kuadrantdnsv1alpha1.DNSHealthCheckProbe, listener gatewayapiv1.Listener) {

	//If we have no probes we can't determine health so return unmodified
	if len(probes) == 0 {
		return
	}

	//Build a map of gateway addresses and their health status
	gwAddressHealth := map[string]bool{}
	allunhealthy := true
	for _, cgt := range t.ClusterGatewayTargets {
		for _, gwa := range cgt.Status.Addresses {
			probe := getProbeForGatewayAddress(probes, gatewayapiv1.GatewayAddress(gwa), t.Gateway.Name, string(listener.Name))
			if probe == nil {
				continue
			}
			probeHealthy := true
			if probe.Status.Healthy != nil {
				probeHealthy = *probe.Status.Healthy
			}
			if probeHealthy && probe.Spec.FailureThreshold != nil && probe.Status.ConsecutiveFailures < *probe.Spec.FailureThreshold {
				allunhealthy = false
			}
			gwAddressHealth[gwa.Value] = probeHealthy

		}
	}
	//If we have no matching probes for our current addresses, or we have no healthy probes, return unmodified
	if len(gwAddressHealth) == 0 || allunhealthy {
		return
	}

	// Remove all unhealthy addresses, we know by this point at least one of our addresses is healthy
	for _, cgt := range t.ClusterGatewayTargets {
		healthyAddresses := []gatewayapiv1.GatewayStatusAddress{}
		for _, gwa := range cgt.Status.Addresses {
			if healthy, exists := gwAddressHealth[gwa.Value]; exists && healthy {
				healthyAddresses = append(healthyAddresses, gwa)
			}
		}
		cgt.Status.Addresses = healthyAddresses
	}
}

func getProbeForGatewayAddress(probes []*kuadrantdnsv1alpha1.DNSHealthCheckProbe, gwa gatewayapiv1.GatewayAddress, gatewayName, listenerName string) *kuadrantdnsv1alpha1.DNSHealthCheckProbe {
	for _, probe := range probes {
		if dnsHealthCheckProbeName(gwa.Value, gatewayName, listenerName) == probe.Name {
			return probe
		}
	}
	return nil
}

func dnsHealthCheckProbeName(address, gatewayName, listenerName string) string {
	return fmt.Sprintf("%s-%s-%s", address, gatewayName, listenerName)
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

func ToBase36hash(s string) string {
	hash := sha256.Sum224([]byte(s))
	// convert the hash to base36 (alphanumeric) to decrease collision probabilities
	base36hash := strings.ToLower(base36.EncodeBytes(hash[:]))
	// use 6 chars of the base36hash, should be enough to avoid collisions and keep the code short enough
	return base36hash[:6]
}
