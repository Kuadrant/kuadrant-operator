//go:build integration

package bare_k8s_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Controller", func() {
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
		It("Status is populated with missing GatewayProvide", func(ctx SpecContext) {
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

				cond := meta.FindStatusCondition(kuadrantCR.Status.Conditions, controllers.ReadyConditionType)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("GatewayAPIProviderNotFound"))
				g.Expect(cond.Message).To(Equal("GatewayAPI provider not found"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
