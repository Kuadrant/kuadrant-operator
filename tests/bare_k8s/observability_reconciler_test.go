//go:build integration

package bare_k8s_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var _ = Describe("Observabiltity monitors", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(15 * time.Second)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("Monitors are not created", func(ctx SpecContext) {
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
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)).To(Succeed())

				kuadrantMonitor := &monitoringv1.ServiceMonitor{
					TypeMeta: metav1.TypeMeta{
						Kind:       monitoringv1.ServiceMonitorsKind,
						APIVersion: monitoringv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kuadrant-operator-monitor",
						Namespace: testNamespace,
					},
				}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantMonitor), kuadrantMonitor)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when kuadrant CR with observability enabled is created", func() {
		It("Monitors are created", func(ctx SpecContext) {
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local",
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta1.KuadrantSpec{
					Observability: kuadrantv1beta1.Observability{
						Enable: true,
					},
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				kuadrantMonitor := &monitoringv1.ServiceMonitor{
					TypeMeta: metav1.TypeMeta{
						Kind:       monitoringv1.ServiceMonitorsKind,
						APIVersion: monitoringv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kuadrant-operator-monitor",
						Namespace: testNamespace,
					},
				}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantMonitor), kuadrantMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(kuadrantMonitor.Labels).To(HaveKeyWithValue("kuadrant-observability", "true"))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				authorinoMonitor := &monitoringv1.ServiceMonitor{
					TypeMeta: metav1.TypeMeta{
						Kind:       monitoringv1.ServiceMonitorsKind,
						APIVersion: monitoringv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "authorino-operator-monitor",
						Namespace: testNamespace,
					},
				}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(authorinoMonitor), authorinoMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(authorinoMonitor.Labels).To(HaveKeyWithValue("kuadrant-observability", "true"))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				limitadorMonitor := &monitoringv1.ServiceMonitor{
					TypeMeta: metav1.TypeMeta{
						Kind:       monitoringv1.ServiceMonitorsKind,
						APIVersion: monitoringv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "limitador-operator-monitor",
						Namespace: testNamespace,
					},
				}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(limitadorMonitor), limitadorMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(limitadorMonitor.Labels).To(HaveKeyWithValue("kuadrant-observability", "true"))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				dnsMonitor := &monitoringv1.ServiceMonitor{
					TypeMeta: metav1.TypeMeta{
						Kind:       monitoringv1.ServiceMonitorsKind,
						APIVersion: monitoringv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dns-operator-monitor",
						Namespace: testNamespace,
					},
				}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(dnsMonitor), dnsMonitor)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dnsMonitor.Labels).To(HaveKeyWithValue("kuadrant-observability", "true"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
