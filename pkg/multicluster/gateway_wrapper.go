package multicluster

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	LabelPrefix                                              = "kuadrant.io/"
	ClustersLabelPrefix                                      = "clusters." + LabelPrefix
	MultiClusterIPAddressType       gatewayapiv1.AddressType = LabelPrefix + "MultiClusterIPAddress"
	MultiClusterHostnameAddressType gatewayapiv1.AddressType = LabelPrefix + "MultiClusterHostnameAddress"
)

type GatewayWrapper struct {
	*gatewayapiv1.Gateway
	ClusterID string
}

func NewGatewayWrapper(g *gatewayapiv1.Gateway, clusterID string) *GatewayWrapper {
	return &GatewayWrapper{Gateway: g, ClusterID: clusterID}
}

func isMultiClusterAddressType(addressType gatewayapiv1.AddressType) bool {
	return addressType == MultiClusterIPAddressType || addressType == MultiClusterHostnameAddressType
}

// IsMultiCluster reports a type of the first address in the Status block
// returns false if no addresses present
func (g *GatewayWrapper) IsMultiCluster() bool {
	if len(g.Status.Addresses) > 0 {
		return isMultiClusterAddressType(*g.Status.Addresses[0].Type)
	}
	return false
}

// Validate ensures correctly configured underlying Gateway object
// Returns nil if validation passed
func (g *GatewayWrapper) Validate() error {
	// Status.Addresses validation
	// Compares all addresses against the first address to ensure the same type
	for _, address := range g.Status.Addresses {
		if g.IsMultiCluster() == isMultiClusterAddressType(*address.Type) {
			continue
		}
		return fmt.Errorf("gateway is invalid: inconsistent status addresses")
	}
	return nil
}

// GetClusterGatewayAddresses constructs a map from Status.Addresses of underlying Gateway
// with key being a cluster and value being an address in the cluster.
// In case of a single-cluster Gateway the key is the Gateway Name.
func (g *GatewayWrapper) GetClusterGatewayAddresses() map[string][]gatewayapiv1.GatewayStatusAddress {
	if !g.IsMultiCluster() {
		// Single Cluster (Normal Gateway Status)
		return map[string][]gatewayapiv1.GatewayStatusAddress{g.GetName(): g.Status.Addresses}
	}

	// Multi Cluster (MGC Gateway Status)
	clusterAddrs := map[string][]gatewayapiv1.GatewayStatusAddress{}
	for _, address := range g.Status.Addresses {
		cluster, addressValue, found := strings.Cut(address.Value, "/")
		//If this fails something is wrong and the value hasn't been set correctly
		if !found {
			continue
		}

		if _, ok := clusterAddrs[cluster]; !ok {
			clusterAddrs[cluster] = []gatewayapiv1.GatewayStatusAddress{}
		}

		addressType, _ := AddressTypeToSingleCluster(gatewayapiv1.GatewayAddress(address))
		clusterAddrs[cluster] = append(clusterAddrs[cluster], gatewayapiv1.GatewayStatusAddress{
			Type:  &addressType,
			Value: addressValue,
		})
	}

	return clusterAddrs
}

// GetClusterGatewayLabels parses the labels of the wrapped Gateway and returns a list of labels for the given clusterName.
// In case of a single-cluster Gateway the wrapped Gateways labels are returned unmodified.
func (g *GatewayWrapper) GetClusterGatewayLabels(clusterName string) map[string]string {
	if !g.IsMultiCluster() {
		// Single Cluster (Normal Gateway Status)
		return g.GetLabels()
	}

	labels := map[string]string{}
	for k, v := range g.GetLabels() {
		if strings.HasPrefix(k, ClustersLabelPrefix) {
			attr, found := strings.CutPrefix(k, ClustersLabelPrefix+clusterName+"_")
			if found {
				labels[LabelPrefix+attr] = v
			}
			continue
		}
		// Only add a label if we haven't already found a cluster specific version of it
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
	return labels
}

// GetClusterGatewayListeners processes the wrapped Gateway and returns a ListenerStatus for the given clusterName.
// In case of a single-cluster Gateway the wrapped Gateways status listeners are returned unmodified.
func (g *GatewayWrapper) GetClusterGatewayListeners(clusterName string) []gatewayapiv1.ListenerStatus {
	if !g.IsMultiCluster() {
		// Single Cluster (Normal Gateway Status)
		return g.Status.Listeners
	}

	// Multi Cluster (MGC Gateway Status)
	listeners := []gatewayapiv1.ListenerStatus{}
	for _, specListener := range g.Spec.Listeners {
		for _, statusListener := range g.Status.Listeners {
			statusClusterName, statusListenerName, found := strings.Cut(string(statusListener.Name), ".")
			if !found {
				continue
			}
			if statusClusterName == clusterName && statusListenerName == string(specListener.Name) {
				ls := gatewayapiv1.ListenerStatus{
					Name:           specListener.Name,
					AttachedRoutes: statusListener.AttachedRoutes,
				}
				listeners = append(listeners, ls)
			}
		}
	}
	return listeners
}

// ClusterGateway contains a Gateway as it would be on a single cluster and the name of the cluster.
type ClusterGateway struct {
	gatewayapiv1.Gateway
	ClusterName string
}

// GetClusterGateways parse the wrapped Gateway and returns a list of ClusterGateway resources.
// In case of a single-cluster Gateway a single ClusterGateway is returned with the unmodified wrapped Gateway and the
// Gateway name used as values.
func (g *GatewayWrapper) GetClusterGateways() []ClusterGateway {
	if !g.IsMultiCluster() {
		// Single Cluster (Normal Gateway Status)
		return []ClusterGateway{
			{
				Gateway:     *g.Gateway,
				ClusterName: g.ClusterID,
			},
		}
	}

	// Multi Cluster (MGC Gateway Status)
	clusterAddrs := g.GetClusterGatewayAddresses()
	clusterGateways := []ClusterGateway{}
	for clusterName, addrs := range clusterAddrs {
		gw := gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      g.GetName(),
				Namespace: g.GetNamespace(),
				Labels:    g.GetClusterGatewayLabels(clusterName),
			},
			Spec: g.Spec,
			Status: gatewayapiv1.GatewayStatus{
				Addresses: addrs,
				Listeners: g.GetClusterGatewayListeners(clusterName),
			},
		}
		clusterGateways = append(clusterGateways, ClusterGateway{
			Gateway:     gw,
			ClusterName: clusterName,
		})
	}
	return clusterGateways
}

// AddressTypeToMultiCluster returns a multi cluster version of the address type
// and a bool to indicate that provided address type was converted. If not - original type is returned
func AddressTypeToMultiCluster(address gatewayapiv1.GatewayAddress) (gatewayapiv1.AddressType, bool) {
	if *address.Type == gatewayapiv1.IPAddressType {
		return MultiClusterIPAddressType, true
	} else if *address.Type == gatewayapiv1.HostnameAddressType {
		return MultiClusterHostnameAddressType, true
	}
	return *address.Type, false
}

// AddressTypeToSingleCluster converts provided multicluster address to single cluster version
// the bool indicates a successful conversion
func AddressTypeToSingleCluster(address gatewayapiv1.GatewayAddress) (gatewayapiv1.AddressType, bool) {
	if *address.Type == MultiClusterIPAddressType {
		return gatewayapiv1.IPAddressType, true
	} else if *address.Type == MultiClusterHostnameAddressType {
		return gatewayapiv1.HostnameAddressType, true
	}
	return *address.Type, false
}
