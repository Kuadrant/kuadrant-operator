//go:build integration

package controllers

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
)

var _ = Describe("Kuadrant Gateway controller", func() {
	var (
		testNamespace string
		gwAName       = "gw-a"
		gwBName       = "gw-b"
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)

	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("two gateways created after Kuadrant instance", func() {
		It("gateways should have required annotation", func() {
			ApplyKuadrantCR(testNamespace)

			gwA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwA), 15*time.Second, 5*time.Second).Should(BeTrue())

			gwB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwB), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwA, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwB, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("two gateways created before Kuadrant instance", func() {
		It("gateways should have required annotation", func() {
			gwA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwA), 15*time.Second, 5*time.Second).Should(BeTrue())

			gwB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwB), 15*time.Second, 5*time.Second).Should(BeTrue())

			ApplyKuadrantCR(testNamespace)

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwA, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwB, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("when Kuadrant instance is deleted", func() {
		It("gateways should not have kuadrant annotation", func() {
			kuadrantName := "sample"
			ApplyKuadrantCRWithName(testNamespace, kuadrantName)

			gwA := testBuildBasicGateway(gwAName, testNamespace)
			err := k8sClient.Create(context.Background(), gwA)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwA), 15*time.Second, 5*time.Second).Should(BeTrue())

			gwB := testBuildBasicGateway(gwBName, testNamespace)
			err = k8sClient.Create(context.Background(), gwB)
			Expect(err).ToNot(HaveOccurred())
			Eventually(testGatewayIsReady(gwB), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwA, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(gwB, testNamespace), 15*time.Second, 5*time.Second).Should(BeTrue())

			kObj := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace}}
			err = testClient().Delete(context.Background(), kObj)

			// Check gwA is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gwA), existingGateway)
				if err != nil {
					logf.Log.Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}, 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gwB is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gwB), existingGateway)
				if err != nil {
					logf.Log.Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("Two kuadrant instances", func() {
		var (
			secondNamespace string
			kuadrantAName   string = "kuadrant-a"
			kuadrantBName   string = "kuadrant-b"
		)

		BeforeEach(func() {
			ApplyKuadrantCRWithName(testNamespace, kuadrantAName)

			CreateNamespace(&secondNamespace)
			ApplyKuadrantCRWithName(secondNamespace, kuadrantBName)
		})

		AfterEach(DeleteNamespaceCallback(&secondNamespace))

		It("new gateway should not be annotated", func() {
			gateway := testBuildBasicGateway("gw-a", testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())

			Eventually(testGatewayIsReady(gateway), 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gateway is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				_, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
				return !isSet
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})
})

func testIsGatewayKuadrantManaged(gw *gatewayapiv1.Gateway, ns string) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gw), existingGateway)
		if err != nil {
			logf.Log.Info("[WARN] Getting gateway failed", "error", err)
			return false
		}
		val, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
		return isSet && val == ns
	}
}
