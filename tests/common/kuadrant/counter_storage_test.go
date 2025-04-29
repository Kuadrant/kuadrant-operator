//go:build integration

package kuadrant

import (
	"reflect"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Resilience counterStorage", Serial, func() {
	const (
		testTimeOut                 = SpecTimeout(1 * time.Minute)
		afterEachTimeOut            = NodeTimeout(2 * time.Minute)
		kuatrantResource            = "kuadrant-sample"
		ResilienceFeatureAnnotation = "kuadrant.io/experimental-dont-use-resilient-data-plane"
	)

	var testNamespace string

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("User configures counterStorage", Serial, func() {
		It("limitador resource is configured", func(ctx SpecContext) {
			By("Set up the initail kuadrant counterStorage configuration")
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: testNamespace}
			spec := &limitadorv1alpha1.Storage{
				Disk: &limitadorv1alpha1.DiskSpec{},
			}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuatrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						CounterStorage: spec,
					},
				}
			})

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				result := reflect.DeepEqual(existingLimitador.Spec.Storage,  spec)
				g.Expect(result).To(BeTrue()) 

			}).WithContext(ctx).Should(Succeed())

			By("Update kuadrant counterStorage configuration")

			existingKuadrant := &kuadrantv1beta1.Kuadrant{}
			err := k8sClient.Get(ctx, kuadrantKey, existingKuadrant)
			Expect(err).ToNot(HaveOccurred())
			existingKuadrant.Spec.Resilience.CounterStorage = &limitadorv1alpha1.Storage{}
			
			err = k8sClient.Update(ctx, existingKuadrant)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				result := reflect.DeepEqual(existingLimitador.Spec.Storage,  &limitadorv1alpha1.Storage{})
				g.Expect(result).To(BeTrue()) 

			}).WithContext(ctx).Should(Succeed())

			By("kuadrant counterStorage configuration removed")

			err = k8sClient.Get(ctx, kuadrantKey, existingKuadrant)
			Expect(err).ToNot(HaveOccurred())
			existingKuadrant.Spec.Resilience.CounterStorage = nil
			
			err = k8sClient.Update(ctx, existingKuadrant)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				result := (existingLimitador.Spec.Storage == nil)
				g.Expect(result).To(BeTrue()) 

			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})


	Context("User set the Storage configuration directly in the limitador resource", Serial, func() {
		It("not reverting of chanages happen", func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: testNamespace}
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

			By("Setup the basic kuadrant installation")
			tests.ApplyKuadrantCR(ctx, testClient(), testNamespace)
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			
			By("Configure the storage in the limitador resource")
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
			Expect(err).ToNot(HaveOccurred())

			existingLimitador.Spec.Storage = &limitadorv1alpha1.Storage{Disk: &limitadorv1alpha1.DiskSpec{}}
			err = k8sClient.Update(ctx, existingLimitador)
			Expect(err).ToNot(HaveOccurred())

			By("Check that kuadrant and limitador are ready")
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			

			By("Check that the limitador resource is still configured as expected.")
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				result := reflect.DeepEqual(existingLimitador.Spec.Storage,  &limitadorv1alpha1.Storage{Disk: &limitadorv1alpha1.DiskSpec{}})
				g.Expect(result).To(BeTrue()) 

			}).WithContext(ctx).Should(Succeed())


		}, testTimeOut)
	})

	Context("counterStorage is configured", Serial, func() {
		It("user modifies the storage configuration in the limitador resource", func(ctx SpecContext) {
			kuadrantKey := client.ObjectKey{Name: "kuadrant-sample", Namespace: testNamespace}
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

			By("Apply kurdrant resouce with counterStorage configured")
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuatrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						CounterStorage: &limitadorv1alpha1.Storage{
							Disk: &limitadorv1alpha1.DiskSpec{},
						},
					},
				}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Modify the storage in the limitador resource")
			existingLimitador := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
			Expect(err).ToNot(HaveOccurred())

			existingLimitador.Spec.Storage = &limitadorv1alpha1.Storage{}
			err = k8sClient.Update(ctx, existingLimitador)
			Expect(err).ToNot(HaveOccurred())

			By("Check for everything to be come ready")
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Check the limitador resource is still equal to the kuadrant resource")
			Eventually(func(g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

				existingLimitador := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, existingLimitador)
				g.Expect(err).ToNot(HaveOccurred())
				result := reflect.DeepEqual(existingLimitador.Spec.Storage,  &limitadorv1alpha1.Storage{Disk: &limitadorv1alpha1.DiskSpec{}})
				g.Expect(result).To(BeTrue()) 

			}).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})
})
