//go:build integration

package kuadrant

import (
	"reflect"
	"time"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller", func() {
	var (
		testNamespace string
	)
	const (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
		kuadrant         = "kuadrant-sample"
	)
	Context("Reconcile limitador resources", func() {
		BeforeEach(func(ctx SpecContext) {
			testNamespace = tests.CreateNamespace(ctx, testClient())
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrant)
		})

		AfterEach(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), testNamespace)
		}, afterEachTimeOut)
		It("Copy configuration from Kuadrant CR to Limitador CR", func(ctx SpecContext) {
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			var tmp *int
			Expect(lObj.Spec.Replicas).Should(Equal(tmp))

			Eventually(func() bool {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				if err != nil {
					return false
				}
				kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(1)}
				err = k8sClient.Update(ctx, kObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}

				return reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(1))
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Kuadrant CR configuration overrides Limitador CR configuration", func(ctx SpecContext) {
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}
				lObj.Spec.Replicas = ptr.To(1)
				err = k8sClient.Update(ctx, lObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				if err != nil {
					return false
				}
				kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(2)}
				err = k8sClient.Update(ctx, kObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}

				return reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(2))
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
