//go:build integration

package dnspolicy

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/rand"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("DNSPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(1 * time.Minute)
		afterEachTimeOut = NodeTimeout(2 * time.Minute)
	)

	var gatewayClass *gatewayapiv1.GatewayClass
	var dnsProviderSecret *corev1.Secret
	var testNamespace string
	var gateway *gatewayapiv1.Gateway
	var dnsPolicy *v1alpha1.DNSPolicy
	var recordName, wildcardRecordName string
	var domain = fmt.Sprintf("example-%s.com", rand.String(6))

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gatewayClass = tests.BuildGatewayClass("gwc-"+testNamespace, "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		dnsProviderSecret = tests.BuildInMemoryCredentialsSecret("inmemory-credentials", testNamespace, domain)
		Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		if gateway != nil {
			err := k8sClient.Delete(ctx, gateway)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if dnsPolicy != nil {
			err := k8sClient.Delete(ctx, dnsPolicy)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}

		//Ensure all dns records in the test namespace are deleted
		dnsRecords := &kuadrantdnsv1alpha1.DNSRecordList{}
		err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
		Expect(err).ToNot(HaveOccurred())
		for _, record := range dnsRecords.Items {
			err := k8sClient.Delete(ctx, &record, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}

		// Wait until dns records are finished deleting since it can't finish deleting without the DNS provider secret
		Eventually(func(g Gomega) {
			err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecords.Items).To(HaveLen(0))
		}).WithContext(ctx).Should(Succeed())

		if dnsProviderSecret != nil {
			err := k8sClient.Delete(ctx, dnsProviderSecret)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if gatewayClass != nil {
			err := k8sClient.Delete(ctx, gatewayClass)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	It("should validate loadBalancing field correctly", func(ctx SpecContext) {
		gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameOne, "").
			WithHTTPListener(tests.ListenerNameOne, tests.HostTwo(domain)).Gateway

		// simple should succeed
		dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway")
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

		// should not allow adding loadBalancing field value after creation
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
			g.Expect(err).NotTo(HaveOccurred())
			dnsPolicy.Spec.LoadBalancing = &v1alpha1.LoadBalancingSpec{
				Weight:     100,
				Geo:        "foo",
				DefaultGeo: false,
			}
			err = k8sClient.Update(ctx, dnsPolicy)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("loadBalancing is immutable")))
		}, tests.TimeoutMedium, time.Second).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dnsPolicy)).ToNot(HaveOccurred())

		// loadbalanced should succeed
		dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway").
			WithLoadBalancingFor(100, "foo", false)
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

		// should allow loadBalancing struct fields to be updated
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsPolicy.Spec.LoadBalancing).ToNot(BeNil())
			g.Expect(dnsPolicy.Spec.LoadBalancing.Geo).To(Equal("foo"))
			g.Expect(dnsPolicy.Spec.LoadBalancing.Weight).To(Equal(100))
			g.Expect(dnsPolicy.Spec.LoadBalancing.DefaultGeo).ToNot(BeTrue())
			dnsPolicy.Spec.LoadBalancing.Geo = "bar"
			dnsPolicy.Spec.LoadBalancing.Weight = 200
			dnsPolicy.Spec.LoadBalancing.DefaultGeo = true
			err = k8sClient.Update(ctx, dnsPolicy)
			g.Expect(err).To(Succeed())
		}, tests.TimeoutMedium, time.Second).Should(Succeed())

		// should not allow removing loadBalancing field value after creation
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
			g.Expect(err).NotTo(HaveOccurred())
			dnsPolicy.Spec.LoadBalancing = nil
			err = k8sClient.Update(ctx, dnsPolicy)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("loadBalancing is immutable")))
		}, tests.TimeoutMedium, time.Second).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dnsPolicy)).ToNot(HaveOccurred())
	}, testTimeOut)

	It("should validate provider ref field correctly", func(ctx SpecContext) {

		gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameOne, "").
			WithHTTPListener(tests.ListenerNameOne, tests.HostTwo(domain)).Gateway

		// should not allow an empty providerRef list
		dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
			WithTargetGateway("test-gateway")
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(MatchError(ContainSubstring("spec.providerRefs: Required value")))

		// should create with a single providerRef
		dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway")
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

		// should not allow adding another providerRef
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
			g.Expect(err).NotTo(HaveOccurred())
			dnsPolicy.Spec.ProviderRefs = append(dnsPolicy.Spec.ProviderRefs, kuadrantdnsv1alpha1.ProviderRef{
				Name: "some-other-provider-secret",
			})
			err = k8sClient.Update(ctx, dnsPolicy)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("spec.providerRefs: Too many: 2: must have at most 1 items")))
		}, tests.TimeoutMedium, time.Second).Should(Succeed())
	}, testTimeOut)

	It("should conflict DNS Policies of different strategy on the same host", func(ctx SpecContext) {

		// setting up two gateways that have the same host
		gateway1 := tests.NewGatewayBuilder("test-gateway1", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).Gateway
		Expect(k8sClient.Create(ctx, gateway1)).To(Succeed())

		gateway2 := tests.NewGatewayBuilder("test-gateway2", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameTwo, tests.HostOne(domain)).Gateway
		Expect(k8sClient.Create(ctx, gateway2)).To(Succeed())

		// update statuses of gateways - attach routes to the listeners and define an IP address
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway1), gateway1)).To(Succeed())
			gateway1.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
				{
					Type:  ptr.To(gatewayapiv1.IPAddressType),
					Value: tests.IPAddressOne,
				},
			}
			gateway1.Status.Listeners = []gatewayapiv1.ListenerStatus{
				{
					Name:           tests.ListenerNameOne,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
			}
			g.Expect(k8sClient.Status().Update(ctx, gateway1)).To(Succeed())

			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway2), gateway2)).To(Succeed())
			gateway2.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
				{
					Type:  ptr.To(gatewayapiv1.IPAddressType),
					Value: tests.IPAddressOne,
				},
			}
			gateway2.Status.Listeners = []gatewayapiv1.ListenerStatus{
				{
					Name:           tests.ListenerNameTwo,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
			}
			g.Expect(k8sClient.Status().Update(ctx, gateway2)).To(Succeed())
		}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

		// Create policy1 targeting gateway1 with simple routing strategy
		dnsPolicy1 := tests.NewDNSPolicy("test-dns-policy1", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway1")
		Expect(k8sClient.Create(ctx, dnsPolicy1)).To(Succeed())

		// the policy 1 should succeed
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy1), dnsPolicy1)).To(Succeed())

			g.Expect(dnsPolicy1.Status.Conditions).To(
				ContainElements(
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonAccepted)),
						"Message": Equal("DNSPolicy has been accepted"),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
						"Message": Equal("DNSPolicy has been successfully enforced"),
					})),
			)
			// long timeout in a separate assertion - this avoids the test from being flaky: sometimes policy needs more time to become enforced
		}, tests.TimeoutLong, tests.RetryIntervalMedium).Should(Succeed())

		// check back with gateway1 (target of the policy1) to ensure it is ready
		// also check that DNS Record was created and successful
		Eventually(func(g Gomega) {
			dnsRecord1 := &kuadrantdnsv1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway1-" + tests.ListenerNameOne,
					Namespace: testNamespace,
				},
			}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord1), dnsRecord1)).To(Succeed())
			g.Expect(dnsRecord1.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionTrue),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("Provider ensured the dns record"),
				})),
			)
		}, tests.TimeoutLong, tests.RetryIntervalMedium).Should(Succeed())

		// create policy2 targeting gateway2 with the load-balanced strategy
		dnsPolicy2 := tests.NewDNSPolicy("test-dns-policy2", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway2").
			WithLoadBalancingFor(100, "foo", false)
		Expect(k8sClient.Create(ctx, dnsPolicy2)).To(Succeed())

		errorMessage := "The DNS provider failed to ensure the record: record type conflict, " +
			"cannot update endpoint '" + tests.HostOne(domain) + "' with record type 'CNAME' when endpoint " +
			"already exists with record type 'A'"

		// policy2 should fail: dns provider already has a record for this host from the gateway1+policy1
		// gateway2+policy2 configured correctly, but conflict with existing records in the zone
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy2), dnsPolicy2)).To(Succeed())
			g.Expect(dnsPolicy2.Status.RecordConditions[tests.HostOne(domain)]).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal("ProviderError"),
					"Message": Equal(errorMessage),
				})),
			)
			// check that policy is not enforced with a correct message
			g.Expect(dnsPolicy2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal(string(kuadrant.PolicyReasonUnknown)),
					"Message": Equal("DNSPolicy has encountered some issues: policy is not enforced on any DNSRecord: not a single DNSRecord is ready"),
				})))
		}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

		// check that error is also displayed in the gateway
		Eventually(func(g Gomega) {
			dnsRecord2 := &kuadrantdnsv1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway2-" + tests.ListenerNameTwo,
					Namespace: testNamespace,
				},
			}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord2), dnsRecord2)).To(Succeed())
			g.Expect(dnsRecord2.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Reason":  Equal("ProviderError"),
					"Message": Equal(errorMessage),
				})),
			)
		}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

		// cleanup. Only needed for this one since we created atypical resources
		Eventually(func(g Gomega) {
			Expect(k8sClient.Delete(ctx, gateway1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, gateway2)).To(Succeed())

			Expect(k8sClient.Delete(ctx, dnsPolicy1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, dnsPolicy2)).To(Succeed())

			// wait for dns records to go before giving it to the AfterEach() call
			Eventually(func(g Gomega) {
				dnsRecords := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecords.Items).To(HaveLen(0))
			}).WithContext(ctx).Should(Succeed())
		}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())
	}, testTimeOut)

	Context("invalid target", func() {
		It("should have accepted condition with status false and correct reason", func(ctx SpecContext) {
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway")
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

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
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should have partially enforced policy if one of the records is not ready", func(ctx SpecContext) {
			// setting up two gateways that have the same host
			gateway1 := tests.NewGatewayBuilder("test-gateway1", gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).Gateway
			Expect(k8sClient.Create(ctx, gateway1)).To(Succeed())

			gateway2 := tests.NewGatewayBuilder("test-gateway2", gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				WithHTTPListener(tests.ListenerNameTwo, tests.HostTwo(domain)).Gateway
			Expect(k8sClient.Create(ctx, gateway2)).To(Succeed())

			// update statuses of gateways - attach routes to the listeners and define an IP address
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway1), gateway1)).To(Succeed())
				gateway1.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
				}
				gateway1.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway1)).To(Succeed())

				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway2), gateway2)).To(Succeed())
				gateway2.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
				}
				gateway2.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameTwo,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway2)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			// Create policy1 targeting gateway1 with simple routing strategy
			dnsPolicy1 := tests.NewDNSPolicy("test-dns-policy1", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway1")
			Expect(k8sClient.Create(ctx, dnsPolicy1)).To(Succeed())

			// policy1 should succeed
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy1), dnsPolicy1)).To(Succeed())
				// check that policy is enforced with a correct message
				g.Expect(dnsPolicy1.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
							"Message": Equal("DNSPolicy has been successfully enforced"),
						})))
			}, tests.TimeoutLong, tests.RetryIntervalMedium).Should(Succeed())

			// create policy2 targeting gateway2 with the load-balanced strategy
			dnsPolicy2 := tests.NewDNSPolicy("test-dns-policy2", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway2").
				WithLoadBalancingFor(100, "foo", false)
			Expect(k8sClient.Create(ctx, dnsPolicy2)).To(Succeed())

			// policy2 should fail: dns provider already has a record for this host from the gateway1+policy1
			// gateway2+policy2 configured correctly, but conflict with existing records in the zone
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy2), dnsPolicy2)).To(Succeed())
				// check that policy is not enforced with a correct message
				g.Expect(dnsPolicy2.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
						"Message": Equal("DNSPolicy has been partially enforced. Not ready DNSRecords are: test-gateway2-test-listener-1 "),
					})))
			}, tests.TimeoutLong, tests.RetryIntervalMedium).Should(Succeed())

			// cleanup. Only needed for this one since we created atypical resources
			Eventually(func(g Gomega) {
				Expect(k8sClient.Delete(ctx, gateway1)).To(Succeed())
				Expect(k8sClient.Delete(ctx, gateway2)).To(Succeed())

				Expect(k8sClient.Delete(ctx, dnsPolicy1)).To(Succeed())
				Expect(k8sClient.Delete(ctx, dnsPolicy2)).To(Succeed())

				// wait for dns records to go before giving it to the AfterEach() call
				Eventually(func(g Gomega) {
					dnsRecords := &kuadrantdnsv1alpha1.DNSRecordList{}
					err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(dnsRecords.Items).To(HaveLen(0))
				}).WithContext(ctx).Should(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())
		}, testTimeOut)

	})

	Context("valid target with no gateway status", func() {
		testGatewayName := "test-no-gateway-status"

		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder(testGatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostTwo(domain)).
				Gateway
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(testGatewayName)

			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should not create a dns record", func(ctx SpecContext) {
			Consistently(func() []kuadrantdnsv1alpha1.DNSRecord { // DNS record exists
				dnsRecords := kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, &dnsRecords, client.InNamespace(dnsPolicy.GetNamespace()))
				Expect(err).ToNot(HaveOccurred())
				return dnsRecords.Items
			}, time.Second*15, time.Second).Should(BeEmpty())
		}, testTimeOut)

		It("should have accepted and enforced status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				fmt.Println("conditions ", dnsPolicy.Status.Conditions)
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
							"Message": ContainSubstring("DNSPolicy has been successfully enforced : no DNSRecords created based on policy and gateway configuration : no valid status addresses to use on gateway"),
						})),
				)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)
	})

	Context("valid target and valid gateway status", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder(tests.GatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				WithHTTPListener(tests.ListenerNameWildcard, tests.HostWildcard(domain)).
				Gateway
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(tests.GatewayName)

			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			recordName = fmt.Sprintf("%s-%s", tests.GatewayName, tests.ListenerNameOne)
			wildcardRecordName = fmt.Sprintf("%s-%s", tests.GatewayName, tests.ListenerNameWildcard)
		})

		It("should create dns records and have correct policy status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				//Check records
				recordList := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, recordList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(recordList.Items).To(HaveLen(2))

				dnsRecord := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, dnsRecord)).To(Succeed())

				wildcardDnsRecord := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, wildcardDnsRecord)).To(Succeed())

				//Check policy status
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
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
							"Status":  Equal(metav1.ConditionTrue),
							"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
							"Message": Equal("DNSPolicy has been successfully enforced"),
						})),
				)
				g.Expect(dnsPolicy.Status.TotalRecords).To(Equal(int32(2)))
			}, tests.TimeoutLong, tests.RetryIntervalMedium, ctx).Should(Succeed())
		}, testTimeOut)

		It("should remove dns records when listener removed", func(ctx SpecContext) {
			Eventually(func(g Gomega) { // DNS records(s) exist
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			//get the gateway and remove the listeners
			By("removing listener from gateway")
			Eventually(func() error {
				existingGateway := &gatewayapiv1.Gateway{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway); err != nil {
					return err
				}
				newListeners := []gatewayapiv1.Listener{}
				for _, existing := range existingGateway.Spec.Listeners {
					if existing.Name == tests.ListenerNameWildcard {
						newListeners = append(newListeners, existing)
					}
				}

				patch := client.MergeFrom(existingGateway.DeepCopy())
				existingGateway.Spec.Listeners = newListeners
				return k8sClient.Patch(ctx, existingGateway, patch)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			Eventually(func(g Gomega) { // DNS record should be removed for non wildcard
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())

				//Check policy status
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status": Equal(metav1.ConditionTrue),
						}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrant.PolicyConditionEnforced)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				g.Expect(dnsPolicy.Status.TotalRecords).To(Equal(int32(1)))
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should remove dns records and gateway back reference on policy deletion", func(ctx SpecContext) {
			Eventually(func(g Gomega) { // DNS records(s) exist and gateway back references set
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())

				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			By("deleting the dns policy")
			Expect(k8sClient.Delete(ctx, dnsPolicy)).To(Succeed())

			Eventually(func(g Gomega) { // DNS records(s) do not exist and gateway back references removed
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))

				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should remove dns records on gateway deletion", func(ctx SpecContext) {
			Eventually(func(g Gomega) { // DNS records(s) exist
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			By("deleting the gateway")
			Expect(k8sClient.Delete(ctx, gateway)).To(Succeed())

			Eventually(func(g Gomega) { // DNS records(s) do not exist
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))

				//Check policy status
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ConsistOf(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal("TargetNotFound"),
						}),
					),
				)
				//ToDo (mnairn) Theres a bug here, this never gets reset when all records are removed due to target removal
				//g.Expect(dnsPolicy.Status.TotalRecords).To(Equal(int32(0)))
			}, tests.TimeoutLong, tests.RetryIntervalMedium).Should(Succeed())
		}, testTimeOut)

		It("should remove dns records and gateway back reference on policy target ref change [invalid target]", func(ctx SpecContext) {
			Eventually(func(g Gomega) { // DNS records(s) exist
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())

				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			By("changing the policy target ref")
			Eventually(func() error {
				existingDNSpolicy := &v1alpha1.DNSPolicy{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), existingDNSpolicy); err != nil {
					return err
				}
				patch := client.MergeFrom(existingDNSpolicy.DeepCopy())
				existingDNSpolicy.Spec.TargetRef.Name = "doesnotexist"
				return k8sClient.Patch(ctx, existingDNSpolicy, patch)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			Eventually(func(g Gomega) { // DNS records(s) do not exist and gateway back references removed
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))

				//ToDo (mnairn) Theres a bug here, the old gateway target never has these annotations removed
				//err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				//g.Expect(err).NotTo(HaveOccurred())
				//g.Expect(gateway.Annotations).ToNot(HaveKey(v1alpha1.DNSPolicyDirectReferenceAnnotationName))
				//g.Expect(gateway.Annotations).ToNot(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))

				//Check policy status
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ConsistOf(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status": Equal(metav1.ConditionFalse),
							"Reason": Equal("TargetNotFound"),
						}),
					),
				)
				//ToDo (mnairn) Theres a bug here, this never gets reset when all records are removed due to target removal
				//g.Expect(dnsPolicy.Status.TotalRecords).To(Equal(int32(0)))
			}, tests.TimeoutLong, time.Second).Should(Succeed())

		}, testTimeOut)

		It("should remove dns records and gateway back reference on policy target ref change [valid target]", func(ctx SpecContext) {
			Eventually(func(g Gomega) { // DNS records(s) exist
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).To(Succeed())

				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			testGateway2Name := "test-gateway-2"
			record2Name := fmt.Sprintf("%s-%s", testGateway2Name, tests.ListenerNameOne)
			gateway2 := tests.NewGatewayBuilder(testGateway2Name, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				Gateway

			By("creating second gateway")
			Expect(k8sClient.Create(ctx, gateway2)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway2), gateway2)).To(Succeed())
				gateway2.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type: ptr.To(gatewayapiv1.IPAddressType),
						//Using the same address can cause a panic in the inmemory provider see https://github.com/Kuadrant/dns-operator/issues/272
						Value: "172.1.1.1",
					},
				}
				gateway2.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway2)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			By("changing the policy target ref")
			Eventually(func() error {
				existingDNSpolicy := &v1alpha1.DNSPolicy{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), existingDNSpolicy); err != nil {
					return err
				}
				patch := client.MergeFrom(existingDNSpolicy.DeepCopy())
				existingDNSpolicy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(testGateway2Name)
				return k8sClient.Patch(ctx, existingDNSpolicy, patch)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			Eventually(func(g Gomega) { // DNS records(s) do not exist and gateway back references removed
				// New dns record exists and old ones removed
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: record2Name, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})).Should(MatchError(ContainSubstring("not found")))

				//Check policy status
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
							"Status": Equal(metav1.ConditionTrue),
						}),
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrant.PolicyConditionEnforced)),
							"Status": Equal(metav1.ConditionTrue),
						}),
					),
				)
				g.Expect(dnsPolicy.Status.TotalRecords).To(Equal(int32(1)))
			}, tests.TimeoutLong, time.Second).Should(Succeed())

		})

		It("should re-create dns record when listener hostname changes", func(ctx SpecContext) {
			//get the current dnsrecord and wildcard dnsrecord
			currentRec := &kuadrantdnsv1alpha1.DNSRecord{}
			currentWildcardRec := &kuadrantdnsv1alpha1.DNSRecord{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, currentRec)).To(Succeed())
				g.Expect(currentRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, currentWildcardRec)).To(Succeed())
				g.Expect(currentRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
			}, tests.TimeoutLong, time.Second).Should(BeNil())

			//get the gateway and change the hostname of the listener that corresponds to the dnsrecord
			newHostname := gatewayapiv1.Hostname(tests.HostTwo(domain))
			Eventually(func() error {
				existingGateway := &gatewayapiv1.Gateway{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway); err != nil {
					return err
				}
				newListeners := []gatewayapiv1.Listener{}
				for _, existing := range existingGateway.Spec.Listeners {
					if existing.Name == tests.ListenerNameOne {
						existing.Hostname = &newHostname
					}
					newListeners = append(newListeners, existing)
				}
				patch := client.MergeFrom(existingGateway.DeepCopy())
				existingGateway.Spec.Listeners = newListeners
				return k8sClient.Patch(ctx, existingGateway, patch)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			//get the dnsrecord again and verify it's no longer the same DNSRecord resource and the rootHost has changed
			//get the wildcard dnsrecord again and verify the DNSRecord resource is unchanged
			Eventually(func(g Gomega) {
				newRec := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, newRec)).To(Succeed())
				g.Expect(newRec.Spec.RootHost).To(Equal(string(newHostname)))
				g.Expect(newRec.Spec.RootHost).ToNot(Equal(currentRec.Spec.RootHost))
				g.Expect(newRec.UID).ToNot(Equal(currentRec.UID))
				g.Expect(newRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				newWildcardRec := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, newWildcardRec)).To(Succeed())
				g.Expect(newWildcardRec.Spec.RootHost).To(Equal(currentWildcardRec.Spec.RootHost))
				g.Expect(newWildcardRec.UID).To(Equal(currentWildcardRec.UID))
				g.Expect(newWildcardRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				currentRec = newRec
				currentWildcardRec = newWildcardRec
			}, tests.TimeoutLong, time.Second).Should(BeNil())

			//get the gateway and change the hostname of the listener that corresponds to the wildcard dnsrecord
			newWildcardHostname := gatewayapiv1.Hostname(tests.HostWildcard(tests.HostTwo(domain)))
			Eventually(func() error {
				existingGateway := &gatewayapiv1.Gateway{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway); err != nil {
					return err
				}
				newListeners := []gatewayapiv1.Listener{}
				for _, existing := range existingGateway.Spec.Listeners {
					if existing.Name == tests.ListenerNameWildcard {
						existing.Hostname = &newWildcardHostname
					}
					newListeners = append(newListeners, existing)
				}
				patch := client.MergeFrom(existingGateway.DeepCopy())
				existingGateway.Spec.Listeners = newListeners
				return k8sClient.Patch(ctx, existingGateway, patch)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			//get the dnsrecord again and verify the DNSRecord resource is unchanged
			//get the wildcard dnsrecord again and verify it's no longer the same DNSRecord resource and the rootHost has changed
			Eventually(func(g Gomega) {
				newRec := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, newRec)).To(Succeed())
				g.Expect(newRec.Spec.RootHost).To(Equal(currentRec.Spec.RootHost))
				g.Expect(newRec.UID).To(Equal(currentRec.UID))
				g.Expect(newRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				newWildcardRec := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, newWildcardRec)).To(Succeed())
				g.Expect(newWildcardRec.Spec.RootHost).To(Equal(string(newWildcardHostname)))
				g.Expect(newWildcardRec.Spec.RootHost).ToNot(Equal(currentWildcardRec.Spec.RootHost))
				g.Expect(newWildcardRec.UID).ToNot(Equal(currentWildcardRec.UID))
				g.Expect(newWildcardRec.Status.Conditions).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"Type":   Equal(string(kuadrantdnsv1alpha1.ConditionTypeReady)),
							"Status": Equal(metav1.ConditionTrue),
						})),
				)
				currentRec = newRec
				currentWildcardRec = newWildcardRec
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)

	})

	Context("cel validation", func() {
		It("should error targeting invalid group", func(ctx SpecContext) {
			p := tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("gateway")
			p.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"))
		}, testTimeOut)

		It("should error targeting invalid kind", func(ctx SpecContext) {
			p := tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("gateway")
			p.Spec.TargetRef.Kind = "TCPRoute"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.kind. The only supported values are 'Gateway'"))
		}, testTimeOut)
	})

	Context("no attached routes to listeners", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder(tests.GatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				WithHTTPListener(tests.ListenerNameWildcard, tests.HostWildcard(domain)).
				Gateway
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(tests.GatewayName)
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 0,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 0,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

		})

		It("should have an accepted and enforced policy with additional context", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Message": ContainSubstring("DNSPolicy has been accepted"),
					})),
				)

				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Message": ContainSubstring("DNSPolicy has been successfully enforced : no DNSRecords created based on policy and gateway configuration : no routes attached to any gateway listeners"),
					})),
				)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		})

	})

	Context("excludeAddresses from DNS", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder(tests.GatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				WithHTTPListener(tests.ListenerNameWildcard, tests.HostWildcard(domain)).
				Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
		})
		It("should create a DNSPolicy with an invalid CIDR", func(ctx SpecContext) {
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(gateway.Name).
				WithExcludeAddresses([]string{"1.1.1.1/345"})
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Message": ContainSubstring("could not parse the CIDR from the excludeAddresses field"),
					})),
				)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

		})

		It("should create a DNSPolicy valid exclude addresses", func(ctx SpecContext) {
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(gateway.Name).
				WithExcludeAddresses([]string{tests.IPAddressOne})
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status": Equal(metav1.ConditionTrue),
					})),
				)
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
						"Message": Equal("DNSPolicy has been successfully enforced"),
					})),
				)
				recordName = fmt.Sprintf("%s-%s", tests.GatewayName, tests.ListenerNameOne)
				rec := &kuadrantdnsv1alpha1.DNSRecord{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, rec)).To(Succeed())
				foundExcluded := false
				foundAllowed := false
				for _, ep := range rec.Spec.Endpoints {
					for _, t := range ep.Targets {
						if t == tests.IPAddressOne {
							foundExcluded = true
						}
						if t == tests.IPAddressTwo {
							foundAllowed = true
						}
					}
				}
				g.Expect(foundExcluded).To(BeFalse())
				g.Expect(foundAllowed).To(BeTrue())
				g.Expect(len(gateway.Status.Listeners)).To(Equal(int(dnsPolicy.Status.TotalRecords)))

			}, tests.TimeoutLong, time.Second).Should(Succeed())

		})
		It("should not create a DNSRecords if no endpoints due to DNSPolicy exclude addresses", func(ctx SpecContext) {
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(gateway.Name).
				WithExcludeAddresses([]string{tests.IPAddressOne, tests.IPAddressTwo})
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressOne,
					},
					{
						Type:  ptr.To(gatewayapiv1.IPAddressType),
						Value: tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

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
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
						"Message": ContainSubstring("DNSPolicy has been successfully enforced : no DNSRecords created based on policy and gateway configuration : no valid status addresses to use on gateway"),
					})),
				)
				g.Expect(int(dnsPolicy.Status.TotalRecords)).To(Equal(0))
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())
		})
	})
})
