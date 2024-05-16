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
	"github.com/kuadrant/kuadrant-operator/test"
)

var _ = Describe("Kuadrant controller", Serial, func() {
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
			testNamespace = test.CreateNamespaceWithContext(ctx, k8sClient)
			test.ApplyKuadrantCR(k8sClient, testNamespace)
		})

		AfterEach(func(ctx SpecContext) {
			test.DeleteNamespaceCallbackWithContext(ctx, k8sClient, testNamespace)
		}, afterEachTimeOut)
		It("Copy configuration from Kuadrant CR to Limitador CR", func(ctx SpecContext) {
			kObj := &kuadrantv1beta1.Kuadrant{}
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			var tmp *int
			Expect(lObj.Spec.Replicas).Should(Equal(tmp))

			kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(1)}
			err := k8sClient.Update(ctx, kObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}
				if reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(1)) {
					return false
				}
				return true
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)

		It("Kuadrant CR configuration overrides Limitador CR configuration", func(ctx SpecContext) {
			kObj := &kuadrantv1beta1.Kuadrant{}
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			lObj.Spec.Replicas = ptr.To(1)
			err := k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())

			kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(2)}
			err = k8sClient.Update(ctx, kObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}
				if reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(2)) {
					return false
				}
				return true
			}).WithContext(ctx).Should(BeTrue())
		}, testTimeOut)
	})
})
