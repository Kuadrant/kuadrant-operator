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
	"k8s.io/apimachinery/pkg/types"
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

	Context("when a Kuadrant CR exists", func() {
		It("creates the developer portal deployment by default", func(ctx SpecContext) {
			deployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "kuadrant-system",
				},
			}

			kuadrantCR := getKuadrantCR(ctx, testClient())

			// Verify Deployment is created by default (no opt-in required)
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
			}).WithTimeout(2 * time.Minute).WithContext(ctx).Should(Succeed())

			// Verify finalizer is present
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(kuadrantCR.GetFinalizers()).To(ContainElement("kuadrant.io/developerportal"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when the deprecated developerPortal.enabled field is set to false", func() {
		var savedSpec *kuadrantv1beta1.KuadrantSpec

		AfterEach(func(ctx SpecContext) {
			// Restore the original spec so this test does not affect the others.
			// Guard on savedSpec so an early failure (before we captured the spec)
			// does not write a zero-value spec back to the shared Kuadrant CR.
			if savedSpec == nil {
				return
			}
			Eventually(func(g Gomega) {
				kuadrantCR := getKuadrantCR(ctx, testClient())
				kuadrantCR.Spec = *savedSpec
				g.Expect(testClient().Update(ctx, kuadrantCR)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())
		}, afterEachTimeOut)

		It("recreates the developer portal deployment even when enabled=false (deprecated no-op)", func(ctx SpecContext) {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "kuadrant-system",
				},
			}

			// Capture the original spec up-front, before any wait, so AfterEach can
			// always restore it (see the nil guard above).
			kuadrantCR := getKuadrantCR(ctx, testClient())
			savedSpec = kuadrantCR.Spec.DeepCopy()

			// The Deployment must already exist (enabled by default). Keep this wait
			// well under the SpecTimeout so the recreation assertion below still fits.
			var originalUID types.UID
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				originalUID = deployment.GetUID()
			}).WithTimeout(time.Minute).WithContext(ctx).Should(Succeed())

			// Set the deprecated enabled=false field through the real API server.
			Eventually(func(g Gomega) {
				kuadrantCR := getKuadrantCR(ctx, testClient())
				kuadrantCR.Spec.Components = &kuadrantv1beta1.Components{
					DeveloperPortal: &kuadrantv1beta1.DeveloperPortal{Enabled: false},
				}
				g.Expect(testClient().Update(ctx, kuadrantCR)).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Delete the Deployment to actively exercise reconciliation.
			Expect(testClient().Delete(ctx, deployment)).To(Succeed())

			// The reconciler must recreate the Deployment: enabled=false is ignored.
			// A different UID proves it is a fresh object, not the one we deleted.
			Eventually(func(g Gomega) {
				recreated := &appsv1.Deployment{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), recreated)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(recreated.GetUID()).NotTo(Equal(originalUID))
				g.Expect(recreated.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("when Kuadrant CR is deleted", func() {
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

			// Verify Deployment is created by default
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(deployment.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
			}).WithTimeout(2 * time.Minute).WithContext(ctx).Should(Succeed())

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
