//go:build integration

package controllers

import (
	"fmt"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

var _ = Describe("Limitador Cluster EnvoyFilter controller", func() {
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)
	var (
		testNamespace string
		gwName        = "toystore-gw"
		rlpName       = "toystore-rlp"
		efName        = fmt.Sprintf("kuadrant-ratelimiting-cluster-%s", gwName)
	)

	beforeEachCallback := func(ctx SpecContext) {
		CreateNamespaceWithContext(ctx, &testNamespace)
		gateway := testBuildBasicGateway(gwName, testNamespace)
		err := k8sClient.Create(ctx, gateway)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			existingGateway := &gatewayapiv1.Gateway{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
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

		ApplyKuadrantCR(testNamespace)

		// Check Limitador Status is Ready
		Eventually(func() bool {
			limitador := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, limitador)
			if err != nil {
				return false
			}
			if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
				return false
			}
			return true
		}).WithContext(ctx).Should(BeTrue())
	}

	BeforeEach(beforeEachCallback)
	AfterEach(func(ctx SpecContext) {
		DeleteNamespaceCallbackWithContext(ctx, &testNamespace)
	}, afterEachTimeOut)

	Context("RLP targeting Gateway", func() {
		It("EnvoyFilter created when RLP exists and deleted with RLP is deleted", func(ctx SpecContext) {
			// create ratelimitpolicy
			rlp := &kuadrantv1beta2.RateLimitPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RateLimitPolicy",
					APIVersion: kuadrantv1beta2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      rlpName,
					Namespace: testNamespace,
				},
				Spec: kuadrantv1beta2.RateLimitPolicySpec{
					TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "Gateway",
						Name:  gatewayapiv1.ObjectName(gwName),
					},
					RateLimitPolicyCommonSpec: kuadrantv1beta2.RateLimitPolicyCommonSpec{
						Limits: map[string]kuadrantv1beta2.Limit{
							"l1": {
								Rates: []kuadrantv1beta2.Rate{
									{
										Limit: 1, Duration: 3, Unit: kuadrantv1beta2.TimeUnit("minute"),
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())
			// Check RLP status is available
			rlpKey := client.ObjectKey{Name: rlpName, Namespace: testNamespace}
			Eventually(testRLPIsAccepted(rlpKey)).WithContext(ctx).Should(BeTrue())
			Eventually(testRLPIsEnforced(rlpKey)).WithContext(ctx).Should(BeFalse())
			Expect(testRLPEnforcedCondition(rlpKey, kuadrant.PolicyReasonUnknown, "RateLimitPolicy has encountered some issues: no free routes to enforce policy"))

			// Check envoy filter
			Eventually(func() bool {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: efName, Namespace: testNamespace}
				err = k8sClient.Get(ctx, efKey, existingEF)
				if err != nil {
					return false
				}
				return true
			}).WithContext(ctx).Should(BeTrue())

			err = k8sClient.Delete(ctx, rlp)
			Expect(err).ToNot(HaveOccurred())

			// Check envoy filter is gone
			Eventually(func() bool {
				existingEF := &istioclientnetworkingv1alpha3.EnvoyFilter{}
				efKey := client.ObjectKey{Name: efName, Namespace: testNamespace}
				err = k8sClient.Get(ctx, efKey, existingEF)
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
