//go:build integration

package bare_k8s_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Observabiltity monitors for kuadrant components", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(30 * time.Second)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	const kuadrantNamespace = "kuadrant-system"

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("monitors are not created at first", func(ctx SpecContext) {
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
			}
			kuadrantMonitor := &monitoringv1.ServiceMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.ServiceMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kuadrant-operator-monitor",
					Namespace: kuadrantNamespace,
				},
			}
			authorinoOpMonitor := &monitoringv1.ServiceMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.ServiceMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "authorino-operator-monitor",
					Namespace: kuadrantNamespace,
				},
			}
			limitadorOpMonitor := &monitoringv1.ServiceMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.ServiceMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "limitador-operator-monitor",
					Namespace: kuadrantNamespace,
				},
			}
			dnsMonitor := &monitoringv1.ServiceMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.ServiceMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dns-operator-monitor",
					Namespace: kuadrantNamespace,
				},
			}
			limitadorMonitor := &monitoringv1.PodMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.PodMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kuadrant-limitador-monitor",
					Namespace: testNamespace,
				},
			}
			authorinoMonitor := &monitoringv1.ServiceMonitor{
				TypeMeta: metav1.TypeMeta{
					Kind:       monitoringv1.ServiceMonitorsKind,
					APIVersion: monitoringv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kuadrant-authorino-monitor",
					Namespace: testNamespace,
				},
			}

			// Create Kuadrant CR with observability not enabled
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			// Verify monitors not created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantMonitor), kuadrantMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoOpMonitor), authorinoOpMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorOpMonitor), limitadorOpMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(dnsMonitor), dnsMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorMonitor), limitadorMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoMonitor), authorinoMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Set observability flag to enable the feature
			kuadrantCR.Spec = kuadrantv1beta1.KuadrantSpec{
				Observability: kuadrantv1beta1.Observability{
					Enable: true,
				},
			}
			Expect(testClient().Patch(ctx, kuadrantCR, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			// Verify monitors created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantMonitor), kuadrantMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(kuadrantMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoOpMonitor), authorinoOpMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(authorinoOpMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorOpMonitor), limitadorOpMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(limitadorOpMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(dnsMonitor), dnsMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dnsMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorMonitor), limitadorMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(limitadorMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
				g.Expect(limitadorMonitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", "limitador"))
				g.Expect(limitadorMonitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("limitador-resource", "limitador"))
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoMonitor), authorinoMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(authorinoMonitor.Labels).To(HaveKeyWithValue("kuadrant.io/observability", "true"))
				g.Expect(authorinoMonitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "authorino"))
				g.Expect(authorinoMonitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/component", "metrics"))
			}).WithContext(ctx).Should(Succeed())

			// Disable observability feature
			kuadrantCR.Spec.Observability.Enable = false
			Expect(testClient().Patch(ctx, kuadrantCR, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			// Verify monitors deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantMonitor), kuadrantMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoOpMonitor), authorinoOpMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorOpMonitor), limitadorOpMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(dnsMonitor), dnsMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorMonitor), limitadorMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoMonitor), authorinoMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
