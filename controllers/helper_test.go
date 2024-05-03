//go:build integration

package controllers

import (
	"context"
	"slices"
	"strings"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

const (
	TestTimeoutMedium        = time.Second * 10
	TestTimeoutLong          = time.Second * 30
	TestRetryIntervalMedium  = time.Millisecond * 250
	TestGatewayName          = "test-placed-gateway"
	TestClusterNameOne       = "test-placed-control"
	TestClusterNameTwo       = "test-placed-workload-1"
	TestHostOne              = "test.example.com"
	TestHostTwo              = "other.test.example.com"
	TestHostWildcard         = "*.example.com"
	TestListenerNameWildcard = "wildcard"
	TestListenerNameOne      = "test-listener-1"
	TestListenerNameTwo      = "test-listener-2"
	TestIPAddressOne         = "172.0.0.1"
	TestIPAddressTwo         = "172.0.0.2"
	TestHTTPRouteName        = "toystore-route"
)

func testRLPIsNotAccepted(ctx context.Context, rlpKey client.ObjectKey) func() bool {
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

func testHTTPRouteWithoutDirectBackReference(routeKey client.ObjectKey, annotationName string) func() bool {
	return testNetworkResourceWithoutDirectBackReference(routeKey, &gatewayapiv1.HTTPRoute{}, annotationName)
}

func testGatewayWithoutDirectBackReference(gwKey client.ObjectKey, annotationName string) func() bool {
	return testNetworkResourceWithoutDirectBackReference(gwKey, &gatewayapiv1.Gateway{}, annotationName)
}

func testNetworkResourceWithoutDirectBackReference(objKey client.ObjectKey, obj client.Object, annotationName string) func() bool {
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

func testHTTPRouteHasDirectBackReference(routeKey client.ObjectKey, annotationName, annotationVal string) func() bool {
	return testNetworkResourceHasDirectBackReference(routeKey, &gatewayapiv1.HTTPRoute{}, annotationName, annotationVal)
}

func testGatewayHasDirectBackReference(gwKey client.ObjectKey, annotationName, annotationVal string) func() bool {
	return testNetworkResourceHasDirectBackReference(gwKey, &gatewayapiv1.Gateway{}, annotationName, annotationVal)
}

func testNetworkResourceHasDirectBackReference(objKey client.ObjectKey, obj client.Object, annotationName, annotationVal string) func() bool {
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

func testObjectDoesNotExist(obj client.Object) func() bool {
	return func() bool {
		err := testClient().Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil && apierrors.IsNotFound(err) {
			return true
		}

		logf.Log.V(1).Info("object not deleted", "object", client.ObjectKeyFromObject(obj),
			"kind", obj.GetObjectKind().GroupVersionKind())
		return false
	}
}

// DNS

func testBuildManagedZone(name, ns, domainName, secretName string) *kuadrantdnsv1alpha1.ManagedZone {
	return &kuadrantdnsv1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantdnsv1alpha1.ManagedZoneSpec{
			DomainName:  domainName,
			Description: domainName,
			SecretRef: kuadrantdnsv1alpha1.ProviderRef{
				Name: secretName,
			},
		},
	}
}

func testBuildInMemoryCredentialsSecret(name, ns string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: map[string][]byte{},
		Type: "kuadrant.io/inmemory",
	}
}

// testEndpointsTraversable consumes an array of endpoints and returns a boolean
// indicating presence of that path from host to all destinations
// this function DOES NOT report a presence of an endpoint with one of destinations DNSNames
func testEndpointsTraversable(endpoints []*endpoint.Endpoint, host string, destinations []string) bool {
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
					allTargetsFound = allTargetsFound || testEndpointsTraversable(endpoints, target, []string{destination})
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

func testBuildGatewayClass(name, ns, controllerName string) *gatewayapiv1.GatewayClass {
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

func testBuildSelfSignedIssuer(name, ns string) (*certmanv1.Issuer, *certmanmetav1.ObjectReference) {
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

func testBuildSelfSignedClusterIssuer(name, ns string) (*certmanv1.ClusterIssuer, *certmanmetav1.ObjectReference) {
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
