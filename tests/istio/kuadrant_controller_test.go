//go:build integration

package istio_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller is disabled", func() {
	var (
		testNamespace    string
		kuadrantName     string = "local"
		afterEachTimeOut        = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespaceWithContext(ctx, testClient())
	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespaceCallbackWithContext(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("when default kuadrant CR is created", func() {
		It("Status is not populated", func(ctx SpecContext) {
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

			kObj := &kuadrantv1beta1.Kuadrant{}
			err := testClient().Get(ctx, client.ObjectKeyFromObject(kuadrantCR), kObj)
			Expect(err).ToNot(HaveOccurred())
			// expected empty. The controller should not have updated it
			// TODO: status should not be empty
			Expect(kObj.Status).To(Equal(kuadrantv1beta1.KuadrantStatus{}))
		})
	})
})
