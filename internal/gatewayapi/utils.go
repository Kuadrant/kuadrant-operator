package gatewayapi

import (
	"reflect"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
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

// IsHTTPRouteAccepted returns true if a given HTTPRoute has the Accepted status condition added by any of its
// parentRefs; otherwise, it returns false
func IsHTTPRouteAccepted(httpRoute *gatewayapiv1.HTTPRoute) bool {
	acceptedParentRefs := GetRouteAcceptedParentRefs(httpRoute)

	if len(acceptedParentRefs) == 0 {
		return false
	}

	return len(acceptedParentRefs) == len(httpRoute.Spec.ParentRefs)
}

// GetRouteAcceptedParentRefs returns the list of parentRefs for which a given route has the Accepted status condition
func GetRouteAcceptedParentRefs(route *gatewayapiv1.HTTPRoute) []gatewayapiv1.ParentReference {
	if route == nil {
		return nil
	}

	return utils.Filter(route.Spec.ParentRefs, func(p gatewayapiv1.ParentReference) bool {
		for _, parentStatus := range route.Status.Parents {
			if reflect.DeepEqual(parentStatus.ParentRef, p) && meta.IsStatusConditionTrue(parentStatus.Conditions, string(gatewayapiv1.RouteConditionAccepted)) {
				return true
			}
		}
		return false
	})
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

func IsListenerReady(listener *gatewayapiv1.Listener, gateway *gatewayapiv1.Gateway) bool {
	listenerStatus, found := lo.Find(gateway.Status.Listeners, func(s gatewayapiv1.ListenerStatus) bool {
		return s.Name == listener.Name
	})
	if !found {
		return false
	}
	return meta.IsStatusConditionTrue(listenerStatus.Conditions, string(gatewayapiv1.ListenerConditionProgrammed))
}

func IsPolicyAccepted(policy Policy) bool {
	condition := meta.FindStatusCondition(policy.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted))
	return condition != nil && condition.Status == metav1.ConditionTrue
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

func EqualLocalPolicyTargetReferencesWithSectionName(a, b []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
	return len(a) == len(b) && lo.EveryBy(a, func(aTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
		return lo.SomeBy(b, func(bTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
			return aTargetRef.Group == bTargetRef.Group && aTargetRef.Kind == bTargetRef.Kind && aTargetRef.Name == bTargetRef.Name && ptr.Deref(aTargetRef.SectionName, gatewayapiv1alpha2.SectionName("")) == ptr.Deref(bTargetRef.SectionName, gatewayapiv1alpha2.SectionName(""))
		})
	})
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
