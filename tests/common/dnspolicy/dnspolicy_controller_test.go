//go:build integration

package dnspolicy

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/rand"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/multicluster"
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
			// Wait until dns records are finished deleting since it can't finish deleting without the DNS provider secret
			Eventually(func(g Gomega) {
				dnsRecords := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, dnsRecords, client.InNamespace(testNamespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsRecords.Items).To(HaveLen(0))
			}).WithContext(ctx).Should(Succeed())
		}
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

	It("should validate routing strategy field correctly", func(ctx SpecContext) {

		gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameOne, tests.HostTwo(domain)).Gateway

		// simple should succeed
		dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway").
			WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

		// should not allow changing routing strategy
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
			g.Expect(err).NotTo(HaveOccurred())
			dnsPolicy.Spec.RoutingStrategy = v1alpha1.LoadBalancedRoutingStrategy
			err = k8sClient.Update(ctx, dnsPolicy)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(MatchError(ContainSubstring("RoutingStrategy is immutable")))
		}, tests.TimeoutMedium, time.Second).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dnsPolicy)).ToNot(HaveOccurred())

		// loadbalanced missing loadbalancing field
		dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway").
			WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy)
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(MatchError(ContainSubstring("spec.loadBalancing is a required field when spec.routingStrategy == 'loadbalanced'")))

		// loadbalanced should succeed
		dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway").
			WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
			WithLoadBalancingFor(100, nil, "foo")
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
	}, testTimeOut)

	It("should validate provider ref field correctly", func(ctx SpecContext) {

		gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
			WithHTTPListener(tests.ListenerNameOne, tests.HostTwo(domain)).Gateway

		// should not allow an empty providerRef list
		dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
			WithTargetGateway("test-gateway").
			WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
		Expect(k8sClient.Create(ctx, dnsPolicy)).To(MatchError(ContainSubstring("spec.providerRefs: Required value")))

		// should create with a single providerRef
		dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway").
			WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
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
		dnsPolicy1 := v1alpha1.NewDNSPolicy("test-dns-policy1", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway1").
			WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
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
		dnsPolicy2 := v1alpha1.NewDNSPolicy("test-dns-policy2", testNamespace).
			WithProviderSecret(*dnsProviderSecret).
			WithTargetGateway("test-gateway2").
			WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
			WithLoadBalancingFor(100, nil, "foo")
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
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway").
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
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
			dnsPolicy1 := v1alpha1.NewDNSPolicy("test-dns-policy1", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway1").
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			Expect(k8sClient.Create(ctx, dnsPolicy1)).To(Succeed())

			// create policy2 targeting gateway2 with the load-balanced strategy
			dnsPolicy2 := v1alpha1.NewDNSPolicy("test-dns-policy2", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("test-gateway2").
				WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
				WithLoadBalancingFor(100, nil, "foo")
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
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(testGatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)

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

		It("should have accepted and not enforced status", func(ctx SpecContext) {
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
							"Reason":  Equal(string(kuadrant.PolicyReasonUnknown)),
							"Message": Equal("DNSPolicy has encountered some issues: policy is not enforced on any DNSRecord: no routes attached for listeners"),
						})),
				)
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should set gateway back reference", func(ctx SpecContext) {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				gw := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: gateway.Name, Namespace: testNamespace}, gw)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)
	})

	Context("valid target and valid gateway status", func() {

		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder(tests.GatewayName, gatewayClass.Name, testNamespace).
				WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
				WithHTTPListener(tests.ListenerNameWildcard, tests.HostWildcard(domain)).
				Gateway
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(tests.GatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)

			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				Expect(err).ShouldNot(HaveOccurred())

				gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
					{
						Type:  ptr.To(multicluster.MultiClusterIPAddressType),
						Value: tests.ClusterNameOne + "/" + tests.IPAddressOne,
					},
					{
						Type:  ptr.To(multicluster.MultiClusterIPAddressType),
						Value: tests.ClusterNameTwo + "/" + tests.IPAddressTwo,
					},
				}
				gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
					{
						Name:           tests.ClusterNameOne + "." + tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ClusterNameTwo + "." + tests.ListenerNameOne,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ClusterNameOne + "." + tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
					{
						Name:           tests.ClusterNameTwo + "." + tests.ListenerNameWildcard,
						SupportedKinds: []gatewayapiv1.RouteGroupKind{},
						AttachedRoutes: 1,
						Conditions:     []metav1.Condition{},
					},
				}
				return k8sClient.Status().Update(ctx, gateway)
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).ShouldNot(HaveOccurred())

			recordName = fmt.Sprintf("%s-%s", tests.GatewayName, tests.ListenerNameOne)
			wildcardRecordName = fmt.Sprintf("%s-%s", tests.GatewayName, tests.ListenerNameWildcard)
		})

		It("should have correct status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Finalizers).To(ContainElement(controllers.DNSPolicyFinalizer))
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
			}, tests.TimeoutLong, time.Second).Should(Succeed())

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
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should set gateway back reference", func(ctx SpecContext) {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should remove dns records when listener removed", func(ctx SpecContext) {
			//get the gateway and remove the listeners

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
		}, testTimeOut)

		It("should remove gateway back reference on policy deletion", func(ctx SpecContext) {
			policyBackRefValue := testNamespace + "/" + dnsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: dnsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gateway.Annotations).To(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))

				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dnsPolicy.Finalizers).To(ContainElement(controllers.DNSPolicyFinalizer))
			}, tests.TimeoutMedium, time.Second).Should(Succeed())

			By("deleting the dns policy")
			Expect(k8sClient.Delete(ctx, dnsPolicy)).To(BeNil())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gateway.Annotations).ToNot(HaveKey(v1alpha1.DNSPolicyDirectReferenceAnnotationName))
				g.Expect(gateway.Annotations).ToNot(HaveKeyWithValue(v1alpha1.DNSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, tests.TimeoutMedium, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should remove dns record reference on policy deletion even if gateway is removed", func(ctx SpecContext) {

			Eventually(func() error { // DNS record exists
				return k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})
			}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())

			err := k8sClient.Delete(ctx, gateway)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			err = k8sClient.Delete(ctx, dnsPolicy)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

			//ToDo We cant assume that a dnsrecord reconciler is running that will remove the record or that it will be removed
			// It's not the responsibility of this operator to do this, so we should just check if it's gone
			// (in case we are running on a cluster that actually has a dnsrecord reconciler running), or that it is marked for deletion
			//Eventually(func() error { // DNS record removed
			//	return k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, &kuadrantdnsv1alpha1.DNSRecord{})
			//}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(MatchError(ContainSubstring("not found")))

		}, testTimeOut)
	})

	Context("cel validation", func() {
		It("should error targeting invalid group", func(ctx SpecContext) {
			p := v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("gateway").
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			p.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"))
		}, testTimeOut)

		It("should error targeting invalid kind", func(ctx SpecContext) {
			p := v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway("gateway").
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			p.Spec.TargetRef.Kind = "TCPRoute"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.kind. The only supported values are 'Gateway'"))
		}, testTimeOut)
	})
})
