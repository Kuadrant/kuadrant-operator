//go:build integration

package istio_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller on istio", func() {
	var (
		testNamespace    string
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when test suite kuadrant CR exists on cluster", func() {
		It("Status is ready", func(ctx SpecContext) {
			kuadrantList := &kuadrantv1beta1.KuadrantList{}
			Expect(testClient().List(ctx, kuadrantList)).ToNot(HaveOccurred())
			Expect(len(kuadrantList.Items)).To(Equal(1))
			kuadrantCR := &kuadrantList.Items[0]

			Eventually(func(g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kObj)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(kObj.Status.Conditions, string(controllers.ReadyConditionType))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("Ready"))
				g.Expect(cond.Message).To(Equal("Kuadrant is ready"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
