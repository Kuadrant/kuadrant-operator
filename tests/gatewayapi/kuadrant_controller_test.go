//go:build integration

package gatewayapi_test

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

var _ = Describe("Kuadrant controller when gateway provider is missing", func() {
	var (
		testNamespace    string
		kuadrantName     string = "local"
		afterEachTimeOut        = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("Status reports error", func(ctx SpecContext) {
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrantName,
					Namespace: testNamespace,
				},
			}
			Expect(testClient().Create(ctx, kuadrantCR)).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kObj)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(kObj.Status.Conditions, string(controllers.ReadyConditionType))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("GatewayAPIPRoviderNotFound"))
				g.Expect(cond.Message).To(Equal("GatewayAPI provider not found"))
			}, time.Minute, 15*time.Second).WithContext(ctx).Should(Succeed())
		})
	})
})
