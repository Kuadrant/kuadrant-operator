//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

var _ = Describe("Developer Portal Controller", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	getKuadrantCR := func(ctx context.Context, cl client.Client) *kuadrantv1beta1.Kuadrant {
		kuadrantList := &kuadrantv1beta1.KuadrantList{}
		err := cl.List(ctx, kuadrantList)
		// must exist
		Expect(err).ToNot(HaveOccurred())
		Expect(kuadrantList.Items).To(HaveLen(1))
		return &kuadrantList.Items[0]
	}

	Context("when developer portal is enabled and then disabled", func() {
		It("creates and deletes deployment", func(ctx SpecContext) {
			deployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "",
				},
			}

			kuadrantCR := getKuadrantCR(ctx, testClient())
			deployment.Namespace = "kuadrant-system"

			// Enable developer portal
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				if kuadrantCR.Spec.Components == nil {
					kuadrantCR.Spec.Components = &kuadrantv1beta1.Components{}
				}
				if kuadrantCR.Spec.Components.DeveloperPortal == nil {
					kuadrantCR.Spec.Components.DeveloperPortal = &kuadrantv1beta1.DeveloperPortal{}
				}
				kuadrantCR.Spec.Components.DeveloperPortal.Enabled = true
				err = testClient().Update(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(5 * time.Minute).WithContext(ctx).Should(Succeed())

			// Verify Deployment is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(deployment.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
				g.Expect(deployment.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
				// Verify deployment spec
				g.Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
				g.Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("developer-portal-controller-manager"))
				// Verify container
				g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
				container := deployment.Spec.Template.Spec.Containers[0]
				g.Expect(container.Name).To(Equal("manager"))
				g.Expect(container.Image).To(ContainSubstring("developer-portal-controller"))
				g.Expect(container.Command).To(ContainElement("/manager"))
				g.Expect(container.Args).To(ContainElement("--leader-elect"))
				// Verify probes exist
				g.Expect(container.LivenessProbe).NotTo(BeNil())
				g.Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
				g.Expect(container.ReadinessProbe).NotTo(BeNil())
				g.Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
			}).WithContext(ctx).Should(Succeed())

			// Verify finalizer is present
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kuadrantCR.GetFinalizers()).To(ContainElement("kuadrant.io/developerportal"))
			}).WithContext(ctx).Should(Succeed())

			// Now disable developer portal
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kuadrantCR.Spec.Components).NotTo(BeNil())
				g.Expect(kuadrantCR.Spec.Components.DeveloperPortal).NotTo(BeNil())
				kuadrantCR.Spec.Components.DeveloperPortal.Enabled = false
				err = testClient().Update(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Verify Deployment is deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("when Kuadrant CR is deleted without disabling developer portal first", func() {
		var savedKuadrantCR *kuadrantv1beta1.Kuadrant

		BeforeEach(func(ctx SpecContext) {
			// Save the current Kuadrant CR state before the test
			savedKuadrantCR = getKuadrantCR(ctx, testClient()).DeepCopy()
		})

		AfterEach(func(ctx SpecContext) {
			// Recreate the Kuadrant CR after the test deletes it
			if savedKuadrantCR != nil {
				kuadrantCR := &kuadrantv1beta1.Kuadrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      savedKuadrantCR.Name,
						Namespace: savedKuadrantCR.Namespace,
					},
					Spec: savedKuadrantCR.Spec,
				}

				err := testClient().Create(ctx, kuadrantCR)
				Expect(err).NotTo(HaveOccurred())

				// Wait for the CR to be ready
				Eventually(func(g Gomega) {
					recreatedCR := &kuadrantv1beta1.Kuadrant{}
					err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), recreatedCR)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(meta.IsStatusConditionTrue(recreatedCR.Status.Conditions, "Ready")).To(BeTrue())
				}).WithContext(ctx).WithTimeout(3 * time.Minute).Should(Succeed())
			}
		}, afterEachTimeOut)

		It("uses finalizer to ensure cleanup happens before deletion", func(ctx SpecContext) {
			const (
				developerPortalFinalizer = "kuadrant.io/developerportal"
			)

			deployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "",
				},
			}

			kuadrantCR := getKuadrantCR(ctx, testClient())
			deployment.Namespace = "kuadrant-system"

			// Enable developer portal
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				if kuadrantCR.Spec.Components == nil {
					kuadrantCR.Spec.Components = &kuadrantv1beta1.Components{}
				}
				if kuadrantCR.Spec.Components.DeveloperPortal == nil {
					kuadrantCR.Spec.Components.DeveloperPortal = &kuadrantv1beta1.DeveloperPortal{}
				}
				kuadrantCR.Spec.Components.DeveloperPortal.Enabled = true
				err = testClient().Update(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(5 * time.Minute).WithContext(ctx).Should(Succeed())

			// Verify Deployment is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(deployment.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				err = testClient().Delete(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kuadrantCR.GetDeletionTimestamp()).NotTo(BeNil(), "CR should have deletion timestamp")
				g.Expect(kuadrantCR.GetFinalizers()).To(ContainElement(developerPortalFinalizer), "finalizer should be present during cleanup")
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})
})
