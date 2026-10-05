//go:build integration

package tokenratelimitpolicy

import (
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("TokenRateLimitPolicy controller (Serial)", Serial, Labels{"common", "tokenratelimitpolicy"}, func() {
	const (
		testTimeOut       = NodeTimeout(2 * time.Minute)
		beforeEachTimeOut = NodeTimeout(1 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		routeName     = "toystore-route"
		trlpName      = "toystore-trlp"
		gateway       *gatewayapiv1.Gateway
	)

	policyFactory := func() *kuadrantv1alpha1.TokenRateLimitPolicy {
		return &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: trlpName, Namespace: testNamespace},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(routeName),
					},
				},
				TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
					Limits: map[string]kuadrantv1alpha1.TokenLimit{
						"l1": {
							Rates: []kuadrantv1.Rate{
								{Limit: 5000, Window: kuadrantv1.Duration("1m")},
							},
						},
					},
				},
			},
		}
	}

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
		gateway = tests.BuildBasicGateway(TestGatewayName, testNamespace)
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).Should(BeTrue())

		route := tests.BuildBasicHttpRoute(routeName, TestGatewayName, testNamespace, []string{"*.toystore.com"})
		Expect(testClient().Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).WithContext(ctx).Should(BeTrue())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	It("is not enforced when Limitador has reservations disabled", func(ctx SpecContext) {
		trlp := policyFactory()
		Expect(testClient().Create(ctx, trlp)).To(Succeed())

		trlpKey := client.ObjectKeyFromObject(trlp)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeTrue())

		// Limitador is a singleton owned by the suite-wide Kuadrant CR (kuadrantInstallationNS),
		// not by the per-test namespace, so restore it on cleanup to avoid poisoning other specs.
		limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: kuadrantInstallationNS}
		DeferCleanup(func(ctx SpecContext) {
			limitadorObj := &limitadorv1alpha1.Limitador{}
			Expect(testClient().Get(ctx, limitadorKey, limitadorObj)).To(Succeed())
			limitadorObj.Spec.Reservations = nil
			Expect(testClient().Update(ctx, limitadorObj)).To(Succeed())
		})

		limitadorObj := &limitadorv1alpha1.Limitador{}
		Expect(testClient().Get(ctx, limitadorKey, limitadorObj)).To(Succeed())
		limitadorObj.Spec.Reservations = &limitadorv1alpha1.Reservations{Enabled: ptr.To(false)}
		Expect(testClient().Update(ctx, limitadorObj)).To(Succeed())

		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), trlpKey)).WithContext(ctx).Should(BeFalse())
		Eventually(func() bool {
			return tests.TokenRateLimitPolicyEnforcedCondition(ctx, testClient(), trlpKey, kuadrant.PolicyReasonReservationsDisabled,
				"TokenRateLimitPolicy cannot be enforced: Kuadrant spec.tokenRateLimiting.mode is Reservation, but Limitador has reservations disabled (spec.reservations.enabled=false)")
		}).WithContext(ctx).Should(BeTrue())
	}, testTimeOut)
})
