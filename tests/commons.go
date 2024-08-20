//go:build integration

package tests

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/external-dns/endpoint"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	kuadrantdnsbuilder "github.com/kuadrant/dns-operator/pkg/builder"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	TimeoutMedium        = time.Second * 10
	TimeoutLong          = time.Second * 30
	RetryIntervalMedium  = time.Millisecond * 250
	GatewayName          = "test-placed-gateway"
	ClusterNameOne       = "test-placed-control"
	ClusterNameTwo       = "test-placed-workload-1"
	ListenerNameWildcard = "wildcard"
	ListenerNameOne      = "test-listener-1"
	ListenerNameTwo      = "test-listener-2"
	IPAddressOne         = "172.0.0.1"
	IPAddressTwo         = "172.0.0.2"
	HTTPRouteName        = "toystore-route"
)

func HostWildcard(domain string) string {
	return fmt.Sprintf("*.%s", domain)
}

func HostOne(domain string) string {
	return fmt.Sprintf("%s.%s", "test", domain)
}

func HostTwo(domain string) string {
	return fmt.Sprintf("%s.%s", "other.test", domain)
}

func BuildBasicGateway(gwName, ns string, mutateFns ...func(*gatewayapiv1.Gateway)) *gatewayapiv1.Gateway {
	gateway := &gatewayapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gwName,
			Namespace:   ns,
			Labels:      map[string]string{"app": "rlptest"},
			Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
		},
		Spec: gatewayapiv1.GatewaySpec{
			GatewayClassName: "istio",
			Listeners: []gatewayapiv1.Listener{
				{
					Name:     "default",
					Port:     gatewayapiv1.PortNumber(80),
					Protocol: "HTTP",
				},
			},
		},
	}
	for _, mutateFn := range mutateFns {
		mutateFn(gateway)
	}
	return gateway
}

func DeleteNamespace(ctx context.Context, cl client.Client, namespace string) {
	desiredTestNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Eventually(func(g Gomega) {
		err := cl.Delete(ctx, desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		g.Expect(err).ToNot(BeNil())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())
}

func DeleteNamespaceCallback(ctx context.Context, cl client.Client, namespace string) func() {
	return func() {
		DeleteNamespace(ctx, cl, namespace)
	}
}

func CreateNamespace(ctx context.Context, cl client.Client) string {
	nsObject := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-namespace-"},
	}
	Expect(cl.Create(ctx, nsObject)).To(Succeed())

	return nsObject.Name
}

func ApplyKuadrantCR(ctx context.Context, cl client.Client, namespace string) {
	ApplyKuadrantCRWithName(ctx, cl, namespace, "kuadrant-sample")
}

func ApplyKuadrantCRWithName(ctx context.Context, cl client.Client, namespace, name string, mutateFns ...func(*kuadrantv1beta1.Kuadrant)) {
	kuadrantCR := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	for _, mutateFn := range mutateFns {
		mutateFn(kuadrantCR)
	}

	err := cl.Create(ctx, kuadrantCR)
	Expect(err).ToNot(HaveOccurred())
}

func GatewayIsReady(ctx context.Context, cl client.Client, gateway *gatewayapiv1.Gateway) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
		if err != nil {
			logf.Log.V(1).Info("gateway not read", "gateway", client.ObjectKeyFromObject(gateway), "error", err)
			return false
		}

		if !meta.IsStatusConditionTrue(existingGateway.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed)) {
			logf.Log.V(1).Info("gateway not programmed", "gateway", client.ObjectKeyFromObject(gateway))
			return false
		}

		return true
	}
}

func BuildBasicHttpRoute(routeName, gwName, ns string, hostnames []string, mutateFns ...func(*gatewayapiv1.HTTPRoute)) *gatewayapiv1.HTTPRoute {
	route := &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: ns,
			Labels:    map[string]string{"app": "rlptest"},
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:      gatewayapiv1.ObjectName(gwName),
						Namespace: ptr.To(gatewayapiv1.Namespace(ns)),
					},
				},
			},
			Hostnames: utils.Map(hostnames, func(hostname string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(hostname) }),
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/toy"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			},
		},
	}
	for _, mutateFn := range mutateFns {
		mutateFn(route)
	}
	return route
}

func RouteIsAccepted(ctx context.Context, cl client.Client, routeKey client.ObjectKey) func() bool {
	return func() bool {
		route := &gatewayapiv1.HTTPRoute{}
		err := cl.Get(ctx, routeKey, route)

		if err != nil {
			logf.Log.V(1).Info("httpRoute not read", "route", routeKey, "error", err)
			return false
		}

		if !kuadrantgatewayapi.IsHTTPRouteAccepted(route) {
			logf.Log.V(1).Info("httpRoute not accepted", "route", routeKey)
			return false
		}

		return true
	}
}

func BuildMultipleRulesHttpRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
	route := BuildBasicHttpRoute(routeName, gwName, ns, hostnames)
	route.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
		{ // POST|DELETE /admin*
			Matches: []gatewayapiv1.HTTPRouteMatch{
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/admin"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
				},
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/admin"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("DELETE")),
				},
			},
		},
		{ // GET /private*
			Matches: []gatewayapiv1.HTTPRouteMatch{
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/private"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
				},
			},
		},
	}
	return route
}

func DeleteKuadrantCR(ctx context.Context, cl client.Client, namespace string) {
	k := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: "kuadrant-sample", Namespace: namespace}}
	Eventually(func(g Gomega) {
		err := cl.Delete(ctx, k)
		g.Expect(err).To(HaveOccurred())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())
}

func RLPIsAccepted(ctx context.Context, cl client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, cl, rlpKey, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func RLPIsEnforced(ctx context.Context, cl client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, cl, rlpKey, string(kuadrant.PolicyConditionEnforced))
}

func RLPIsConditionTrue(ctx context.Context, cl client.Client, rlpKey client.ObjectKey, condition string) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := cl.Get(ctx, rlpKey, existingRLP)
		if err != nil {
			logf.Log.V(1).Error(err, "ratelimitpolicy not read", "rlp", rlpKey)
			return false
		}

		return meta.IsStatusConditionTrue(existingRLP.Status.Conditions, condition)
	}
}

func RLPEnforcedCondition(ctx context.Context, cl client.Client, rlpKey client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) bool {
	p := &kuadrantv1beta2.RateLimitPolicy{}
	if err := cl.Get(ctx, rlpKey, p); err != nil {
		return false
	}

	cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
	if cond == nil {
		return false
	}

	return cond.Reason == string(reason) && cond.Message == message
}

func WasmPluginIsAvailable(ctx context.Context, cl client.Client, key client.ObjectKey) func() bool {
	return func() bool {
		wp := &istioclientgoextensionv1alpha1.WasmPlugin{}
		err := cl.Get(ctx, key, wp)
		if err != nil {
			logf.Log.V(1).Info("wasmplugin not read", "key", key, "error", err)
			return false
		}

		// Unfortunately, WasmPlugin does not have status yet
		// Leaving this here for future use
		//if !meta.IsStatusConditionTrue(wp.Status.Conditions, "Available") {
		//	return false
		//}

		return true
	}
}

func IsAuthPolicyAcceptedAndEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, cl, policy)() && IsAuthPolicyEnforced(ctx, cl, policy)()
	}
}

func IsAuthPolicyAcceptedAndNotEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, cl, policy)() && !IsAuthPolicyEnforced(ctx, cl, policy)()
	}
}

func IsAuthPolicyAccepted(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, cl, policy, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func IsAuthPolicyEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, cl, policy, string(kuadrant.PolicyConditionEnforced))
}

func IsAuthPolicyEnforcedCondition(ctx context.Context, cl client.Client, key client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) func() bool {
	return func() bool {
		p := &kuadrantv1beta2.AuthPolicy{}
		if err := cl.Get(ctx, key, p); err != nil {
			return false
		}

		cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
		if cond == nil {
			return false
		}

		return cond.Reason == string(reason) && cond.Message == message
	}
}

func IsAuthPolicyConditionTrue(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy, condition string) func() bool {
	return func() bool {
		existingPolicy := &kuadrantv1beta2.AuthPolicy{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
		return err == nil && meta.IsStatusConditionTrue(existingPolicy.Status.Conditions, condition)
	}
}

func RLPIsNotAccepted(ctx context.Context, k8sClient client.Client, rlpKey client.ObjectKey) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := k8sClient.Get(ctx, rlpKey, existingRLP)
		if err != nil {
			logf.Log.V(1).Info("ratelimitpolicy not read", "rlp", rlpKey, "error", err)
			return false
		}
		if meta.IsStatusConditionTrue(existingRLP.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted)) {
			logf.Log.V(1).Info("ratelimitpolicy still accepted", "rlp", rlpKey)
			return false
		}

		return true
	}
}

func HTTPRouteWithoutDirectBackReference(k8sClient client.Client, routeKey client.ObjectKey, annotationName string) func() bool {
	return NetworkResourceWithoutDirectBackReference(k8sClient, routeKey, &gatewayapiv1.HTTPRoute{}, annotationName)
}

func GatewayWithoutDirectBackReference(k8sClient client.Client, gwKey client.ObjectKey, annotationName string) func() bool {
	return NetworkResourceWithoutDirectBackReference(k8sClient, gwKey, &gatewayapiv1.Gateway{}, annotationName)
}

func NetworkResourceWithoutDirectBackReference(k8sClient client.Client, objKey client.ObjectKey, obj client.Object, annotationName string) func() bool {
	return func() bool {
		err := k8sClient.Get(context.Background(), objKey, obj)
		if err != nil {
			logf.Log.V(1).Info("object not read", "object", objKey,
				"kind", obj.GetObjectKind().GroupVersionKind(), "error", err)
			return false
		}

		_, ok := obj.GetAnnotations()[annotationName]
		if ok {
			logf.Log.V(1).Info("object sill has the direct ref annotation",
				"object", objKey, "kind", obj.GetObjectKind().GroupVersionKind())
			return false
		}

		return true
	}
}

func HTTPRouteHasDirectBackReference(k8sClient client.Client, routeKey client.ObjectKey, annotationName, annotationVal string) func() bool {
	return NetworkResourceHasDirectBackReference(k8sClient, routeKey, &gatewayapiv1.HTTPRoute{}, annotationName, annotationVal)
}

func GatewayHasDirectBackReference(k8sClient client.Client, gwKey client.ObjectKey, annotationName, annotationVal string) func() bool {
	return NetworkResourceHasDirectBackReference(k8sClient, gwKey, &gatewayapiv1.Gateway{}, annotationName, annotationVal)
}

func NetworkResourceHasDirectBackReference(k8sClient client.Client, objKey client.ObjectKey, obj client.Object, annotationName, annotationVal string) func() bool {
	return func() bool {
		err := k8sClient.Get(context.Background(), objKey, obj)
		if err != nil {
			logf.Log.V(1).Info("object not read", "object", objKey,
				"kind", obj.GetObjectKind().GroupVersionKind(), "error", err)
			return false
		}

		val, ok := obj.GetAnnotations()[annotationName]
		if !ok {
			logf.Log.V(1).Info("object does not have the direct ref annotation",
				"object", objKey, "kind", obj.GetObjectKind().GroupVersionKind())
			return false
		}

		if val != annotationVal {
			logf.Log.V(1).Info("object direct ref annotation value does not match",
				"object", objKey, "kind", obj.GetObjectKind().GroupVersionKind(),
				"val", val)
			return false
		}

		return true
	}
}

func ObjectDoesNotExist(k8sClient client.Client, obj client.Object) func() bool {
	return func() bool {
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil && apierrors.IsNotFound(err) {
			return true
		}

		logf.Log.V(1).Info("object not deleted", "object", client.ObjectKeyFromObject(obj),
			"kind", obj.GetObjectKind().GroupVersionKind())
		return false
	}
}

// DNS

func BuildInMemoryCredentialsSecret(name, ns, initDomain string) *corev1.Secret {
	return kuadrantdnsbuilder.NewProviderBuilder(name, ns).
		For(kuadrantdnsv1alpha1.SecretTypeKuadrantInmemory).
		WithZonesInitialisedFor(initDomain).
		Build()
}

// EndpointsTraversable consumes an array of endpoints and returns a boolean
// indicating presence of that path from host to all destinations
// this function DOES NOT report a presence of an endpoint with one of destinations DNSNames
func EndpointsTraversable(endpoints []*endpoint.Endpoint, host string, destinations []string) bool {
	allDestinationsFound := len(destinations) > 0
	for _, destination := range destinations {
		allTargetsFound := false
		for _, ep := range endpoints {
			// the host exists as a DNSName on an endpoint
			if ep.DNSName == host {
				// we found destination in the targets of the endpoint.
				if slices.Contains(ep.Targets, destination) {
					return true
				}
				// destination is not found on the endpoint. Use target as a host and check for existence of Endpoints with such a DNSName
				for _, target := range ep.Targets {
					// if at least one returns as true allTargetsFound will be locked in true
					// this means that at least one of the targets on the endpoint leads to the destination
					allTargetsFound = allTargetsFound || EndpointsTraversable(endpoints, target, []string{destination})
				}
			}
		}
		// we must match all destinations
		allDestinationsFound = allDestinationsFound && allTargetsFound
	}
	// there are no destinations to look for: len(destinations) == 0 locks allDestinationsFound into false
	// or every destination was matched to a target on the endpoint
	return allDestinationsFound
}

//Gateway

func BuildGatewayClass(name, ns, controllerName string) *gatewayapiv1.GatewayClass {
	return &gatewayapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: gatewayapiv1.GatewayClassSpec{
			ControllerName: gatewayapiv1.GatewayController(controllerName),
		},
	}
}

// GatewayBuilder wrapper for Gateway builder helper
type GatewayBuilder struct {
	*gatewayapiv1.Gateway
}

func NewGatewayBuilder(gwName, gwClassName, ns string) *GatewayBuilder {
	return &GatewayBuilder{
		&gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gwName,
				Namespace: ns,
			},
			Spec: gatewayapiv1.GatewaySpec{
				GatewayClassName: gatewayapiv1.ObjectName(gwClassName),
				Listeners:        []gatewayapiv1.Listener{},
			},
		},
	}
}

func (t *GatewayBuilder) WithListener(listener gatewayapiv1.Listener) *GatewayBuilder {
	t.Spec.Listeners = append(t.Spec.Listeners, listener)
	return t
}

func (t *GatewayBuilder) WithLabels(labels map[string]string) *GatewayBuilder {
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	for key, value := range labels {
		t.Labels[key] = value
	}
	return t
}

func (t *GatewayBuilder) WithHTTPListener(name, hostname string) *GatewayBuilder {
	typedHostname := gatewayapiv1.Hostname(hostname)
	t.WithListener(gatewayapiv1.Listener{
		Name:     gatewayapiv1.SectionName(name),
		Hostname: &typedHostname,
		Port:     gatewayapiv1.PortNumber(80),
		Protocol: gatewayapiv1.HTTPProtocolType,
	})
	return t
}

func (t *GatewayBuilder) WithHTTPSListener(hostname, tlsSecretName string) *GatewayBuilder {
	typedHostname := gatewayapiv1.Hostname(hostname)
	typedNamespace := gatewayapiv1.Namespace(t.GetNamespace())
	typedNamed := gatewayapiv1.SectionName(strings.Replace(hostname, "*", "wildcard", 1))
	t.WithListener(gatewayapiv1.Listener{
		Name:     typedNamed,
		Hostname: &typedHostname,
		Port:     gatewayapiv1.PortNumber(443),
		Protocol: gatewayapiv1.HTTPSProtocolType,
		TLS: &gatewayapiv1.GatewayTLSConfig{
			Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
			CertificateRefs: []gatewayapiv1.SecretObjectReference{
				{
					Name:      gatewayapiv1.ObjectName(tlsSecretName),
					Namespace: ptr.To(typedNamespace),
				},
			},
		},
	})
	return t
}

//CertMan

func BuildSelfSignedIssuer(name, ns string) (*certmanv1.Issuer, *certmanmetav1.ObjectReference) {
	issuer := &certmanv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: createSelfSignedIssuerSpec(),
	}
	objRef := &certmanmetav1.ObjectReference{
		Group: certmanv1.SchemeGroupVersion.Group,
		Kind:  certmanv1.IssuerKind,
		Name:  issuer.Name,
	}
	return issuer, objRef
}

func BuildSelfSignedClusterIssuer(name, ns string) (*certmanv1.ClusterIssuer, *certmanmetav1.ObjectReference) {
	issuer := &certmanv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: createSelfSignedIssuerSpec(),
	}
	objRef := &certmanmetav1.ObjectReference{
		Group: certmanv1.SchemeGroupVersion.Group,
		Kind:  certmanv1.ClusterIssuerKind,
		Name:  issuer.Name,
	}
	return issuer, objRef
}

func createSelfSignedIssuerSpec() certmanv1.IssuerSpec {
	return certmanv1.IssuerSpec{
		IssuerConfig: certmanv1.IssuerConfig{
			SelfSigned: &certmanv1.SelfSignedIssuer{},
		},
	}
}

func LimitadorIsReady(ctx context.Context, cl client.Client, lKey client.ObjectKey) func(g Gomega) {
	return func(g Gomega) {
		existing := &limitadorv1alpha1.Limitador{}
		g.Expect(cl.Get(ctx, lKey, existing)).To(Succeed())
		g.Expect(meta.IsStatusConditionTrue(existing.Status.Conditions, limitadorv1alpha1.StatusConditionReady)).To(BeTrue())
	}
}

func KuadrantIsReady(ctx context.Context, cl client.Client, key client.ObjectKey) func(g Gomega) {
	return func(g Gomega) {
		kuadrantCR := &kuadrantv1beta1.Kuadrant{}
		err := cl.Get(ctx, key, kuadrantCR)
		g.Expect(err).To(Succeed())
		g.Expect(meta.IsStatusConditionTrue(kuadrantCR.Status.Conditions, "Ready")).To(BeTrue())
	}
}
