//go:build integration

package envoygateway_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

var _ = Describe("Observabiltity monitors for envoy gateway", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		err := testClient().Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			existingGateway := &gatewayapiv1.Gateway{}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
			if err != nil {
				logf.Log.V(1).Info("[WARN] Creating gateway failed", "error", err)
				return false
			}

			if meta.IsStatusConditionFalse(existingGateway.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed)) {
				logf.Log.V(1).Info("[WARN] Gateway not ready")
				return false
			}

			return true
		}).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	getKuadrantNamespace := func(ctx context.Context, cl client.Client) string {
		kuadrantList := &kuadrantv1beta1.KuadrantList{}
		err := cl.List(ctx, kuadrantList)
		// must exist
		Expect(err).ToNot(HaveOccurred())
		Expect(kuadrantList.Items).To(HaveLen(1))
		return kuadrantList.Items[0].Namespace
	}

	Context("when default kuadrant CR is created", func() {
		It("monitors are not created at first", func(ctx SpecContext) {
			envoyStatsMonitor := &monitoringv1.PodMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.PodMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "envoy-stats-monitor",
					Namespace: "gateway-system",
				},
			}

			// Verify monitors don't exists yet
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(envoyStatsMonitor), envoyStatsMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Fetch current CR & set observability flag to enable the feature
			kuadrantNS := getKuadrantNamespace(ctx, testClient())
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kuadrant-sample",
					Namespace: kuadrantNS,
				},
			}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
			Expect(err).NotTo(HaveOccurred())
			kuadrantCR.Spec.Observability.Enable = true
			err = testClient().Update(ctx, kuadrantCR)
			Expect(err).NotTo(HaveOccurred())

			// Verify all monitors are created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(envoyStatsMonitor), envoyStatsMonitor)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(envoyStatsMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
			}).WithContext(ctx).Should(Succeed())

			// Unset observability flag to disable the feature
			err = testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
			Expect(err).NotTo(HaveOccurred())
			kuadrantCR.Spec.Observability.Enable = false
			err = testClient().Update(ctx, kuadrantCR)
			Expect(err).NotTo(HaveOccurred())

			// Verify monitors were deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(envoyStatsMonitor), envoyStatsMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
