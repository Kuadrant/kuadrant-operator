//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Developer Portal Controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	getKuadrantCR := func(ctx context.Context, cl client.Client) *kuadrantv1beta1.Kuadrant {
		kuadrantList := &kuadrantv1beta1.KuadrantList{}
		err := cl.List(ctx, kuadrantList)
		// must exist
		Expect(err).ToNot(HaveOccurred())
		Expect(kuadrantList.Items).To(HaveLen(1))
		return &kuadrantList.Items[0]
	}

	Context("when developer portal is enabled and then disabled", func() {
		It("creates and deletes all required resources", func(ctx SpecContext) {
			// Resources to check
			clusterRole := &rbacv1.ClusterRole{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterRole",
					APIVersion: rbacv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "kuadrant-operator-developer-portal-controller-manager-role",
				},
			}

			serviceAccount := &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ServiceAccount",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "", // Will be set to kuadrant namespace
				},
			}

			clusterRoleBinding := &rbacv1.ClusterRoleBinding{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterRoleBinding",
					APIVersion: rbacv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "developer-portal-controller-rolebinding",
				},
			}

			deployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "developer-portal-controller",
					Namespace: "", // Will be set to kuadrant namespace
				},
			}

			// Verify resources don't exist yet
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Get and enable developer portal
			kuadrantCR := getKuadrantCR(ctx, testClient())
			serviceAccount.Namespace = kuadrantCR.Namespace
			deployment.Namespace = kuadrantCR.Namespace

			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				if kuadrantCR.Spec.Components.DeveloperPortal.Enabled {
					return // Already enabled
				}
				kuadrantCR.Spec.Components.DeveloperPortal.Enabled = true
				err = testClient().Update(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Verify ClusterRole is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(clusterRole.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
				g.Expect(clusterRole.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
				// Verify rules include devportal.kuadrant.io
				foundDevPortalRule := false
				for _, rule := range clusterRole.Rules {
					if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "devportal.kuadrant.io" {
						foundDevPortalRule = true
						g.Expect(rule.Resources).To(ContainElements("apiproducts", "apikeyrequests"))
						g.Expect(rule.Verbs).To(ContainElement("get"))
						g.Expect(rule.Verbs).To(ContainElement("list"))
						g.Expect(rule.Verbs).To(ContainElement("watch"))
						break
					}
				}
				g.Expect(foundDevPortalRule).To(BeTrue(), "ClusterRole should have devportal.kuadrant.io rules")
			}).WithContext(ctx).Should(Succeed())

			// Verify ServiceAccount is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(serviceAccount), serviceAccount)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(serviceAccount.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
				g.Expect(serviceAccount.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
				// Verify owner reference
				g.Expect(serviceAccount.OwnerReferences).To(HaveLen(1))
				g.Expect(serviceAccount.OwnerReferences[0].Kind).To(Equal("Kuadrant"))
				g.Expect(*serviceAccount.OwnerReferences[0].Controller).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Verify ClusterRoleBinding is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(clusterRoleBinding), clusterRoleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(clusterRoleBinding.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
				g.Expect(clusterRoleBinding.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
				// Verify RoleRef
				g.Expect(clusterRoleBinding.RoleRef.Name).To(Equal("kuadrant-operator-developer-portal-controller-manager-role"))
				g.Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
				// Verify Subjects
				g.Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
				g.Expect(clusterRoleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
				g.Expect(clusterRoleBinding.Subjects[0].Name).To(Equal("developer-portal-controller"))
				g.Expect(clusterRoleBinding.Subjects[0].Namespace).To(Equal(kuadrantCR.Namespace))
			}).WithContext(ctx).Should(Succeed())

			// Verify Deployment is created
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(deployment.Labels).To(HaveKeyWithValue("app", "developer-portal-controller"))
				g.Expect(deployment.Labels).To(HaveKeyWithValue(kuadrant.DeveloperPortalLabel, "true"))
				// Verify owner reference
				g.Expect(deployment.OwnerReferences).To(HaveLen(1))
				g.Expect(deployment.OwnerReferences[0].Kind).To(Equal("Kuadrant"))
				// Verify deployment spec
				g.Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
				g.Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("developer-portal-controller"))
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

			// Now disable developer portal
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
				kuadrantCR.Spec.Components.DeveloperPortal.Enabled = false
				err = testClient().Update(ctx, kuadrantCR)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())

			// Verify ClusterRole is deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Verify ServiceAccount is deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(serviceAccount), serviceAccount)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Verify ClusterRoleBinding is deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(clusterRoleBinding), clusterRoleBinding)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			// Verify Deployment is deleted
			Eventually(func(g Gomega) {
				err := testClient().Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})
})
