//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/multicluster"
)

var _ = Describe("DNSPolicy controller", func() {

	var gatewayClass *gatewayapiv1.GatewayClass
	var managedZone *kuadrantdnsv1alpha1.ManagedZone
	var testNamespace string
	var gateway *gatewayapiv1.Gateway
	var dnsPolicy *v1alpha1.DNSPolicy
	var recordName, wildcardRecordName string
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		CreateNamespace(&testNamespace)

		gatewayClass = testBuildGatewayClass("foo", "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		managedZone = testBuildManagedZone("mz-example-com", testNamespace, "example.com")
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())
	})

	AfterEach(func() {
		if gateway != nil {
			err := k8sClient.Delete(ctx, gateway)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if dnsPolicy != nil {
			err := k8sClient.Delete(ctx, dnsPolicy)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if managedZone != nil {
			err := k8sClient.Delete(ctx, managedZone)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if gatewayClass != nil {
			err := k8sClient.Delete(ctx, gatewayClass)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		DeleteNamespaceCallback(&testNamespace)()
	})

	Context("invalid target", func() {

		BeforeEach(func() {
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway("test-gateway").
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should have accepted condition with status false and correct reason", func() {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonTargetNotFound)),
						"Message": Equal("DNSPolicy target test-gateway was not found"),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should have accepted condition with status true", func() {
			By("creating a valid Gateway")

			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Message": Equal("DNSPolicy has been accepted"),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should not allow changing routing strategy", func() {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				dnsPolicy.Spec.RoutingStrategy = v1alpha1.LoadBalancedRoutingStrategy
				err = k8sClient.Update(ctx, dnsPolicy)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("RoutingStrategy is immutable")))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should not process gateway with inconsistent addresses", func() {
			// build invalid gateway
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener(TestListenerNameOne, TestHostTwo).Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

			// ensure Gateway exists and invalidate it by setting inconsistent addresses
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: gateway.Name, Namespace: gateway.Namespace}, gateway)
				g.Expect(err).ToNot(HaveOccurred())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.HostnameAddressType),
						Value: TestIPAddressOne,
					},
					{
						Type:  ptr.To(multicluster.MultiClusterIPAddressType),
						Value: TestIPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           TestClusterNameOne + "." + TestListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           TestListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				err = k8sClient.Status().Update(ctx, gateway)
				g.Expect(err).ToNot(HaveOccurred())
			}, TestTimeoutMedium, TestRetryIntervalMedium).Should(Succeed())

			// expect no dns records
			Consistently(func() []kuadrantdnsv1alpha1.DNSRecord {
				dnsRecords := kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, &dnsRecords, client.InNamespace(dnsPolicy.GetNamespace()))
				Expect(err).ToNot(HaveOccurred())
				return dnsRecords.Items
			}, time.Second*15, time.Second).Should(BeEmpty())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(kuadrant.PolicyReasonUnknown)),
						"Message": ContainSubstring("gateway is invalid: inconsistent status addresses"),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

	})

	Context("valid target with no gateway status", func() {
		testGatewayName := "test-no-gateway-status"

		BeforeEach(func() {
			gateway = NewGatewayBuilder(testGatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(TestListenerNameOne, TestHostTwo).
				Gateway
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(testGatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)

			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should not create a dns record", func() {
			Consistently(func() []kuadrantdnsv1alpha1.DNSRecord { // DNS record exists
				dnsRecords := kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, &dnsRecords, client.InNamespace(dnsPolicy.GetNamespace()))
				Expect(err).ToNot(HaveOccurred())
				return dnsRecords.Items
			}, time.Second*15, time.Second).Should(BeEmpty())
		})

		It("should have accepted and not enforced status", func() {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Message": Equal("DNSPolicy has been accepted"),
						}),
						MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
							"Status":  Equal(metav1.ConditionFalse),
							"Reason":  Equal(PolicyReasonUnknown),
							"Message": Equal("DNSPolicy has encountered some issues: policy is not enforced on any dns record: no routes attached for listeners"),
						})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should set gateway back reference", func() {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				gw := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: gateway.Name, Namespace: testNamespace}, gw)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})
	})

	Context("valid target and valid gateway status", func() {

		BeforeEach(func() {
			gateway = NewGatewayBuilder(TestGatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(TestListenerNameOne, TestHostTwo).
				WithHTTPListener(TestListenerNameWildcard, TestHostWildcard).
				Gateway
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(TestGatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)

			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				Expect(err).ShouldNot(HaveOccurred())

				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(multicluster.MultiClusterIPAddressType),
						Value: TestClusterNameOne + "/" + TestIPAddressOne,
					},
					{
						Type:  ptr.To(multicluster.MultiClusterIPAddressType),
						Value: TestClusterNameTwo + "/" + TestIPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           TestClusterNameOne + "." + TestListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           TestClusterNameTwo + "." + TestListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           TestClusterNameOne + "." + TestListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           TestClusterNameTwo + "." + TestListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				return k8sClient.Status().Update(ctx, gateway)
			}, TestTimeoutMedium, TestRetryIntervalMedium).ShouldNot(HaveOccurred())

			recordName = fmt.Sprintf("%s-%s", TestGatewayName, TestListenerNameOne)
			wildcardRecordName = fmt.Sprintf("%s-%s", TestGatewayName, TestListenerNameWildcard)
		})

		It("should have correct status", func() {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Finalizers).To(ContainElement(DNSPolicyFinalizer))
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Message": Equal("DNSPolicy has been accepted"),
						}),
						MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
							"Message": Equal("DNSPolicy has been successfully enforced"),
						})),
				)
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(DNSPolicyAffected),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal(string(gatewayapiv1alpha2.PolicyReasonAccepted)),
						"ObservedGeneration": Equal(gateway.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			// ensure there are no policies with not accepted condition
			// in this case the "enforced" on the policy should be false
			Eventually(func(g Gomega) {
				dnsRecord := &kuadrantdnsv1alpha1.DNSRecord{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecord.Status.Conditions).ToNot(ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
					"Status": Equal(metav1.ConditionFalse),
				})))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should set gateway back reference", func() {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should remove dns records when listener removed", func() {
			//get the gateway and remove the listeners

			Eventually(func() error {
				existingGateway := &gatewayapiv1.Gateway{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway); err != nil {
					return err
				}
				newListeners := []gatewayapiv1.Listener{}
				for _, existing := range existingGateway.Spec.Listeners {
					if existing.Name == TestListenerNameWildcard {
						newListeners = append(newListeners, existing)
					}
				}

				patch := client.MergeFrom(existingGateway.DeepCopy())
				existingGateway.Spec.Listeners = newListeners
				rec := &kuadrantdnsv1alpha1.DNSRecord{}
				if err := k8sClient.Patch(ctx, existingGateway, patch); err != nil {
					return err
				}
				//dns record should be removed for non wildcard
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, rec); err != nil && !k8serrors.IsNotFound(err) {
					return err
				}
				return k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, rec)
			}, time.Second*10, time.Second).Should(BeNil())
		})

		It("should remove gateway back reference on policy deletion", func() {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
				g.Expect(gateway.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(DNSPolicyAffected),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal(string(gatewayapiv1alpha2.PolicyReasonAccepted)),
						"ObservedGeneration": Equal(gateway.Generation),
					})),
				)

				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Finalizers).To(ContainElement(DNSPolicyFinalizer))
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("deleting the dns policy")
			Expect(k8sClient.Delete(ctx, dnsPolicy)).To(BeNil())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).ToNot(HaveKey(v1alpha1.DNSPolicyDirectReferenceAnnotationName))
				g.Expect(gateway.Annotations).ToNot(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
				g.Expect(gateway.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(DNSPolicyAffected)),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should remove dns record reference on policy deletion even if gateway is removed", func() {

			Eventually(func() error { // DNS record exists
				return k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})
			}, TestTimeoutMedium, TestRetryIntervalMedium).Should(Succeed())

			err := k8sClient.Delete(ctx, gateway)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			err = k8sClient.Delete(ctx, dnsPolicy)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			//ToDo We cant assume that a dnsrecord reconciler is running that will remove the record or that it will be removed
			// It's not the responsibility of this operator to do this, so we should just check if it's gone
			// (in case we are running on a cluster that actually has a dnsrecord reconciler running), or that it is marked for deletion
			//Eventually(func() error { // DNS record removed
			//	return k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})
			//}, TestTimeoutMedium, TestRetryIntervalMedium).Should(MatchError(ContainSubstring("not found")))

		})

	})

})
