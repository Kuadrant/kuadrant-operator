package controllers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/multicluster"
)

const (
	LabelGatewayReference  = "kuadrant.io/gateway"
	LabelGatewayNSRef      = "kuadrant.io/gateway-namespace"
	LabelListenerReference = "kuadrant.io/listener-name"

	DefaultTTL      = 60
	DefaultCnameTTL = 300
)

var (
	ErrUnknownRoutingStrategy = fmt.Errorf("unknown routing strategy")
	ErrNoManagedZoneForHost   = fmt.Errorf("no managed zone for host")
)

type dnsHelper struct {
	client.Client
}

func getSetID(e *externaldns.Endpoint) string {
	return e.DNSName + e.SetIdentifier
}

func findMatchingManagedZone(originalHost, host string, zones []kuadrantdnsv1alpha1.ManagedZone) (*kuadrantdnsv1alpha1.ManagedZone, string, error) {
	if len(zones) == 0 {
		return nil, "", fmt.Errorf("%w : %s", ErrNoManagedZoneForHost, host)
	}
	host = strings.ToLower(host)
	//get the TLD from this host
	tld, _ := publicsuffix.PublicSuffix(host)

	//The host is a TLD, so we now know `originalHost` can't possibly have a valid `ManagedZone` available.
	if host == tld {
		return nil, "", fmt.Errorf("no valid zone found for host: %v", originalHost)
	}

	hostParts := strings.SplitN(host, ".", 2)
	if len(hostParts) < 2 {
		return nil, "", fmt.Errorf("no valid zone found for host: %s", originalHost)
	}
	parentDomain := hostParts[1]

	// We do not currently support creating records for Apex domains, and a ManagedZone represents an Apex domain, as such
	// we should never be trying to find a managed zone that matches the `originalHost` exactly. Instead, we just continue
	// on to the next possible valid host to try i.e. the parent domain.
	if host == originalHost {
		return findMatchingManagedZone(originalHost, parentDomain, zones)
	}

	zone, ok := utils.Find(zones, func(zone kuadrantdnsv1alpha1.ManagedZone) bool {
		return strings.ToLower(zone.Spec.DomainName) == host
	})

	if ok {
		subdomain := strings.Replace(strings.ToLower(originalHost), "."+strings.ToLower(zone.Spec.DomainName), "", 1)
		return zone, subdomain, nil
	}
	return findMatchingManagedZone(originalHost, parentDomain, zones)
}

func commonDNSRecordLabels(gwKey client.ObjectKey, p *v1alpha1.DNSPolicy) map[string]string {
	commonLabels := map[string]string{}
	for k, v := range policyDNSRecordLabels(p) {
		commonLabels[k] = v
	}
	for k, v := range gatewayDNSRecordLabels(gwKey) {
		commonLabels[k] = v
	}
	return commonLabels
}

func policyDNSRecordLabels(p *v1alpha1.DNSPolicy) map[string]string {
	return map[string]string{
		p.DirectReferenceAnnotationName():                              p.Name,
		fmt.Sprintf("%s-namespace", p.DirectReferenceAnnotationName()): p.Namespace,
	}
}

func gatewayDNSRecordLabels(gwKey client.ObjectKey) map[string]string {
	return map[string]string{
		LabelGatewayNSRef:     gwKey.Namespace,
		LabelGatewayReference: gwKey.Name,
	}
}

func (dh *dnsHelper) setEndpoints(mcgTarget *multicluster.GatewayTarget, dnsRecord *kuadrantdnsv1alpha1.DNSRecord, listener gatewayapiv1.Listener, strategy v1alpha1.RoutingStrategy) error {
	gwListenerHost := string(*listener.Hostname)
	var endpoints []*externaldns.Endpoint

	//Health Checks currently modify endpoints so we have to keep existing ones in order to not lose health check ids
	currentEndpoints := make(map[string]*externaldns.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, ep := range dnsRecord.Spec.Endpoints {
		currentEndpoints[getSetID(ep)] = ep
	}

	switch strategy {
	case v1alpha1.SimpleRoutingStrategy:
		endpoints = dh.getSimpleEndpoints(mcgTarget, gwListenerHost, currentEndpoints)
	case v1alpha1.LoadBalancedRoutingStrategy:
		endpoints = dh.getLoadBalancedEndpoints(mcgTarget, gwListenerHost, currentEndpoints)
	default:
		return fmt.Errorf("%w : %s", ErrUnknownRoutingStrategy, strategy)
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return getSetID(endpoints[i]) < getSetID(endpoints[j])
	})

	dnsRecord.Spec.Endpoints = endpoints

	return nil
}

// getSimpleEndpoints returns the endpoints for the given GatewayTarget using the simple routing strategy

func (dh *dnsHelper) getSimpleEndpoints(mcgTarget *multicluster.GatewayTarget, hostname string, currentEndpoints map[string]*externaldns.Endpoint) []*externaldns.Endpoint {
	var (
		endpoints  []*externaldns.Endpoint
		ipValues   []string
		hostValues []string
	)

	for _, cgwTarget := range mcgTarget.ClusterGatewayTargets {
		for _, gwa := range cgwTarget.Status.Addresses {
			if *gwa.Type == gatewayapiv1.IPAddressType {
				ipValues = append(ipValues, gwa.Value)
			} else {
				hostValues = append(hostValues, gwa.Value)
			}
		}
	}

	if len(ipValues) > 0 {
		ep := createOrUpdateEndpoint(hostname, ipValues, kuadrantdnsv1alpha1.ARecordType, "", DefaultTTL, currentEndpoints)
		endpoints = append(endpoints, ep)
	}

	//ToDO This could possibly result in an invalid record since you can't have multiple CNAME target values https://github.com/kuadrant/kuadrant-operator/issues/663
	if len(hostValues) > 0 {
		ep := createOrUpdateEndpoint(hostname, hostValues, kuadrantdnsv1alpha1.CNAMERecordType, "", DefaultTTL, currentEndpoints)
		endpoints = append(endpoints, ep)
	}

	return endpoints
}

// getLoadBalancedEndpoints returns the endpoints for the given GatewayTarget using the loadbalanced routing strategy
//
// Builds an array of externaldns.Endpoint resources and sets them on the given DNSRecord. The endpoints expected are calculated
// from the GatewayTarget using the target Gateway (GatewayTarget.Gateway), the LoadBalancing Spec
// from the DNSPolicy attached to the target gateway (GatewayTarget.LoadBalancing) and the list of clusters the
// target gateway is currently placed on (GatewayTarget.ClusterGatewayTargets).
//
// GatewayTarget.ClusterGatewayTarget are grouped by Geo, in the case of Geo not being defined in the
// LoadBalancing Spec (Weighted only) an internal only Geo Code of "default" is used and all clusters added to it.
//
// A CNAME record is created for the target host (DNSRecord.name), pointing to a generated gateway lb host.
// A CNAME record for the gateway lb host is created for every Geo, with appropriate Geo information, pointing to a geo
// specific host.
// A CNAME record for the geo specific host is created for every Geo, with weight information for that target added,
// pointing to a target cluster hostname.
// An A record for the target cluster hostname is created for any IP targets retrieved for that cluster.
//
// Example(Weighted only)
//
// www.example.com CNAME lb-1ab1.www.example.com
// lb-1ab1.www.example.com CNAME geolocation * default.lb-1ab1.www.example.com
// default.lb-1ab1.www.example.com CNAME weighted 100 1bc1.lb-1ab1.www.example.com
// default.lb-1ab1.www.example.com CNAME weighted 100 aws.lb.com
// 1bc1.lb-1ab1.www.example.com A 192.22.2.1
//
// Example(Geo, default IE)
//
// shop.example.com CNAME lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com CNAME geolocation ireland ie.lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com geolocation australia aus.lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com geolocation default ie.lb-a1b2.shop.example.com (set by the default geo option)
// ie.lb-a1b2.shop.example.com CNAME weighted 100 ab1.lb-a1b2.shop.example.com
// ie.lb-a1b2.shop.example.com CNAME weighted 100 aws.lb.com
// aus.lb-a1b2.shop.example.com CNAME weighted 100 ab2.lb-a1b2.shop.example.com
// aus.lb-a1b2.shop.example.com CNAME weighted 100 ab3.lb-a1b2.shop.example.com
// ab1.lb-a1b2.shop.example.com A 192.22.2.1 192.22.2.5
// ab2.lb-a1b2.shop.example.com A 192.22.2.3
// ab3.lb-a1b2.shop.example.com A 192.22.2.4

func (dh *dnsHelper) getLoadBalancedEndpoints(mcgTarget *multicluster.GatewayTarget, hostname string, currentEndpoints map[string]*externaldns.Endpoint) []*externaldns.Endpoint {
	cnameHost := hostname
	if isWildCardHost(hostname) {
		cnameHost = strings.Replace(hostname, "*.", "", -1)
	}

	var ep *externaldns.Endpoint
	var defaultEndpoint *externaldns.Endpoint
	endpoints := make([]*externaldns.Endpoint, 0)
	lbName := strings.ToLower(fmt.Sprintf("lb-%s.%s", mcgTarget.GetShortCode(), cnameHost))

	for geoCode, cgwTargets := range mcgTarget.GroupTargetsByGeo() {
		geoLbName := strings.ToLower(fmt.Sprintf("%s.%s", geoCode, lbName))
		var clusterEndpoints []*externaldns.Endpoint
		for _, cgwTarget := range cgwTargets {
			var ipValues []string
			var hostValues []string
			for _, gwa := range cgwTarget.Status.Addresses {
				if *gwa.Type == gatewayapiv1.IPAddressType {
					ipValues = append(ipValues, gwa.Value)
				} else {
					hostValues = append(hostValues, gwa.Value)
				}
			}

			if len(ipValues) > 0 {
				clusterLbName := strings.ToLower(fmt.Sprintf("%s.%s", cgwTarget.GetShortCode(), lbName))
				ep = createOrUpdateEndpoint(clusterLbName, ipValues, kuadrantdnsv1alpha1.ARecordType, "", DefaultTTL, currentEndpoints)
				clusterEndpoints = append(clusterEndpoints, ep)
				hostValues = append(hostValues, clusterLbName)
			}

			for _, hostValue := range hostValues {
				ep = createOrUpdateEndpoint(geoLbName, []string{hostValue}, kuadrantdnsv1alpha1.CNAMERecordType, hostValue, DefaultTTL, currentEndpoints)
				ep.SetProviderSpecificProperty(kuadrantdnsv1alpha1.ProviderSpecificWeight, strconv.Itoa(cgwTarget.GetWeight()))
				clusterEndpoints = append(clusterEndpoints, ep)
			}
		}
		if len(clusterEndpoints) == 0 {
			continue
		}
		endpoints = append(endpoints, clusterEndpoints...)

		//Create lbName CNAME (lb-a1b2.shop.example.com -> default.lb-a1b2.shop.example.com)
		ep = createOrUpdateEndpoint(lbName, []string{geoLbName}, kuadrantdnsv1alpha1.CNAMERecordType, string(geoCode), DefaultCnameTTL, currentEndpoints)

		//Deal with the default geo externaldns first
		if geoCode.IsDefaultCode() {
			defaultEndpoint = ep
			// continue here as we will add the `defaultEndpoint` later
			continue
		} else if (geoCode == mcgTarget.GetDefaultGeo()) || defaultEndpoint == nil {
			// Ensure that a `defaultEndpoint` is always set, but the expected default takes precedence
			defaultEndpoint = createOrUpdateEndpoint(lbName, []string{geoLbName}, kuadrantdnsv1alpha1.CNAMERecordType, "default", DefaultCnameTTL, currentEndpoints)
		}

		ep.SetProviderSpecificProperty(kuadrantdnsv1alpha1.ProviderSpecificGeoCode, string(geoCode))

		endpoints = append(endpoints, ep)
	}

	if len(endpoints) > 0 {
		// Add the `defaultEndpoint`, this should always be set by this point if `endpoints` isn't empty
		defaultEndpoint.SetProviderSpecificProperty(kuadrantdnsv1alpha1.ProviderSpecificGeoCode, string(v1alpha1.WildcardGeo))
		endpoints = append(endpoints, defaultEndpoint)
		//Create gwListenerHost CNAME (shop.example.com -> lb-a1b2.shop.example.com)
		ep = createOrUpdateEndpoint(hostname, []string{lbName}, kuadrantdnsv1alpha1.CNAMERecordType, "", DefaultCnameTTL, currentEndpoints)
		endpoints = append(endpoints, ep)
	}

	return endpoints
}

func createOrUpdateEndpoint(dnsName string, targets externaldns.Targets, recordType kuadrantdnsv1alpha1.DNSRecordType, setIdentifier string,
	recordTTL externaldns.TTL, currentEndpoints map[string]*externaldns.Endpoint) (ep *externaldns.Endpoint) {
	ok := false
	endpointID := dnsName + setIdentifier
	if ep, ok = currentEndpoints[endpointID]; !ok {
		ep = &externaldns.Endpoint{}
		if setIdentifier != "" {
			ep.SetIdentifier = setIdentifier
		}
	}
	ep.DNSName = dnsName
	ep.RecordType = string(recordType)
	ep.Targets = targets
	ep.RecordTTL = recordTTL
	return ep
}

// removeDNSForDeletedListeners remove any DNSRecords that are associated with listeners that no longer exist in this gateway
func (dh *dnsHelper) removeDNSForDeletedListeners(ctx context.Context, upstreamGateway *gatewayapiv1.Gateway) error {
	dnsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	//List all dns records that belong to this gateway
	labelSelector := &client.MatchingLabels{
		LabelGatewayReference: upstreamGateway.Name,
	}
	if err := dh.List(ctx, dnsList, labelSelector, &client.ListOptions{Namespace: upstreamGateway.Namespace}); err != nil {
		return err
	}

	for i, dnsRecord := range dnsList.Items {
		listenerExists := false
		for _, listener := range upstreamGateway.Spec.Listeners {
			if listener.Name == gatewayapiv1.SectionName(dnsRecord.Labels[LabelListenerReference]) {
				listenerExists = true
				break
			}
		}
		if !listenerExists {
			if err := dh.Delete(ctx, &dnsList.Items[i], &client.DeleteOptions{}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

func (dh *dnsHelper) getManagedZoneForListener(ctx context.Context, ns string, listener gatewayapiv1.Listener) (*kuadrantdnsv1alpha1.ManagedZone, error) {
	var managedZones kuadrantdnsv1alpha1.ManagedZoneList
	if err := dh.List(ctx, &managedZones, client.InNamespace(ns)); err != nil {
		log.FromContext(ctx).Error(err, "unable to list managed zones for gateway ", "in ns", ns)
		return nil, err
	}
	host := string(*listener.Hostname)
	mz, _, err := findMatchingManagedZone(host, host, managedZones.Items)
	return mz, err
}

func dnsRecordName(gatewayName, listenerName string) string {
	return fmt.Sprintf("%s-%s", gatewayName, listenerName)
}

func (dh *dnsHelper) deleteDNSRecordForListener(ctx context.Context, owner metav1.Object, listener gatewayapiv1.Listener) error {
	recordName := dnsRecordName(owner.GetName(), string(listener.Name))
	dnsRecord := kuadrantdnsv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      recordName,
			Namespace: owner.GetNamespace(),
		},
	}
	return dh.Delete(ctx, &dnsRecord, &client.DeleteOptions{})
}

func isWildCardHost(host string) bool {
	return strings.HasPrefix(host, "*")
}
