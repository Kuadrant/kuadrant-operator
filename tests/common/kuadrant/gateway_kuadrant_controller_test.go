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
	"github.com/kuadrant/kuadrant-operator/tests"
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
		kuadrantName  = "sample"
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("two gateways created after Kuadrant instance", func() {
		It("gateways should have required annotation", func(ctx SpecContext) {
			// it is not required that the kuadrant cr object is reconciled successfully.
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			gwA := tests.BuildBasicGateway(gwAName, testNamespace)
			err := testClient().Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())

			gwB := tests.BuildBasicGateway(gwBName, testNamespace)
			err = testClient().Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwB, testNamespace)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("two gateways created before Kuadrant instance", func() {
		It("gateways should have required annotation", func(ctx SpecContext) {
			gwA := tests.BuildBasicGateway(gwAName, testNamespace)
			err := testClient().Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())

			gwB := tests.BuildBasicGateway(gwBName, testNamespace)
			err = testClient().Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())

			// it is not required that the kuadrant cr object is reconciled successfully.
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwB, testNamespace)).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})

	Context("when Kuadrant instance is deleted", func() {
		It("gateways should not have kuadrant annotation", func(ctx SpecContext) {
			// it is not required that the kuadrant cr object is reconciled successfully.
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			gwA := tests.BuildBasicGateway(gwAName, testNamespace)
			err := testClient().Create(ctx, gwA)
			Expect(err).ToNot(HaveOccurred())

			gwB := tests.BuildBasicGateway(gwBName, testNamespace)
			err = testClient().Create(ctx, gwB)
			Expect(err).ToNot(HaveOccurred())

			// Check gwA is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwA, testNamespace)).WithContext(ctx).Should(BeTrue())

			// Check gwB is annotated with kuadrant annotation
			Eventually(testIsGatewayKuadrantManaged(ctx, testClient(), gwB, testNamespace)).WithContext(ctx).Should(BeTrue())

			kObj := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: kuadrantName, Namespace: testNamespace}}
			err = testClient().Delete(ctx, kObj)

			// Check gwA is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(gwA), existingGateway)
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
				err := testClient().Get(ctx, client.ObjectKeyFromObject(gwB), existingGateway)
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
			firstKuadrant := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantAName, Namespace: testNamespace},
			}
			Expect(testClient().Create(ctx, firstKuadrant)).ToNot(HaveOccurred())

			secondNamespace = tests.CreateNamespace(ctx, testClient())
			secondKuadrant := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantBName, Namespace: secondNamespace},
			}
			Expect(testClient().Create(ctx, secondKuadrant)).ToNot(HaveOccurred())
		})

		AfterEach(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), secondNamespace)
		}, afterEachTimeOut)

		It("new gateway should not be annotated", func(ctx SpecContext) {
			gateway := tests.BuildBasicGateway("gw-a", testNamespace)
			err := testClient().Create(ctx, gateway)
			Expect(err).ToNot(HaveOccurred())

			// Check gateway is not annotated with kuadrant annotation
			Eventually(func() bool {
				existingGateway := &gatewayapiv1.Gateway{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
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

func testIsGatewayKuadrantManaged(ctx context.Context, cl client.Client, gw *gatewayapiv1.Gateway, ns string) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(gw), existingGateway)
		if err != nil {
			logf.Log.Info("[WARN] Getting gateway failed", "error", err)
			return false
		}
		val, isSet := existingGateway.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
		return isSet && val == ns
	}
}
