//go:build integration

package controllers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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
)

func ApplyKuadrantCR(namespace string) {
	ApplyKuadrantCRWithName(namespace, "kuadrant-sample")
}

func ApplyKuadrantCRWithName(namespace, name string) {
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
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() bool {
		kuadrant := &kuadrantv1beta1.Kuadrant{}
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: name, Namespace: namespace}, kuadrant)
		if err != nil {
			return false
		}
		if !meta.IsStatusConditionTrue(kuadrant.Status.Conditions, "Ready") {
			return false
		}
		return true
	}, time.Minute, 5*time.Second).Should(BeTrue())
}

func DeleteKuadrantCR(ctx context.Context, namespace string) {
	k := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: "kuadrant-sample", Namespace: namespace}}
	Eventually(func(g Gomega) {
		err := k8sClient.Delete(ctx, k)
		g.Expect(err).To(HaveOccurred())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())
}

func CreateNamespaceWithContext(ctx context.Context) string {
	nsObject := &v1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-namespace-"},
	}
	Expect(testClient().Create(ctx, nsObject)).To(Succeed())

	return nsObject.Name
}

func CreateNamespace() string {
	return CreateNamespaceWithContext(context.Background())
}

func DeleteNamespaceCallbackWithContext(ctx context.Context, namespace string) {
	desiredTestNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Eventually(func(g Gomega) {
		err := k8sClient.Delete(ctx, desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		g.Expect(err).ToNot(BeNil())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())

}

func DeleteNamespaceCallback(namespace string) func() {
	return func() {
		desiredTestNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		err := testClient().Delete(context.Background(), desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))

		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := testClient().Get(context.Background(), types.NamespacedName{Name: namespace}, existingNamespace)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}, 3*time.Minute, 2*time.Second).Should(BeTrue())
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

func testBuildBasicGateway(gwName, ns string, mutateFns ...func(*gatewayapiv1.Gateway)) *gatewayapiv1.Gateway {
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

func testBuildBasicHttpRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
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

func testBuildMultipleRulesHttpRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
	route := testBuildBasicHttpRoute(routeName, gwName, ns, hostnames)
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

func testRouteIsAccepted(routeKey client.ObjectKey) func() bool {
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

func testGatewayIsReady(gateway *gatewayapiv1.Gateway) func() bool {
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

func testWasmPluginIsAvailable(key client.ObjectKey) func() bool {
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

func testRLPIsAccepted(ctx context.Context, rlpKey client.ObjectKey) func() bool {
	return testRLPIsConditionTrue(ctx, rlpKey, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func testRLPIsEnforced(ctx context.Context, rlpKey client.ObjectKey) func() bool {
	return testRLPIsConditionTrue(ctx, rlpKey, string(kuadrant.PolicyConditionEnforced))
}

func testRLPEnforcedCondition(ctx context.Context, rlpKey client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) bool {
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

func testRLPIsConditionTrue(ctx context.Context, rlpKey client.ObjectKey, condition string) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := k8sClient.Get(ctx, rlpKey, existingRLP)
		if err != nil {
			logf.Log.V(1).Error(err, "ratelimitpolicy not read", "rlp", rlpKey)
			return false
		}
		if meta.IsStatusConditionFalse(existingRLP.Status.Conditions, condition) {
			logf.Log.V(1).Info("ratelimitpolicy condition not true", "rlp", rlpKey, "condition", condition, "conditions", existingRLP.Status.Conditions)
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

func testBuildManagedZone(name, ns, domainName string) *kuadrantdnsv1alpha1.ManagedZone {
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
