//go:build integration

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

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
				existingGateway := &gatewayapiv1beta1.Gateway{}
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
				existingGateway := &gatewayapiv1beta1.Gateway{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gateway), existingGateway)
				if err != nil {
					logf.Log.V(1).Info("[WARN] Getting gateway failed", "error", err)
					return false
				}
				return common.IsKuadrantManaged(existingGateway)
			}, 15*time.Second, 5*time.Second).Should(BeTrue())
		})
	})
})
