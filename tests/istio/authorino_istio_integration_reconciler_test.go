//go:build integration

package istio_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// The tests need to be run in serial as kuadrant CR namespace is shared
var _ = Describe("Authorino Istio integration reconciler", Serial, func() {
	const (
		testTimeOut      = SpecTimeout(3 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
		kapName          = "toystore-kap"
	)

	var (
		testNamespace string
	)

	beforeEachCallback := func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway := tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).ToNot(HaveOccurred())

		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route := tests.BuildBasicHttpRoute(TestHTTPRouteName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
		Expect(k8sClient.Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())

		// create authpolicy
		policy := &kuadrantv1.AuthPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AuthPolicy",
				APIVersion: kuadrantv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "toystore",
				Namespace: testNamespace,
			},
			Spec: kuadrantv1.AuthPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  TestHTTPRouteName,
					},
				},
				Defaults: &kuadrantv1.MergeableAuthPolicySpec{
					AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
						AuthScheme: tests.BuildBasicAuthScheme(),
					},
				},
			},
		}
		Expect(testClient().Create(ctx, policy)).ToNot(HaveOccurred())
		// check policy status
		Eventually(tests.IsAuthPolicyAcceptedAndEnforced(ctx, testClient(), policy)).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when mTLS is on", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())

			Eventually(tests.AuthorinoIsReady(testClient(), client.ObjectKey{
				Name:      "authorino",
				Namespace: kuadrantInstallationNS,
			})).WithContext(ctx).Should(Succeed())
		})

		It("deployment pod template labels are correct", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				deployment := &appsv1.Deployment{}
				deploymentKey := client.ObjectKey{Name: "authorino", Namespace: kuadrantInstallationNS}
				g.Expect(testClient().Get(ctx, deploymentKey, deployment)).NotTo(HaveOccurred())
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("sidecar.istio.io/inject", "true"))
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kuadrant.io/managed", "true"))
			}).WithContext(ctx).Should(Succeed())

			// Delete the policy
			policyKey := client.ObjectKey{Name: "toystore", Namespace: testNamespace}
			policy := &kuadrantv1.AuthPolicy{}
			Expect(testClient().Get(ctx, policyKey, policy)).NotTo(HaveOccurred())
			Expect(testClient().Delete(ctx, policy)).ToNot(HaveOccurred())

			Eventually(func(g Gomega, ctx context.Context) {
				deployment := &appsv1.Deployment{}
				deploymentKey := client.ObjectKey{Name: "authorino", Namespace: kuadrantInstallationNS}
				g.Expect(testClient().Get(ctx, deploymentKey, deployment)).NotTo(HaveOccurred())
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("sidecar.istio.io/inject", "false"))
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kuadrant.io/managed", "true"))
			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("when mTLS is off", func() {
		BeforeEach(func(ctx SpecContext) {
			kuadrantObj := &kuadrantv1beta1.Kuadrant{}
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: kuadrantInstallationNS}
			Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
			kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: false}
			Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())

			Eventually(tests.AuthorinoIsReady(testClient(), client.ObjectKey{
				Name:      "authorino",
				Namespace: kuadrantInstallationNS,
			})).WithContext(ctx).Should(Succeed())
		})
		It("deployment pod template labels are correct", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				deployment := &appsv1.Deployment{}
				deploymentKey := client.ObjectKey{Name: "authorino", Namespace: kuadrantInstallationNS}
				g.Expect(testClient().Get(ctx, deploymentKey, deployment)).NotTo(HaveOccurred())
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("sidecar.istio.io/inject", "false"))
				g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kuadrant.io/managed", "true"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
