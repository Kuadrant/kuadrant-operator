//go:build integration

package controllers

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

var _ = Describe("Kuadrant controller deploys wasm server", func() {
	var (
		testNamespace    string
		kuadrantName     string = "local"
		afterEachTimeOut        = NodeTimeout(3 * time.Minute)
		specTimeOut             = SpecTimeout(time.Minute * 2)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = CreateNamespaceWithContext(ctx)
	})

	AfterEach(func(ctx SpecContext) {
		DeleteNamespaceCallbackWithContext(ctx, testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("wasm server is being deployed", func(ctx SpecContext) {
			// this method checks kuadrant status is available
			// kuadrant status is checking wasm server availability as well
			ApplyKuadrantCRWithName(testNamespace, kuadrantName)
			kObj := &kuadrantv1beta1.Kuadrant{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Namespace: testNamespace, Name: kuadrantName},
				kObj)
			Expect(err).ToNot(HaveOccurred())
			// Should create a service
			service := &corev1.Service{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(
					ctx,
					client.ObjectKey{Namespace: testNamespace, Name: WasmServerServiceName(kObj)},
					service)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).Should(Equal("http"))
			Expect(service.Spec.Ports[0].Port).Should(Equal(int32(80)))
			Expect(service.Spec.Ports[0].TargetPort).Should(Equal(intstr.FromString("http")))

			//Should create a deployment
			deploymentKey := client.ObjectKey{Namespace: testNamespace, Name: WasmServerDeploymentName(kObj)}
			Eventually(testDeploymentIsReady(ctx, deploymentKey)).WithContext(ctx).Should(Succeed())

			deployment := &appsv1.Deployment{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(
				Equal(WASMServerImageURL),
			)
			Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.InitContainers[0].Image).Should(
				Equal(WASMServerImageURL),
			)
			Expect(deployment.Spec.Template.Spec.InitContainers[0].Name).Should(
				Equal(WASMServerInitContainerName),
			)

			// Should create a ConfigMap with empty limits
			configMap := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(
					ctx,
					client.ObjectKey{Namespace: testNamespace, Name: WasmServerConfigMapName(kObj)},
					configMap)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Expect(configMap.Data).To(HaveKey(WASMServerConfigMapDataKey))
		}, specTimeOut)
	})
})
