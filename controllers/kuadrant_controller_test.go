//go:build integration

package controllers

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

var _ = Describe("Kuadrant controller deploys limitador", func() {
	var (
		testNamespace    string
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	BeforeEach(func(ctx SpecContext) {
		testNamespace = CreateNamespaceWithContext(ctx)
	})
	AfterEach(func(ctx SpecContext) {
		DeleteNamespaceCallbackWithContext(ctx, testNamespace)
	}, afterEachTimeOut)

	Context("when config is empty", func() {
		It("creates basic Limitador CR", func(ctx SpecContext) {
			ApplyKuadrantCR(testNamespace, func(kCR *kuadrantv1beta1.Kuadrant) {
				kCR.Spec.Limitador = nil
			})

			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}

			Eventually(testLimitadorIsReady(ctx, limitadorKey)).WithContext(ctx).Should(Succeed())
		})
	})

	Context("when config has fields set", func() {
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

			ApplyKuadrantCR(testNamespace, func(kCR *kuadrantv1beta1.Kuadrant) {
				kCR.Spec.Limitador = &kuadrantv1beta1.LimitadorSpec{}
				kCR.Spec.Limitador.Affinity = affinity
				kCR.Spec.Limitador.PodDisruptionBudget = podDisruptionBudget
				kCR.Spec.Limitador.Storage = storage
			})

			limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: testNamespace}

			Eventually(testLimitadorIsReady(ctx, limitadorKey)).WithContext(ctx).Should(Succeed())

			limitador := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, limitador)
			// must exist
			Expect(err).ToNot(HaveOccurred())
			Expect(limitador.Spec.Affinity).To(Equal(affinity))
			Expect(limitador.Spec.PodDisruptionBudget).To(Equal(podDisruptionBudget))
			Expect(limitador.Spec.Storage).To(Equal(storage))
		})
	})
})
