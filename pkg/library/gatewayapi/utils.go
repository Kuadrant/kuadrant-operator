package gatewayapi

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func HostnamesFromListenerAndHTTPRoute(listener *gatewayapiv1.Listener, httpRoute *gatewayapiv1.HTTPRoute) []gatewayapiv1.Hostname {
	hostname := listener.Hostname
	if hostname == nil {
		hostname = ptr.To(gatewayapiv1.Hostname("*"))
	}
	hostnames := []gatewayapiv1.Hostname{*hostname}
	if routeHostnames := httpRoute.Spec.Hostnames; len(routeHostnames) > 0 {
		hostnames = lo.Filter(httpRoute.Spec.Hostnames, func(h gatewayapiv1.Hostname, _ int) bool {
			return utils.Name(h).SubsetOf(utils.Name(*hostname))
		})
	}
	return hostnames
}

func IsTargetRefHTTPRoute(targetRef gatewayapiv1alpha2.LocalPolicyTargetReference) bool {
	return targetRef.Group == (gatewayapiv1.GroupName) && targetRef.Kind == ("HTTPRoute")
}

func IsTargetRefGateway(targetRef gatewayapiv1alpha2.LocalPolicyTargetReference) bool {
	return targetRef.Group == (gatewayapiv1.GroupName) && targetRef.Kind == ("Gateway")
}

// TargetHostnames returns an array of hostnames coming from the network object (HTTPRoute, Gateway)
func TargetHostnames(targetNetworkObject client.Object) []string {
	hosts := make([]string, 0)
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		for _, hostname := range obj.Spec.Hostnames {
			hosts = append(hosts, string(hostname))
		}
	case *gatewayapiv1.Gateway:
		for idx := range obj.Spec.Listeners {
			if obj.Spec.Listeners[idx].Hostname != nil {
				hosts = append(hosts, string(*obj.Spec.Listeners[idx].Hostname))
			}
		}
	}

	if len(hosts) == 0 {
		hosts = append(hosts, "*")
	}

	return hosts
}

func GatewayHostnames(gw *gatewayapiv1.Gateway) []gatewayapiv1.Hostname {
	hostnames := make([]gatewayapiv1.Hostname, 0)
	if gw == nil {
		return hostnames
	}

	for idx := range gw.Spec.Listeners {
		if gw.Spec.Listeners[idx].Hostname != nil {
			hostnames = append(hostnames, *gw.Spec.Listeners[idx].Hostname)
		}
	}

	return hostnames
}

func GetGatewayWorkloadSelector(ctx context.Context, cli client.Client, gateway *gatewayapiv1.Gateway) (map[string]string, error) {
	address, found := utils.Find(
		gateway.Status.Addresses,
		func(address gatewayapiv1.GatewayStatusAddress) bool {
			return address.Type != nil && *address.Type == gatewayapiv1.HostnameAddressType
		},
	)
	if !found {
		return nil, fmt.Errorf("cannot find service Hostname in the Gateway status")
	}
	serviceNameParts := strings.Split(address.Value, ".")
	serviceKey := client.ObjectKey{
		Name:      serviceNameParts[0],
		Namespace: serviceNameParts[1],
	}
	return utils.GetServiceWorkloadSelector(ctx, cli, serviceKey)
}

// IsHTTPRouteAccepted returns true if a given HTTPRoute has the Accepted status condition added by any of its
// parentRefs; otherwise, it returns false
func IsHTTPRouteAccepted(httpRoute *gatewayapiv1.HTTPRoute) bool {
	acceptedParentRefs := GetRouteAcceptedParentRefs(httpRoute)

	if len(acceptedParentRefs) == 0 {
		return false
	}

	return len(acceptedParentRefs) == len(httpRoute.Spec.ParentRefs)
}

func IsPolicyAccepted(policy Policy) bool {
	condition := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func IsNotPolicyAccepted(policy Policy) bool {
	condition := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	return condition == nil || condition.Status != metav1.ConditionTrue
}

// GetRouteAcceptedGatewayParentKeys returns the object keys of all gateways that have accepted a given route
func GetRouteAcceptedGatewayParentKeys(route *gatewayapiv1.HTTPRoute) []client.ObjectKey {
	acceptedParentRefs := GetRouteAcceptedParentRefs(route)

	gatewayParentRefs := utils.Filter(acceptedParentRefs, IsParentGateway)

	return utils.Map(gatewayParentRefs, func(p gatewayapiv1.ParentReference) client.ObjectKey {
		return client.ObjectKey{
			Name:      string(p.Name),
			Namespace: string(ptr.Deref(p.Namespace, gatewayapiv1.Namespace(route.Namespace))),
		}
	})
}

// GetRouteAcceptedParentRefs returns the list of parentRefs for which a given route has the Accepted status condition
func GetRouteAcceptedParentRefs(route *gatewayapiv1.HTTPRoute) []gatewayapiv1.ParentReference {
	if route == nil {
		return nil
	}

	return utils.Filter(route.Spec.ParentRefs, func(p gatewayapiv1.ParentReference) bool {
		for _, parentStatus := range route.Status.RouteStatus.Parents {
			if reflect.DeepEqual(parentStatus.ParentRef, p) && meta.IsStatusConditionTrue(parentStatus.Conditions, string(gatewayapiv1.RouteConditionAccepted)) {
				return true
			}
		}
		return false
	})
}

func IsParentGateway(ref gatewayapiv1.ParentReference) bool {
	return (ref.Kind == nil || *ref.Kind == "Gateway") && (ref.Group == nil || *ref.Group == gatewayapiv1.GroupName)
}

// FilterValidSubdomains returns every subdomain that is a subset of at least one of the (super) domains specified in the first argument.
func FilterValidSubdomains(domains, subdomains []gatewayapiv1.Hostname) []gatewayapiv1.Hostname {
	arr := make([]gatewayapiv1.Hostname, 0)
	for _, subsubdomain := range subdomains {
		if _, found := utils.Find(domains, func(domain gatewayapiv1.Hostname) bool {
			return utils.Name(subsubdomain).SubsetOf(utils.Name(domain))
		}); found {
			arr = append(arr, subsubdomain)
		}
	}
	return arr
}

func IsGatewayAPIInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, gatewayapiv1.GroupName, "HTTPRoute", gatewayapiv1.GroupVersion.Version)
}

func IsCertManagerInstalled(restMapper meta.RESTMapper, logger logr.Logger) (bool, error) {
	if ok, err := utils.IsCRDInstalled(restMapper, certmanager.GroupName, certmanv1.CertificateKind, certmanv1.SchemeGroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "CertManager CRD was not installed", "group", certmanager.GroupName, "kind", certmanv1.CertificateKind, "version", certmanv1.SchemeGroupVersion.Version)
		return false, err
	}

	if ok, err := utils.IsCRDInstalled(restMapper, certmanager.GroupName, certmanv1.IssuerKind, certmanv1.SchemeGroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "CertManager CRD was not installed", "group", certmanager.GroupName, "kind", certmanv1.IssuerKind, "version", certmanv1.SchemeGroupVersion.Version)
		return false, err
	}

	if ok, err := utils.IsCRDInstalled(restMapper, certmanager.GroupName, certmanv1.ClusterIssuerKind, certmanv1.SchemeGroupVersion.Version); !ok || err != nil {
		logger.V(1).Error(err, "CertManager CRD was not installed", "group", certmanager.GroupName, "kind", certmanv1.ClusterIssuerKind, "version", certmanv1.SchemeGroupVersion.Version)
		return false, err
	}

	return true, nil
}

// GetGatewayParentRefsFromRoute returns the list of parentRefs that are Gateway typed
func GetGatewayParentRefsFromRoute(route *gatewayapiv1.HTTPRoute) []gatewayapiv1.ParentReference {
	if route == nil {
		return nil
	}
	return utils.Filter(route.Spec.ParentRefs, IsParentGateway)
}

// GetGatewayParentKeys returns the object keys of all parent gateways
func GetGatewayParentKeys(route *gatewayapiv1.HTTPRoute) []client.ObjectKey {
	gatewayParentRefs := GetGatewayParentRefsFromRoute(route)

	return utils.Map(gatewayParentRefs, func(p gatewayapiv1.ParentReference) client.ObjectKey {
		return client.ObjectKey{
			Name:      string(p.Name),
			Namespace: string(ptr.Deref(p.Namespace, gatewayapiv1.Namespace(route.Namespace))),
		}
	})
}

func EqualLocalPolicyTargetReferencesWithSectionName(a, b []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
	return len(a) == len(b) && lo.EveryBy(a, func(aTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
		return lo.SomeBy(b, func(bTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
			return aTargetRef.Group == bTargetRef.Group && aTargetRef.Kind == bTargetRef.Kind && aTargetRef.Name == bTargetRef.Name && ptr.Deref(aTargetRef.SectionName, gatewayapiv1alpha2.SectionName("")) == ptr.Deref(bTargetRef.SectionName, gatewayapiv1alpha2.SectionName(""))
		})
	})
}

// PolicyStatusConditionsFromAncestor returns the conditions from a policy status for a given ancestor
func PolicyStatusConditionsFromAncestor(policyStatus gatewayapiv1alpha2.PolicyStatus, controllerName gatewayapiv1.GatewayController, ancestor gatewayapiv1.ParentReference, defaultNamespace gatewayapiv1.Namespace) []metav1.Condition {
	if status, found := lo.Find(policyStatus.Ancestors, func(a gatewayapiv1alpha2.PolicyAncestorStatus) bool {
		defaultGroup := gatewayapiv1alpha2.Group(gatewayapiv1.GroupName)
		defaultKind := gatewayapiv1alpha2.Kind(machinery.GatewayGroupKind.Kind)
		defaultSectionName := gatewayapiv1.SectionName("")
		ref := a.AncestorRef
		return a.ControllerName == controllerName &&
			ptr.Deref(ref.Group, defaultGroup) == ptr.Deref(ancestor.Group, defaultGroup) &&
			ptr.Deref(ref.Kind, defaultKind) == ptr.Deref(ancestor.Kind, defaultKind) &&
			ptr.Deref(ref.Namespace, defaultNamespace) == ptr.Deref(ancestor.Namespace, defaultNamespace) &&
			ref.Name == ancestor.Name &&
			ptr.Deref(ref.SectionName, defaultSectionName) == ptr.Deref(ancestor.SectionName, defaultSectionName)
	}); found {
		return status.Conditions
	}
	return nil
}

func IsListenerReady(listener *gatewayapiv1.Listener, gateway *gatewayapiv1.Gateway) bool {
	listenerStatus, found := lo.Find(gateway.Status.Listeners, func(s gatewayapiv1.ListenerStatus) bool {
		return s.Name == listener.Name
	})
	if !found {
		return false
	}
	return meta.IsStatusConditionTrue(listenerStatus.Conditions, string(gatewayapiv1.ListenerConditionProgrammed))
}

func IsHTTPRouteReady(httpRoute *gatewayapiv1.HTTPRoute, gateway *gatewayapiv1.Gateway, controllerName gatewayapiv1.GatewayController) bool {
	routeStatus, found := lo.Find(httpRoute.Status.Parents, func(s gatewayapiv1.RouteParentStatus) bool {
		ref := s.ParentRef
		return s.ControllerName == controllerName &&
			ptr.Deref(ref.Group, gatewayapiv1.Group(gatewayapiv1.GroupName)) == gatewayapiv1.Group(gateway.GroupVersionKind().Group) &&
			ptr.Deref(ref.Kind, gatewayapiv1.Kind(machinery.GatewayGroupKind.Kind)) == gatewayapiv1.Kind(gateway.GroupVersionKind().Kind) &&
			ptr.Deref(ref.Namespace, gatewayapiv1.Namespace(httpRoute.GetNamespace())) == gatewayapiv1.Namespace(gateway.GetNamespace()) &&
			ref.Name == gatewayapiv1.ObjectName(gateway.GetName())
	})
	if !found {
		return false
	}
	return meta.IsStatusConditionTrue(routeStatus.Conditions, string(gatewayapiv1.RouteConditionAccepted))
}
