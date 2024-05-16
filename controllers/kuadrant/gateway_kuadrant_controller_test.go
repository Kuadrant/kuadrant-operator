//go:build integration

package kuadrant

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/test"
)

var _ = Describe("Kuadrant Gateway controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwAName       = "gw-a"
		gwBName       = "gw-b"
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = test.CreateNamespaceWithContext(ctx, k8sClient)
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) { test.DeleteNamespaceCallbackWithContext(ctx, k8sClient, testNamespace) }, afterEachTimeOut)

	Context("two gateways created after Kuadrant instance", func() {
		It("gateways should have required annotation", func(ctx SpecContext) {
			test.ApplyKuadrantCR(k8sClient, testNamespace)

			gwA := test.BuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwA)).WithContext(ctx).Should(BeTrue())

			gwB := test.BuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwB)).WithContext(ctx).Should(BeTrue())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwB, testNamespace)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("two gateways created before Kuadrant instance", func() {
		It("gateways should have required annotation", func(ctx SpecContext) {
			gwA := test.BuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwA)).WithContext(ctx).Should(BeTrue())

			gwB := test.BuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwB)).WithContext(ctx).Should(BeTrue())

			test.ApplyKuadrantCR(k8sClient, testNamespace)

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwB, testNamespace)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("when Kuadrant instance is deleted", func() {
		It("gateways should not have kuadrant annotation", func(ctx SpecContext) {
			kuadrantName := "sample"
			test.ApplyKuadrantCRWithName(k8sClient, testNamespace, kuadrantName)

			gwA := test.BuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwA)).WithContext(ctx).Should(BeTrue())

			gwB := test.BuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(test.GatewayIsReady(k8sClient, gwB)).WithContext(ctx).Should(BeTrue())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, gwB, testNamespace)).WithContext(ctx).Should(BeTrue())

			kObj := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace}}
			err = k8sClient.Delete(ctx, kObj)

			// Check gwA is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gwA), existingGateway)
				if err != nil {
					logf.Log.Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}).WithContext(ctx).Should(BeTrue())

			// Check gwB is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gwB), existingGateway)
				if err != nil {
					logf.Log.Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("Two kuadrant instances", func() {
		var (
			secondNamespace string
			kuadrantAName   = "kuadrant-a"
			kuadrantBName   = "kuadrant-b"
		)

		BeforeEach(func(ctx SpecContext) {
			test.ApplyKuadrantCRWithName(k8sClient, testNamespace, kuadrantAName)

			secondNamespace = test.CreateNamespaceWithContext(ctx, k8sClient)
			test.ApplyKuadrantCRWithName(k8sClient, secondNamespace, kuadrantBName)
		})

		AfterEach(func(ctx SpecContext) { test.DeleteNamespaceCallbackWithContext(ctx, k8sClient, secondNamespace) }, afterEachTimeOut)

		It("new gateway should not be annotated", func(ctx SpecContext) {
			gateway := test.BuildBasicGateway("gw-a", testNamespace)
			err := k8sClient.Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())

			Eventually(test.GatewayIsReady(k8sClient, gateway)).WithContext(ctx).Should(BeTrue())

			// Check gateway is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})

func testIsGatewayKuadrantManaged(ctx context.Context, gw *gatewayapiv1.Gateway, ns string) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gw), existingGateway)
		if err != nil {
			logf.Log.Info("[WARN] Getting gateway failed", "error", err)
			return false
		}
		val, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
		return isSet && val == ns
	}
}
