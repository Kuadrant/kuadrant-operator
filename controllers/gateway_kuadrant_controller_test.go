//go:build integration

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

var _ = Describe("Kuadrant Gateway controller", func() {
	var (
		testNamespace string
		gwName        = "toystore-gw"
	)

	beforeEachCallback := func() {
		CreateNamespace(&testNamespace)

		ApplyKuadrantCR(testNamespace)
	}

	BeforeEach(beforeEachCallback)
	AfterEach(DeleteNamespaceCallback(&testNamespace))

	Context("Gateway created after Kuadrant instance", func() {
		It("gateway should have required annotation", func() {
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}

				if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, common.GatewayProgrammedConditionType) {
					logf.Log.V(1).Info("[WARN] Gateway not ready")
					return false
				}

				return true
			}, 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gateway is annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				return common.IsKuadrantManaged(existingGateway)
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("Two kuadrant instances", func() {

		BeforeEach(func() {
			newKuadrantName := "second"
			newKuadrant := &kuadrantv1beta1.Kuadrant{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1beta1", Kind: "Kuadrant"},
				ObjectMeta: metav1.ObjectMeta{Name: newKuadrantName, Namespace: testNamespace},
			}
			err := testClient().Create(context.Background(), newKuadrant)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				kuadrant := &kuadrantv1beta1.Kuadrant{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: newKuadrantName, Namespace: testNamespace}, kuadrant)
				if err != nil {
					return false
				}
				if !meta.IsStatusConditionTrue(kuadrant.Status.Conditions, "Ready") {
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("new gateway should not be annotated", func() {
			gateway := testBuildBasicGateway(gwName, testNamespace)
			err := k8sClient.Create(context.Background(), gateway)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}

				if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, common.GatewayProgrammedConditionType) {
					logf.Log.V(1).Info("[WARN] Gateway not ready")
					return false
				}

				return true
			}, 15*time.Second, 5*time.Second).Should(BeTrue())

			// Check gateway is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				return !common.IsKuadrantManaged(existingGateway)
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})
})
