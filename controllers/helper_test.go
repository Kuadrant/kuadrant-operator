//go:build integration

package controllers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
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
)

func ApplyKuadrantCR(namespace string) {
	err := ApplyResources(filepath.Join("..", "examples", "toystore", "kuadrant.yaml"), k8sClient, namespace)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() bool {
		kuadrant := &kuadrantv1beta1.Kuadrant{}
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "kuadrant-sample", Namespace: namespace}, kuadrant)
		if err != nil {
			return false
		}
		if !meta.IsStatusConditionTrue(kuadrant.Status.Conditions, "Ready") {
			return false
		}
		return true
	}, time.Minute, 5*time.Second).Should(BeTrue())
}

func CreateNamespace(namespace *string) {
	var generatedTestNamespace = "test-namespace-" + uuid.New().String()

	nsObject := &v1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: generatedTestNamespace},
	}

	err := testClient().Create(context.Background(), nsObject)
	Expect(err).ToNot(HaveOccurred())

	existingNamespace := &v1.Namespace{}
	Eventually(func() bool {
		err := testClient().Get(context.Background(), types.NamespacedName{Name: generatedTestNamespace}, existingNamespace)
		if err != nil {
			return false
		}
		return true
	}, time.Minute, 5*time.Second).Should(BeTrue())

	*namespace = existingNamespace.Name
}

func DeleteNamespaceCallback(namespace *string) func() {
	return func() {
		desiredTestNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: *namespace}}
		err := testClient().Delete(context.Background(), desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))

		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := testClient().Get(context.Background(), types.NamespacedName{Name: *namespace}, existingNamespace)
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

func testBuildBasicGateway(gwName, ns string) *gatewayapiv1.Gateway {
	return &gatewayapiv1.Gateway{
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
			Hostnames: common.Map(hostnames, func(hostname string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(hostname) }),
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
