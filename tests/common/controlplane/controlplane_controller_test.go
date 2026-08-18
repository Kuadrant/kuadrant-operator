//go:build integration

package controlplane

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

// Serial: KuadrantControlPlane is a cluster-scoped singleton. Destructive tests
// (deletion, drift) must not run in parallel with status or deployment tests.
var _ = Describe("KuadrantControlPlane controller", Serial, func() {
	var (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	AfterEach(func(ctx SpecContext) {
		// Ensure the default CR is restored if a test deleted it.
		cp := &kuadrantv1beta1.KuadrantControlPlane{}
		err := testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)
		if apierrors.IsNotFound(err) {
			cp = &kuadrantv1beta1.KuadrantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName},
			}
			_ = testClient().Create(ctx, cp)
		}
	}, afterEachTimeOut)

	Context("auto-creation on startup", func() {
		It("creates a default KuadrantControlPlane CR", func(ctx SpecContext) {
			cp := &kuadrantv1beta1.KuadrantControlPlane{}
			err := testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)
			Expect(err).ToNot(HaveOccurred())
			Expect(cp.Name).To(Equal(kuadrantv1beta1.KuadrantControlPlaneDefaultName))
		}, testTimeOut)
	})

	Context("singleton enforcement", func() {
		It("rejects creation of KuadrantControlPlane with non-default name", func(ctx SpecContext) {
			cp := &kuadrantv1beta1.KuadrantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "not-default"},
			}
			err := testClient().Create(ctx, cp)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err) || apierrors.IsForbidden(err)).To(BeTrue(),
				"expected invalid or forbidden error, got: %v", err)
		}, testTimeOut)
	})

	Context("component deployment", func() {
		It("deploys dns-operator Deployment in operator namespace", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				}, deploy)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("status reporting", func() {
		It("reports Ready=True when dns-operator is available", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1beta1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				cond := meta.FindStatusCondition(cp.Status.Conditions, kuadrantv1beta1.ControlPlaneConditionReady)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(kuadrantv1beta1.ControlPlaneReasonComponentsHealthy))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports component status with CRD establishment", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1beta1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				g.Expect(cp.Status.Components).ToNot(BeEmpty())
				dnsComponent := cp.Status.Components[0]
				g.Expect(dnsComponent.Name).To(Equal("dns-operator"))
				g.Expect(dnsComponent.Ready).To(BeTrue())
				g.Expect(dnsComponent.CRDs).To(HaveLen(2))
				for _, crd := range dnsComponent.CRDs {
					g.Expect(crd.Established).To(BeTrue())
				}
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports chart version for each component", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1beta1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				for _, cs := range cp.Status.Components {
					g.Expect(cs.ChartVersion).ToNot(BeEmpty(), "component %s should have a chart version", cs.Name)
				}
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports dns-operator image status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1beta1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				var dns *kuadrantv1beta1.ComponentStatus
				for i := range cp.Status.Components {
					if cp.Status.Components[i].Name == "dns-operator" {
						dns = &cp.Status.Components[i]
						break
					}
				}
				g.Expect(dns).ToNot(BeNil(), "dns-operator component not found in status")
				g.Expect(dns.Images).To(HaveLen(1))
				g.Expect(dns.Images[0].Name).To(Equal("controller"))
				g.Expect(dns.Images[0].Image).ToNot(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

	})

	Context("self-healing on deletion", func() {
		It("re-creates KuadrantControlPlane CR when deleted", func(ctx SpecContext) {
			cp := &kuadrantv1beta1.KuadrantControlPlane{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

			Expect(testClient().Delete(ctx, cp)).To(Succeed())

			Eventually(func(g Gomega) {
				recreated := &kuadrantv1beta1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, recreated)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("child Deployments survive KuadrantControlPlane deletion", func(ctx SpecContext) {
			// Ensure dns-operator is running first
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				}, deploy)).To(Succeed())
				g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">", 0))
			}).WithContext(ctx).Should(Succeed())

			// Delete the CR
			cp := &kuadrantv1beta1.KuadrantControlPlane{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1beta1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())
			Expect(testClient().Delete(ctx, cp)).To(Succeed())

			// Deployment should still exist (no cascade delete)
			Consistently(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				}, deploy)).To(Succeed())
			}, 5*time.Second, 1*time.Second).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("drift reconciliation", func() {
		It("recreates dns-operator Deployment when deleted", func(ctx SpecContext) {
			// Ensure it exists first
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				}, deploy)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// Delete the Deployment
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				},
			}
			Expect(testClient().Delete(ctx, deploy)).To(Succeed())

			// Should be recreated by the controller (watch triggers immediate reconcile)
			Eventually(func(g Gomega) {
				recreated := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: "kuadrant-system",
					Name:      "dns-operator-controller-manager",
				}, recreated)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
