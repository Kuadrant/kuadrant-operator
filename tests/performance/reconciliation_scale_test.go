//go:build performance

package performance

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

// Reconciliation runs in the test process (via SetupKuadrantOperatorForTest),
// so Go's built-in profiling captures the operator's hot paths directly:
//
//	go test -tags=performance -v -timeout=60m \
//	  -cpuprofile=perf-cpu.prof -memprofile=perf-mem.prof \
//	  ./tests/performance/ -ginkgo.v
//
//	go tool pprof -http=:8080 perf-cpu.prof
//
// TODO: Capture pprof profiles from all cluster components (Authorino,
// authorino-operator, limitador-operator, dns-operator) via their
// --pprof-bind-address endpoints during the test. This would give
// visibility into downstream bottlenecks (e.g., Authorino throughput
// at high scale) that in-process profiling cannot capture.

var _ = Describe("Reconciliation scale", func() {
	var testNamespace string
	var gwName string

	BeforeEach(func(ctx context.Context) {
		GinkgoWriter.Printf("creating test namespace\n")
		setupStart := time.Now()
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gwName = "perf-gateway"
		gw := tests.BuildBasicGateway(gwName, testNamespace, func(gw *gatewayapiv1.Gateway) {
			gw.Spec.Listeners = []gatewayapiv1.Listener{
				{
					Name:     "http",
					Hostname: ptr.To[gatewayapiv1.Hostname]("*.perf.example.com"),
					Port:     80,
					Protocol: gatewayapiv1.HTTPProtocolType,
				},
			}
		})
		err := testClient().Create(ctx, gw)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func(g Gomega) {
			existingGw := &gatewayapiv1.Gateway{}
			g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(gw), existingGw)).To(Succeed())
			g.Expect(existingGw.Status.Conditions).ToNot(BeEmpty())
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())
		GinkgoWriter.Printf("test setup took %s\n", time.Since(setupStart))
	})

	AfterEach(func(ctx context.Context) {
		GinkgoWriter.Printf("cleaning up namespace %s\n", testNamespace)
		cleanupStart := time.Now()
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
		GinkgoWriter.Printf("cleanup took %s\n", time.Since(cleanupStart))
	})

	scaleCases := []int{10, 50, 100, 300}

	for _, n := range scaleCases {
		numRoutes := n

		It(fmt.Sprintf("reconciles %d AuthPolicies within acceptable time", numRoutes), func(ctx context.Context) {
			By(fmt.Sprintf("creating %d HTTPRoutes with AuthPolicies", numRoutes))

			createStart := time.Now()

			for i := range numRoutes {
				routeName := fmt.Sprintf("route-%d", i)
				route := tests.BuildBasicHttpRoute(routeName, gwName, testNamespace,
					[]string{fmt.Sprintf("app-%d.perf.example.com", i)})
				Expect(testClient().Create(ctx, route)).To(Succeed())

				policy := buildAuthPolicy(fmt.Sprintf("auth-%d", i), testNamespace, routeName)
				Expect(testClient().Create(ctx, policy)).To(Succeed())
			}

			createDuration := time.Since(createStart)
			GinkgoWriter.Printf("resource creation took %s\n", createDuration)

			By("waiting for all AuthPolicies to reach Enforced")
			reconcileStart := time.Now()

			Eventually(func(g Gomega) {
				policies := &kuadrantv1.AuthPolicyList{}
				g.Expect(testClient().List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())

				enforced := 0
				for _, p := range policies.Items {
					if meta.IsStatusConditionTrue(p.Status.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) &&
						meta.IsStatusConditionTrue(p.Status.Conditions, "Enforced") {
						enforced++
					}
				}
				GinkgoWriter.Printf("enforced: %d/%d (elapsed: %s)\n", enforced, numRoutes, time.Since(reconcileStart))
				g.Expect(enforced).To(Equal(numRoutes))
			}).WithContext(ctx).WithTimeout(30 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

			reconcileDuration := time.Since(reconcileStart)

			GinkgoWriter.Printf("\n=== RESULTS (routes=%d) ===\n", numRoutes)
			GinkgoWriter.Printf("resource creation: %s\n", createDuration)
			GinkgoWriter.Printf("reconciliation:    %s\n", reconcileDuration)
			GinkgoWriter.Printf("==========================\n\n")
		})
	}
})

func buildAuthPolicy(name, ns, routeName string) *kuadrantv1.AuthPolicy {
	return &kuadrantv1.AuthPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthPolicy",
			APIVersion: kuadrantv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: kuadrantv1.AuthPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1alpha2.ObjectName(routeName),
				},
			},
			AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
				AuthScheme: tests.BuildBasicAuthScheme(),
			},
		},
	}
}
