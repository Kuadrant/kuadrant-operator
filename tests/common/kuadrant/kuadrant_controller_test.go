//go:build integration

package kuadrant

import (
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Kuadrant controller", Serial, func() {
	var (
		testNamespace string
	)
	const (
		testTimeOut       = SpecTimeout(2 * time.Minute)
		afterEachTimeOut  = NodeTimeout(3 * time.Minute)
		beforeEachTimeOut = NodeTimeout(2 * time.Minute)
		kuadrant          = "kuadrant-sample"
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())
	}, beforeEachTimeOut)

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("Reconcile limitador resources", func() {
		BeforeEach(func(ctx SpecContext) {
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrant)
		}, beforeEachTimeOut)

		It("Copy configuration from Kuadrant CR to Limitador CR", func(ctx SpecContext) {
			lObj := &limitadorv1alpha1.Limitador{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}, lObj)
				return err == nil
			}).WithContext(ctx).Should(BeTrue())
			Expect(lObj.Spec.Replicas).Should(BeNil())

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
			lObj := &limitadorv1alpha1.Limitador{}

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

	Context("deploy limitador resources", func() {
		It("creates basic Limitador CR", func(ctx SpecContext) {
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrant, func(kCR *kuadrantv1beta1.Kuadrant) {
				kCR.Spec.Limitador = nil
			})

			kuadrantKey := client.ObjectKey{Name: kuadrant, Namespace: testNamespace}

			Eventually(tests.KuadrantIsReady(ctx, testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}

			Eventually(tests.LimitadorIsReady(ctx, testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("Limitador CR has the same fields set", func(ctx SpecContext) {
			var (
				affinity *corev1.Affinity = &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "limitador",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": "limitador",
										},
									},
								},
							},
						},
					},
				}

				podDisruptionBudget *limitadorv1alpha1.PodDisruptionBudgetType = &limitadorv1alpha1.PodDisruptionBudgetType{
					MinAvailable: &intstr.IntOrString{
						IntVal: 1,
					},
				}

				storage *limitadorv1alpha1.Storage = &limitadorv1alpha1.Storage{
					Disk: &limitadorv1alpha1.DiskSpec{},
				}
			)
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrant, func(kCR *kuadrantv1beta1.Kuadrant) {
				kCR.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{}
				kCR.Spec.Limitador.Affinity = affinity
				kCR.Spec.Limitador.PodDisruptionBudget = podDisruptionBudget
				kCR.Spec.Limitador.Storage = storage
			})

			kuadrantKey := client.ObjectKey{Name: kuadrant, Namespace: testNamespace}

			Eventually(tests.KuadrantIsReady(ctx, testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}

			Eventually(tests.LimitadorIsReady(ctx, testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			limitador := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, limitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(limitador.Spec.Affinity).To(Equal(affinity))
			Expect(limitador.Spec.PodDisruptionBudget).To(Equal(podDisruptionBudget))
			Expect(limitador.Spec.Storage).To(Equal(storage))
		}, testTimeOut)
	})
})
