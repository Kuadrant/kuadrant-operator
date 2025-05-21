//go:build integration

package kuadrant

import (
	"reflect"
	"time"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("Resilience rateLimiting", Serial, func() {
	const (
		testTimeOut                 = SpecTimeout(2 * time.Minute)
		afterEachTimeOut            = NodeTimeout(4 * time.Minute)
		kuadrantResource            = "kuadrant-sample"
		ResilienceFeatureAnnotation = "kuadrant.io/experimental-dont-use-resilient-data-plane"
	)

	var testNamespace string

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

	})

	AfterEach(func(ctx SpecContext) {
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("User configures ratelimiting", Serial, func() {
		It("User applies kuadrant configuration", Serial, func(ctx SpecContext) {
			By("Configuration does not contain counterStorage")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			kuadrantCR := &kuadrantv1beta1.Kuadrant{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Kuadrant",
					APIVersion: kuadrantv1beta1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      kuadrantKey.Name,
					Namespace: kuadrantKey.Namespace,
					Labels:    tests.CommonLabels,
				},
				Spec: kuadrantv1beta1.KuadrantSpec{Resilience: &kuadrantv1beta1.Resilience{RateLimiting: true}},
			}

			err := k8sClient.Create(ctx, kuadrantCR)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resilience.counterStorage needs to be explictly configured when using resilience.rateLimiting"))

			By("Configuration is configured correctly with counterStorage")
			kuadrantCR.Spec.Resilience.CounterStorage = &limitadorv1alpha1.Storage{}
			err = k8sClient.Create(ctx, kuadrantCR)
			Expect(err).NotTo(HaveOccurred())
	
			By("counterStorage is removed after correct configuration")
			kuadrantCR.Spec.Resilience.CounterStorage = nil
			err = k8sClient.Update(ctx, kuadrantCR)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resilience.counterStorage needs to be explictly configured when using resilience.rateLimiting"))
		}, testTimeOut)
	})

	Context("User modifies", Serial, func() {
		It("The limitador resource replicas", Serial, func(ctx SpecContext) {
			By("Initial configuration is correct")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						RateLimiting: true,
						CounterStorage: &limitadorv1alpha1.Storage{},
					},
				}
			})

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("The number of replicas is incressed")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}

			lObj := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.Replicas = ptr.To(3)
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				found := false
				for _, condition := range kObj.Status.Conditions {
					if condition.Type == controllers.ResilienceInfoRRConditionType {
						found = true
						g.Expect(condition.Message).To(ContainSubstring("greater than minimum default"))
					}
				}
				g.Expect(found).To(Equal(true))},
			).WithContext(ctx).Should(Succeed())

			By("The number of replicas is decreased")
			lObj = &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.Replicas = ptr.To(0)
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				found := false
				for _, condition := range kObj.Status.Conditions {
					if condition.Type == controllers.ResilienceWarningRRConditionType {
						found = true
						g.Expect(condition.Message).To(ContainSubstring("below minimum default"))
					}
				}
				g.Expect(found).To(Equal(true))},
			).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("Changes Reverted", Serial, func() {
		It("User removes default configuration", Serial, func(ctx SpecContext) {
			By("Deploy configured kuadrant resource")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						RateLimiting: true,
						CounterStorage: &limitadorv1alpha1.Storage{},
					},
				}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Check the replica vaules in the limitador resource")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(*lObj.Spec.Replicas).To(Equal(controllers.LimitadorReplicas))
				},
			).WithContext(ctx).Should(Succeed())

			By("Disable the rateLimiting feature")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err := k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience.RateLimiting = false
			err = k8sClient.Update(ctx, kObj)
			Expect(err).NotTo(HaveOccurred())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Check the replica vaules in the limitador resource have being reverted")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(*lObj.Spec.Replicas).To(Equal(1))
				},
			).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("Limitador resource", Serial, func() {
		It("Has existing replica configation", Serial, func(ctx SpecContext) {
			By("Deploy blank kuadrant resource")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
			
				g.Expect(lObj.Spec.Replicas).To(BeNil())
				},
			).WithContext(ctx).Should(Succeed())

			By("Update the number of replicas in the limitador resource")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			lObj := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.Replicas = ptr.To(0)
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			By("Enabe rateLimiting in the kuadrant resource")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err = k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience = &kuadrantv1beta1.Resilience{
				RateLimiting: true,
				CounterStorage: &limitadorv1alpha1.Storage{},
			}
			err = k8sClient.Update(ctx, kObj)
			Expect(err).NotTo(HaveOccurred())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("limitador resource keeps initial configuration")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(*lObj.Spec.Replicas).To(Equal(0))
				},
			).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("Kuadrant Resliences", Serial, func() {
		It("PDB Configured and User Modified", Serial, func(ctx SpecContext) {
			By("Deploy configured kuadrant resource")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						RateLimiting: true,
						CounterStorage: &limitadorv1alpha1.Storage{},
					},
				}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Limitador spec has PDB configuration")
			Eventually(func (g Gomega) {
				configuration := &limitadorv1alpha1.PodDisruptionBudgetType{MaxUnavailable: &intstr.IntOrString{IntVal: 1}}
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.PodDisruptionBudget).ToNot(BeNil())
				g.Expect(*lObj.Spec.PodDisruptionBudget).To(Equal(*configuration))
				},
			).WithContext(ctx).Should(Succeed())

			By("User modifies the max unavailable")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			lObj := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.PodDisruptionBudget.MaxUnavailable.IntVal = 2
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			By("Limitador PDB spec is modified")
			Eventually(func (g Gomega) {
				configuration := &limitadorv1alpha1.PodDisruptionBudgetType{MaxUnavailable: &intstr.IntOrString{IntVal: 2}}
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.PodDisruptionBudget).ToNot(BeNil())
				g.Expect(*lObj.Spec.PodDisruptionBudget).To(Equal(*configuration))
				},
			).WithContext(ctx).Should(Succeed())

			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				found := false
				for _, condition := range kObj.Status.Conditions {
					if condition.Type == controllers.ResilienceInfoPDBConditionType {
						found = true
						g.Expect(condition.Message).To(ContainSubstring("Limitador recource Pod Disruption Budget differs from default configuration"))
					}
				}
				g.Expect(found).To(Equal(true))},
			).WithContext(ctx).Should(Succeed())

			By("Limitador PDB spec switched to Min Available")
			Eventually(func (g Gomega) {
				err = k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())

				lObj.Spec.PodDisruptionBudget.MaxUnavailable = nil
				lObj.Spec.PodDisruptionBudget.MinAvailable = &intstr.IntOrString{IntVal: 1}

				err = k8sClient.Update(ctx, lObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			Eventually(func (g Gomega) {
				configuration := &limitadorv1alpha1.PodDisruptionBudgetType{MinAvailable: &intstr.IntOrString{IntVal: 1}}
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.PodDisruptionBudget).ToNot(BeNil())
				g.Expect(*lObj.Spec.PodDisruptionBudget).To(Equal(*configuration))
				},
			).WithContext(ctx).Should(Succeed())

			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				found := false
				for _, condition := range kObj.Status.Conditions {
					if condition.Type == controllers.ResilienceInfoPDBConditionType {
						found = true
						g.Expect(condition.Message).To(ContainSubstring("Limitador recource Pod Disruption Budget differs from default configuration"))
					}
				}
				g.Expect(found).To(Equal(true))},
			).WithContext(ctx).Should(Succeed())

			By("User disables resilience feature in kuadrant")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err = k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience.RateLimiting = false
			err = k8sClient.Update(ctx, kObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.PodDisruptionBudget.MaxUnavailable).To(BeNil())
				},
			).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("Limitador resource (PDB)", Serial, func() {
		It("PDB User Configured Initial", Serial, func(ctx SpecContext) {
			By("Deploy blank kuadrant resource")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Update the MaxUnavailable in the limitador resource")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			lObj := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.PodDisruptionBudget = &limitadorv1alpha1.PodDisruptionBudgetType{MaxUnavailable: &intstr.IntOrString{IntVal: 2}}
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			By("Enabe rateLimiting in the kuadrant resource")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err = k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience = &kuadrantv1beta1.Resilience{
				RateLimiting: true,
				CounterStorage: &limitadorv1alpha1.Storage{},
			}
			err = k8sClient.Update(ctx, kObj)
			Expect(err).NotTo(HaveOccurred())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("limitador resource keeps initial configuration")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(*&lObj.Spec.PodDisruptionBudget.MaxUnavailable.IntVal).To(Equal(int32(2)))
				},
			).WithContext(ctx).Should(Succeed())

			By("Kuadrant resource gives correct status message")
			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				found := false
				for _, condition := range kObj.Status.Conditions {
					if condition.Type == controllers.ResilienceInfoPDBConditionType {
						found = true
						g.Expect(condition.Message).To(ContainSubstring("Limitador recource Pod Disruption Budget differs from default configuration"))
					}
				}
				g.Expect(found).To(Equal(true))},
			).WithContext(ctx).Should(Succeed())

		}, testTimeOut)
	})

	Context("Limitador Resource Requirements", Serial, func() {
		Resource_10Mi := "10Mi"
		Resource_15Mi := "10Mi"
		Resource_10m  := "10m"
		Resource_15m  := "15m"
		cpu, err := resource.ParseQuantity(Resource_10m)
		Expect(err).Error().ToNot(HaveOccurred())
		userCpu, err := resource.ParseQuantity(Resource_15m)
		Expect(err).Error().ToNot(HaveOccurred())
		memory, err := resource.ParseQuantity(Resource_10Mi)
		Expect(err).Error().ToNot(HaveOccurred())
		userMemory, err := resource.ParseQuantity(Resource_15Mi)
		Expect(err).Error().ToNot(HaveOccurred())

		It("User enables the feature", Serial, func(ctx SpecContext) {
			By("Create kuadrant resource with reslience enabled")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						RateLimiting: true,
						CounterStorage: &limitadorv1alpha1.Storage{},
					},
				}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Limitador resource has the correct resource requirements")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Cpu().Value()).To(Equal(cpu.Value()))
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Memory().Value()).To(Equal(memory.Value()))
				},
			).WithContext(ctx).Should(Succeed())

			By("User can modify there resources")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			lObj := &limitadorv1alpha1.Limitador{}
			err := k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())

			lObj.Spec.ResourceRequirements.Requests[corev1.ResourceCPU] = userCpu
			lObj.Spec.ResourceRequirements.Requests[corev1.ResourceMemory] = userMemory
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			By("User configuration of limitador was not reverted.")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Cpu().Value()).To(Equal(userCpu.Value()))
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Memory().Value()).To(Equal(userMemory.Value()))
				},
			).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("Limitador resource (ResourceRequirements)", Serial, func() {
		Resource_15Mi := "10Mi"
		Resource_15m  := "15m"
		userCpu, err := resource.ParseQuantity(Resource_15m)
		Expect(err).Error().ToNot(HaveOccurred())
		userMemory, err := resource.ParseQuantity(Resource_15Mi)
		Expect(err).Error().ToNot(HaveOccurred())

		It("the user has existing resource configuration", Serial, func(ctx SpecContext) {

			By("Deploy a standard kuadrant")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Configure limitador")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			lObj := &limitadorv1alpha1.Limitador{}
			err = k8sClient.Get(ctx, limitadorKey, lObj)
			Expect(err).ToNot(HaveOccurred())
			lObj.Spec.ResourceRequirements = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: userMemory, 
					corev1.ResourceCPU: userCpu,
				},
			}
			err = k8sClient.Update(ctx, lObj)
			Expect(err).ToNot(HaveOccurred())
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())

			By("Enable Resilient deployment")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err = k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience = &kuadrantv1beta1.Resilience{
				RateLimiting: true,
				CounterStorage: &limitadorv1alpha1.Storage{},
			}
			err = k8sClient.Update(ctx, kObj)
			Expect(err).NotTo(HaveOccurred())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Existing limitador configuration is not overridion")
			Eventually(func (g Gomega) {
				limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
				lObj := &limitadorv1alpha1.Limitador{}
				err := k8sClient.Get(ctx, limitadorKey, lObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Cpu().Value()).To(Equal(userCpu.Value()))
				g.Expect(lObj.Spec.ResourceRequirements.Requests.Memory().Value()).To(Equal(userMemory.Value()))
				},
			).WithContext(ctx).Should(Succeed())
			By("")
			By("")
		}, testTimeOut)
	})

	Context("Limitador Deployment (Topology Spread Constraints)", Serial, func() {
		It("user has existing topology spread constraints", Serial, func(ctx SpecContext) {
			By("Deploy basic kuadrant resource")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Set the topology spread contraints on the limitador deployment")
			// The configuration should be different than the default that kuadrant uses
			limitadorDeploymentKey := client.ObjectKey{Name: "limitador-limitador", Namespace: testNamespace}
			lDeployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
			Expect(err).ToNot(HaveOccurred())


			hostnameConstraint := corev1.TopologySpreadConstraint{
				MaxSkew:           2,
				TopologyKey:       "kubernetes.io/hostname",
				WhenUnsatisfiable: "ScheduleAnyway",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"limitador-reource": "limitador"},
				},
			}
			lDeployment.Spec.Template.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{hostnameConstraint}
			err = k8sClient.Update(ctx, lDeployment)
			Expect(err).NotTo(HaveOccurred())

			By("Enable the limitador reilient deployment in the kuadrant resource")
			kObj := &kuadrantv1beta1.Kuadrant{}
			err = k8sClient.Get(ctx, kuadrantKey, kObj)
			Expect(err).ToNot(HaveOccurred())
			kObj.Spec.Resilience = &kuadrantv1beta1.Resilience{
				RateLimiting: true,
				CounterStorage: &limitadorv1alpha1.Storage{},
			}
			err = k8sClient.Update(ctx, kObj)
			Expect(err).NotTo(HaveOccurred())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("User configuration should not be modified")
			err = k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
			Expect(err).ToNot(HaveOccurred())

			for _, item := range lDeployment.Spec.Template.Spec.TopologySpreadConstraints {
				if item.TopologyKey == "kubernetes.io/hostname" {
					Expect(item.MaxSkew).To(Equal(int32(2)))
				} 
			}

			By("Kuadrant status should should the topology spread contraints are not the default")
			Eventually(func (g Gomega) {
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				contains := false
				for _, item := range kObj.Status.Conditions {
					if item.Reason == "UserModifiedLimitadorTopologySpreadConstraints" {
						contains = true
						break
					}
				}
				g.Expect(contains).To(Equal(true))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("Limitador Deployment (Topology Spread Constraints)", Serial, func() {
		It("user updates topology spread contraints after feature enabled", Serial, func(ctx SpecContext) {
			By("Create kuadrant resource with reslience enabled")
			kuadrantKey := client.ObjectKey{Name: kuadrantResource, Namespace: testNamespace}
			tests.ApplyKuadrantCRWithName(ctx, testClient(), testNamespace, kuadrantResource, func(k *kuadrantv1beta1.Kuadrant) {
				k.Annotations = map[string]string{ResilienceFeatureAnnotation: "true"}
				k.Spec = kuadrantv1beta1.KuadrantSpec{
					Resilience: &kuadrantv1beta1.Resilience{
						RateLimiting: true,
						CounterStorage: &limitadorv1alpha1.Storage{},
					},
				}
			})
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())
			
			By("Limitador Deployment has correct configuration")
			Eventually(func (g Gomega) {
				limitadorDeploymentKey := client.ObjectKey{Name: "limitador-limitador", Namespace: testNamespace}
				lDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
				g.Expect(err).ToNot(HaveOccurred())

				correctConfiguration := 0
				for _, item := range lDeployment.Spec.Template.Spec.TopologySpreadConstraints {
					if item.TopologyKey == "kubernetes.io/hostname" {
						hostnameConstraint := corev1.TopologySpreadConstraint{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: "ScheduleAnyway",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"limitador-reource": "limitador"},
							},
						}
						if reflect.DeepEqual(item, hostnameConstraint) {
							correctConfiguration += 1
						}
					}

					if item.TopologyKey == "kubernetes.io/zone" {
						zoneConstraint := corev1.TopologySpreadConstraint{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/zone",
							WhenUnsatisfiable: "ScheduleAnyway",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"limitador-reource": "limitador"},
							},
						}
						if reflect.DeepEqual(item, zoneConstraint) {
							correctConfiguration += 1
						}
					}
				}
				g.Expect(correctConfiguration).To(Equal(2))

			}).WithContext(ctx).Should(Succeed())


			By("User sets the topology spread contraints on the limitador deployment")
			// The configuration should be different than the default that kuadrant uses
			limitadorDeploymentKey := client.ObjectKey{Name: "limitador-limitador", Namespace: testNamespace}
			lDeployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
			Expect(err).ToNot(HaveOccurred())


			hostnameConstraint := corev1.TopologySpreadConstraint{
				MaxSkew:           2,
				TopologyKey:       "kubernetes.io/hostname",
				WhenUnsatisfiable: "ScheduleAnyway",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"limitador-reource": "limitador"},
				},
			}
			lDeployment.Spec.Template.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{hostnameConstraint}
			err = k8sClient.Update(ctx, lDeployment)
			Expect(err).NotTo(HaveOccurred())

			By("User configuration should not be modified")
			err = k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
			Expect(err).ToNot(HaveOccurred())

			for _, item := range lDeployment.Spec.Template.Spec.TopologySpreadConstraints {
				if item.TopologyKey == "kubernetes.io/hostname" {
					Expect(item.MaxSkew).To(Equal(int32(2)))
				} 
			}

			By("Kuadrant status should show the topology spread contraints are not the default")
			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				contains := false
				for _, item := range kObj.Status.Conditions {
					if item.Reason == "UserModifiedLimitadorTopologySpreadConstraints" {
						contains = true
						break
					}
				}
				g.Expect(contains).To(Equal(true))
			}).WithContext(ctx).Should(Succeed())

			By("User removes a constraint from the limitador deployment")
			limitadorKey := client.ObjectKey{Name: kuadrant.LimitadorName, Namespace: testNamespace}
			Eventually(tests.LimitadorIsReady(testClient(), limitadorKey)).WithContext(ctx).Should(Succeed())
			err = k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
			Expect(err).ToNot(HaveOccurred())
			lDeployment.Spec.Template.Spec.TopologySpreadConstraints = nil
			err = k8sClient.Update(ctx, lDeployment)
			Expect(err).NotTo(HaveOccurred())

			By("Kuadrant operator recreates the removed constraint")
			Eventually(func (g Gomega) {
				limitadorDeploymentKey := client.ObjectKey{Name: "limitador-limitador", Namespace: testNamespace}
				lDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
				g.Expect(err).ToNot(HaveOccurred())

				correctConfiguration := 0
				for _, item := range lDeployment.Spec.Template.Spec.TopologySpreadConstraints {
					if item.TopologyKey == "kubernetes.io/hostname" {
						hostnameConstraint := corev1.TopologySpreadConstraint{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: "ScheduleAnyway",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"limitador-reource": "limitador"},
							},
						}
						if reflect.DeepEqual(item, hostnameConstraint) {
							correctConfiguration += 1
						}
					}

					if item.TopologyKey == "kubernetes.io/zone" {
						zoneConstraint := corev1.TopologySpreadConstraint{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/zone",
							WhenUnsatisfiable: "ScheduleAnyway",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"limitador-reource": "limitador"},
							},
						}
						if reflect.DeepEqual(item, zoneConstraint) {
							correctConfiguration += 1
						}
					}
				}
				g.Expect(correctConfiguration).To(Equal(2))

			}).WithContext(ctx).Should(Succeed())

			By("User disable the resilient feature for limitador")
			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				kObj.Spec.Resilience = &kuadrantv1beta1.Resilience{
					RateLimiting: false,
					CounterStorage: &limitadorv1alpha1.Storage{},
				}
				err = k8sClient.Update(ctx, kObj)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithContext(ctx).Should(Succeed())
			Eventually(tests.KuadrantIsReady(testClient(), kuadrantKey)).WithContext(ctx).Should(Succeed())

			By("Expect no error status messages in the kuadrant resource")
			Eventually(func (g Gomega) {
				kObj := &kuadrantv1beta1.Kuadrant{}
				err = k8sClient.Get(ctx, kuadrantKey, kObj)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(kObj.Status.Conditions)).To(Equal(1))
			}).WithContext(ctx).Should(Succeed())

			By("Expect the topology spread constraints to be removed from the limitador deployment")
			Eventually(func (g Gomega) {
				limitadorDeploymentKey := client.ObjectKey{Name: "limitador-limitador", Namespace: testNamespace}
				lDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, limitadorDeploymentKey, lDeployment)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(lDeployment.Spec.Template.Spec.TopologySpreadConstraints).To(BeNil())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
