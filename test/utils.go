//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	istioapis "istio.io/istio/operator/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
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
	HostOne              = "test.example.com"
	HostTwo              = "other.test.example.com"
	HostWildcard         = "*.example.com"
	ListenerNameWildcard = "wildcard"
	ListenerNameOne      = "test-listener-1"
	ListenerNameTwo      = "test-listener-2"
	IPAddressOne         = "172.0.0.1"
	IPAddressTwo         = "172.0.0.2"
	HTTPRouteName        = "toystore-route"
)

func ApplyKuadrantCR(k8sClient client.Client, namespace string) {
	ApplyKuadrantCRWithName(k8sClient, namespace, "kuadrant-sample")
}

func ApplyKuadrantCRWithName(k8sClient client.Client, namespace, name string) {
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
	err := k8sClient.Create(context.Background(), kuadrantCR)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func DeleteKuadrantCR(ctx context.Context, k8sClient client.Client, namespace string) {
	k := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: "kuadrant-sample", Namespace: namespace}}
	gomega.Eventually(func(g gomega.Gomega) {
		err := k8sClient.Delete(ctx, k)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(apierrors.IsNotFound(err)).To(gomega.BeTrue())
	}).WithContext(ctx).Should(gomega.Succeed())
}

func CreateNamespaceWithContext(ctx context.Context, k8sClient client.Client) string {
	nsObject := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-namespace-"},
	}
	gomega.Expect(k8sClient.Create(ctx, nsObject)).To(gomega.Succeed())

	return nsObject.Name
}

func CreateNamespace(k8sClient client.Client) string {
	return CreateNamespaceWithContext(context.Background(), k8sClient)
}

func DeleteNamespaceCallbackWithContext(ctx context.Context, k8sClient client.Client, namespace string) {
	desiredNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	gomega.Eventually(func(g gomega.Gomega) {
		err := k8sClient.Delete(ctx, desiredNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		g.Expect(err).ToNot(gomega.BeNil())
		g.Expect(apierrors.IsNotFound(err)).To(gomega.BeTrue())
	}).WithContext(ctx).Should(gomega.Succeed())
}

func DeleteNamespaceCallback(k8sClient client.Client, namespace string) func() {
	return func() {
		desiredNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		err := k8sClient.Delete(context.Background(), desiredNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))

		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		existingNamespace := &corev1.Namespace{}
		gomega.Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: namespace}, existingNamespace)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}, 3*time.Minute, 2*time.Second).Should(gomega.BeTrue())
	}
}

func ApplyResources(fileName string, k8sClient client.Client, ns string) error {
	logf.Log.Info("ApplyResources", "Resource file", fileName)
	data, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}

	codec := serializer.NewCodecFactory(scheme.Scheme)
	decoder := codec.UniversalDeserializer()

	// the maximum size used to buffer a doc 5M
	buf := make([]byte, 5*1024*1024)
	docDecoder := yaml.NewDocumentDecoder(io.NopCloser(bytes.NewReader(data)))

	for {
		n, err := docDecoder.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		if n == 0 {
			// empty docs
			continue
		}

		docData := buf[:n]
		obj, _, err := decoder.Decode(docData, nil, nil)
		if err != nil {
			logf.Log.Info("Document decode error", "error", err)
			continue
		}

		metadata, err := meta.Accessor(obj)
		if err != nil {
			return err
		}
		metadata.SetNamespace(ns)

		err = CreateOrUpdateK8SObject(obj, k8sClient)
		if err != nil {
			return err
		}
	}
	return nil
}

func CreateOrUpdateK8SObject(obj runtime.Object, k8sClient client.Client) error {
	k8sObj, ok := obj.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}
	logf.Log.Info("CreateOrUpdateK8SObject", "GKV", k8sObj.GetObjectKind(), "name", k8sObj.GetName(), "namespace", k8sObj.GetNamespace())

	err := k8sClient.Create(context.Background(), k8sObj)
	if err == nil {
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	// Already exists
	currentObj := k8sObj.DeepCopyObject()
	k8sCurrentObj, ok := currentObj.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObj), k8sCurrentObj)
	if err != nil {
		return err
	}

	objCopy := k8sObj.DeepCopyObject()

	objCopyMetadata, err := meta.Accessor(objCopy)
	if err != nil {
		return err
	}

	objCopyMetadata.SetResourceVersion(k8sCurrentObj.GetResourceVersion())

	k8sObjCopy, ok := objCopy.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}

	return k8sClient.Update(context.Background(), k8sObjCopy)
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

func BuildBasicHTTPRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
	return &gatewayapiv1.HTTPRoute{
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
}

func BuildMultipleRulesHTTPRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
	route := BuildBasicHTTPRoute(routeName, gwName, ns, hostnames)
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

func RouteIsAccepted(k8sClient client.Client, routeKey client.ObjectKey) func() bool {
	return func() bool {
		route := &gatewayapiv1.HTTPRoute{}
		err := k8sClient.Get(context.Background(), routeKey, route)

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

func GatewayIsReady(k8sClient client.Client, gateway *gatewayapiv1.Gateway) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
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

func WasmPluginIsAvailable(k8sClient client.Client, key client.ObjectKey) func() bool {
	return func() bool {
		wp := &istioclientgoextensionv1alpha1.WasmPlugin{}
		err := k8sClient.Get(context.Background(), key, wp)
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

func RLPIsAccepted(ctx context.Context, k8sClient client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, k8sClient, rlpKey, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func RLPIsEnforced(ctx context.Context, k8sClient client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, k8sClient, rlpKey, string(kuadrant.PolicyConditionEnforced))
}

func RLPEnforcedCondition(ctx context.Context, k8sClient client.Client, rlpKey client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) bool {
	p := &kuadrantv1beta2.RateLimitPolicy{}
	if err := k8sClient.Get(ctx, rlpKey, p); err != nil {
		return false
	}

	cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
	if cond == nil {
		return false
	}

	return cond.Reason == string(reason) && cond.Message == message
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

func RLPIsConditionTrue(ctx context.Context, k8sClient client.Client, rlpKey client.ObjectKey, condition string) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := k8sClient.Get(ctx, rlpKey, existingRLP)
		if err != nil {
			logf.Log.V(1).Error(err, "ratelimitpolicy not read", "rlp", rlpKey)
			return false
		}

		return meta.IsStatusConditionTrue(existingRLP.Status.Conditions, condition)
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

func BuildManagedZone(name, ns, domainName string) *kuadrantdnsv1alpha1.ManagedZone {
	return &kuadrantdnsv1alpha1.ManagedZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantdnsv1alpha1.ManagedZoneSpec{
			ID:          "1234",
			DomainName:  domainName,
			Description: domainName,
			SecretRef: kuadrantdnsv1alpha1.ProviderRef{
				Name: "secretname",
			},
		},
	}
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

func IsAuthPolicyAcceptedAndEnforced(ctx context.Context, k8sClient client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, k8sClient, policy)() && IsAuthPolicyEnforced(ctx, k8sClient, policy)()
	}
}

func IsAuthPolicyAcceptedAndNotEnforced(ctx context.Context, k8sClient client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, k8sClient, policy)() && !IsAuthPolicyEnforced(ctx, k8sClient, policy)()
	}
}

func IsAuthPolicyAccepted(ctx context.Context, k8sClient client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, k8sClient, policy, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func IsAuthPolicyEnforced(ctx context.Context, k8sClient client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, k8sClient, policy, string(kuadrant.PolicyConditionEnforced))
}

func IsAuthPolicyEnforcedCondition(ctx context.Context, k8sClient client.Client, key client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) func() bool {
	return func() bool {
		p := &kuadrantv1beta2.AuthPolicy{}
		if err := k8sClient.Get(ctx, key, p); err != nil {
			return false
		}

		cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
		if cond == nil {
			return false
		}

		return cond.Reason == string(reason) && cond.Message == message
	}
}

func IsAuthPolicyConditionTrue(ctx context.Context, k8sClient client.Client, policy *kuadrantv1beta2.AuthPolicy, condition string) func() bool {
	return func() bool {
		existingPolicy := &kuadrantv1beta2.AuthPolicy{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
		return err == nil && meta.IsStatusConditionTrue(existingPolicy.Status.Conditions, condition)
	}
}

// SharedConfig contains minimum cluster connection config that can be safely marshalled as rest.Config is unsafe to marshall
type SharedConfig struct {
	Host            string          `json:"host"`
	TLSClientConfig TLSClientConfig `json:"tlsClientConfig"`
	KuadrantNS      string          `json:"kuadrantNS"`
}

type TLSClientConfig struct {
	Insecure bool    `json:"insecure"`
	CertData []uint8 `json:"certData,omitempty"`
	KeyData  []uint8 `json:"keyData,omitempty"`
	CAData   []uint8 `json:"caData,omitempty"`
}

// BootstrapTestEnv bootstraps the test environment and returns the config and client
func BootstrapTestEnv() (*rest.Config, client.Client, *envtest.Environment, *runtime.Scheme) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    ptr.To(true),
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(cfg).NotTo(gomega.BeNil())

	s := BootstrapScheme()

	k8sClient, err := client.New(cfg, client.Options{Scheme: s})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(k8sClient).NotTo(gomega.BeNil())

	return cfg, k8sClient, testEnv, s
}

func BootstrapScheme() *runtime.Scheme {
	s := runtime.NewScheme()

	sb := runtime.NewSchemeBuilder(
		scheme.AddToScheme,
		kuadrantdnsv1alpha1.AddToScheme,
		kuadrantv1alpha1.AddToScheme,
		kuadrantv1beta1.AddToScheme,
		kuadrantv1beta2.AddToScheme,
		gatewayapiv1.Install,
		authorinoopapi.AddToScheme,
		authorinoapi.AddToScheme,
		istioapis.AddToScheme,
		istiov1alpha1.AddToScheme,
		istiosecurityv1beta1.AddToScheme,
		limitadorv1alpha1.AddToScheme,
		istioclientnetworkingv1alpha3.AddToScheme,
		istioclientgoextensionv1alpha1.AddToScheme,
		certmanv1.AddToScheme,
	)

	err := sb.AddToScheme(s)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return s
}

// MarshalConfig marshals the config to a shared configuration struct
func MarshalConfig(cfg *rest.Config, opts ...func(config *SharedConfig)) []byte {
	sharedCfg := &SharedConfig{
		Host: cfg.Host,
		TLSClientConfig: TLSClientConfig{
			Insecure: cfg.TLSClientConfig.Insecure,
			CertData: cfg.TLSClientConfig.CertData,
			KeyData:  cfg.TLSClientConfig.KeyData,
			CAData:   cfg.TLSClientConfig.CAData,
		},
	}

	for _, opt := range opts {
		opt(sharedCfg)
	}

	data, err := json.Marshal(sharedCfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return data
}

func WithKuadrantInstallNS(ns string) func(config *SharedConfig) {
	return func(cfg *SharedConfig) {
		cfg.KuadrantNS = ns
	}
}
