//go:build integration

package dnspolicy

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/test"
)

var _ = Describe("DNSPolicy Single Cluster", func() {

	var gatewayClass *gatewayapiv1.GatewayClass
	var managedZone *kuadrantdnsv1alpha1.ManagedZone
	var testNamespace string
	var gateway *gatewayapiv1.Gateway
	var dnsPolicy *v1alpha1.DNSPolicy
	var ownerID, clusterHash, gwHash, recordName, wildcardRecordName string
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = test.CreateNamespaceWithContext(ctx, k8sClient)

		var err error
		clusterUID, err := utils.GetClusterUID(ctx, k8sClient)
		Expect(err).To(BeNil())

		gatewayClass = test.BuildGatewayClass("gwc-"+testNamespace, "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		managedZone = test.BuildManagedZone("mz-example-com", testNamespace, "example.com")
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())

		gateway = test.NewGatewayBuilder(test.GatewayName, gatewayClass.Name, testNamespace).
			WithHTTPListener("foo", "foo.example.com").
			WithHTTPListener(test.ListenerNameOne, test.HostOne).
			WithHTTPListener(test.ListenerNameWildcard, test.HostWildcard).
			Gateway
		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

		clusterHash = common.ToBase36HashLen(clusterUID, utils.ClusterIDLength)
		ownerID = common.ToBase36HashLen(fmt.Sprintf("%s-%s-%s", clusterUID, gateway.Name, gateway.Namespace), utils.ClusterIDLength)

		gwHash = common.ToBase36HashLen(gateway.Name+"-"+gateway.Namespace, 6)

		//Set single cluster gateway status
		Eventually(func() error {
			gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
				{
					Type:  ptr.To(gatewayapiv1.IPAddressType),
					Value: test.IPAddressOne,
				},
				{
					Type:  ptr.To(gatewayapiv1.IPAddressType),
					Value: test.IPAddressTwo,
				},
			}
			gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
				{
					Name:           "foo",
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 0,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           test.ListenerNameOne,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           test.ListenerNameWildcard,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
			}
			return k8sClient.Status().Update(ctx, gateway)
		}, test.TimeoutMedium, test.RetryIntervalMedium).Should(Succeed())

		recordName = fmt.Sprintf("%s-%s", test.GatewayName, test.ListenerNameOne)
		wildcardRecordName = fmt.Sprintf("%s-%s", test.GatewayName, test.ListenerNameWildcard)
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
		test.DeleteNamespaceCallbackWithContext(ctx, k8sClient, testNamespace)
	})

	Context("simple routing strategy", func() {

		BeforeEach(func() {
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(test.GatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should create dns records", func() {

			Eventually(func(g Gomega, ctx context.Context) {

				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)).To(Succeed())
				g.Expect(dnsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyReasonEnforced)),
						"Message": Equal("DNSPolicy has been partially enforced"),
					})),
				)

			}, test.TimeoutMedium, test.RetryIntervalMedium, ctx).Should(Succeed())

			Eventually(func(g Gomega, ctx context.Context) {
				recordList := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, recordList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(recordList.Items).To(HaveLen(2))
				g.Expect(recordList.Items).To(
					ContainElements(
						MatchFields(IgnoreExtras, Fields{
							"ObjectMeta": HaveField("Name", recordName),
							"Spec": MatchFields(IgnoreExtras, Fields{
								"OwnerID":        Equal(&ownerID),
								"ManagedZoneRef": HaveField("Name", "mz-example-com"),
								"Endpoints": ConsistOf(
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(test.HostOne),
										"Targets":       ContainElements(test.IPAddressOne, test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
								),
							}),
						}),
						MatchFields(IgnoreExtras, Fields{
							"ObjectMeta": HaveField("Name", wildcardRecordName),
							"Spec": MatchFields(IgnoreExtras, Fields{
								"OwnerID":        Equal(&ownerID),
								"ManagedZoneRef": HaveField("Name", "mz-example-com"),
								"Endpoints": ConsistOf(
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(test.HostWildcard),
										"Targets":       ContainElements(test.IPAddressOne, test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
								),
							}),
						}),
					))
			}, test.TimeoutMedium, test.RetryIntervalMedium, ctx).Should(Succeed())
		})

	})

	Context("loadbalanced routing strategy", func() {

		BeforeEach(func() {
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(test.GatewayName).
				WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
				WithLoadBalancingFor(120, nil, "IE")
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should create dns records", func() {
			Eventually(func(g Gomega, ctx context.Context) {
				recordList := &kuadrantdnsv1alpha1.DNSRecordList{}
				err := k8sClient.List(ctx, recordList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(recordList.Items).To(HaveLen(2))

				dnsRecord := &kuadrantdnsv1alpha1.DNSRecord{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: recordName, Namespace: testNamespace}, dnsRecord)
				g.Expect(err).NotTo(HaveOccurred())

				wildcardDnsRecord := &kuadrantdnsv1alpha1.DNSRecord{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: wildcardRecordName, Namespace: testNamespace}, wildcardDnsRecord)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(*dnsRecord).To(
					MatchFields(IgnoreExtras, Fields{
						"ObjectMeta": HaveField("Name", recordName),
						"Spec": MatchFields(IgnoreExtras, Fields{
							"OwnerID":        Equal(&ownerID),
							"ManagedZoneRef": HaveField("Name", "mz-example-com"),
							"Endpoints": ConsistOf(
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test.example.com"),
									"Targets":       ConsistOf(test.IPAddressOne, test.IPAddressTwo),
									"RecordType":    Equal("A"),
									"SetIdentifier": Equal(""),
									"RecordTTL":     Equal(externaldns.TTL(60)),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("ie.klb.test.example.com"),
									"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test.example.com"),
									"RecordTTL":        Equal(externaldns.TTL(60)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("klb.test.example.com"),
									"Targets":          ConsistOf("ie.klb.test.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal("IE"),
									"RecordTTL":        Equal(externaldns.TTL(300)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("klb.test.example.com"),
									"Targets":          ConsistOf("ie.klb.test.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal("default"),
									"RecordTTL":        Equal(externaldns.TTL(300)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":       Equal(test.HostOne),
									"Targets":       ConsistOf("klb.test.example.com"),
									"RecordType":    Equal("CNAME"),
									"SetIdentifier": Equal(""),
									"RecordTTL":     Equal(externaldns.TTL(300)),
								})),
							),
						}),
					}),
				)
				g.Expect(*wildcardDnsRecord).To(
					MatchFields(IgnoreExtras, Fields{
						"ObjectMeta": HaveField("Name", wildcardRecordName),
						"Spec": MatchFields(IgnoreExtras, Fields{
							"OwnerID":        Equal(&ownerID),
							"ManagedZoneRef": HaveField("Name", "mz-example-com"),
							"Endpoints": ContainElements(
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.example.com"),
									"Targets":       ConsistOf(test.IPAddressOne, test.IPAddressTwo),
									"RecordType":    Equal("A"),
									"SetIdentifier": Equal(""),
									"RecordTTL":     Equal(externaldns.TTL(60)),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("ie.klb.example.com"),
									"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.example.com"),
									"RecordTTL":        Equal(externaldns.TTL(60)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("klb.example.com"),
									"Targets":          ConsistOf("ie.klb.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal("default"),
									"RecordTTL":        Equal(externaldns.TTL(300)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":          Equal("klb.example.com"),
									"Targets":          ConsistOf("ie.klb.example.com"),
									"RecordType":       Equal("CNAME"),
									"SetIdentifier":    Equal("IE"),
									"RecordTTL":        Equal(externaldns.TTL(300)),
									"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
								})),
								PointTo(MatchFields(IgnoreExtras, Fields{
									"DNSName":       Equal(test.HostWildcard),
									"Targets":       ConsistOf("klb.example.com"),
									"RecordType":    Equal("CNAME"),
									"SetIdentifier": Equal(""),
									"RecordTTL":     Equal(externaldns.TTL(300)),
								})),
							),
						}),
					}),
				)
			}, test.TimeoutMedium, test.RetryIntervalMedium, ctx).Should(Succeed())
		})

	})
})
