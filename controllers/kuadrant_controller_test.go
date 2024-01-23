//go:build integration

package controllers

import (
	"context"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var _ = FDescribe("Kuadrant controller", func() {
	var (
		testNamespace string
		kuadrant      = "kuadrant-sample"
	)
	Context("Reconcile limitador resources", func() {
		BeforeEach(func() {
			CreateNamespace(&testNamespace)
			ApplyKuadrantCR(testNamespace)
		})

		AfterEach(DeleteNamespaceCallback(&testNamespace))
		It("Copy configuration from Kuadrant CR to Limitador CR", func() {
			kObj := &kuadrantv1beta1.Kuadrant{}
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				return err == nil
			}, time.Minute, 5*time.Second).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}, time.Minute, 5*time.Second).Should(BeTrue())
			var tmp *int
			Expect(lObj.Spec.Replicas).Should(Equal(tmp))

			kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(1)}
			err := k8sClient.Update(context.Background(), kObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}
				if reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(1)) {
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("Kuadrant CR configuration overrides Limitador CR configuration", func() {
			kObj := &kuadrantv1beta1.Kuadrant{}
			lObj := &v1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}, time.Minute, 5*time.Second).Should(BeTrue())
			lObj.Spec.Replicas = ptr.To(1)
			err := k8sClient.Update(context.Background(), lObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: kuadrant, Namespace: testNamespace}, kObj)
				return err == nil
			}, time.Minute, 5*time.Second).Should(BeTrue())

			kObj.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{Replicas: ptr.To(2)}
			err = k8sClient.Update(context.Background(), kObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				if err != nil {
					return false
				}
				if reflect.DeepEqual(lObj.Spec.Replicas, ptr.To(2)) {
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})
