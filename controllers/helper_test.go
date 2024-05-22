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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
