//go:build integration

package controllers

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
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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
		CreateNamespaceWithContext(ctx, &testNamespace)

		var err error
		clusterUID, err := utils.GetClusterUID(ctx, k8sClient)
		Expect(err).To(BeNil())

		gatewayClass = testBuildGatewayClass("foo", "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		managedZone = testBuildManagedZone("mz-example-com", testNamespace, "example.com")
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())

		gateway = NewGatewayBuilder(TestGatewayName, gatewayClass.Name, testNamespace).
			WithHTTPListener(TestListenerNameOne, TestHostOne).
			WithHTTPListener(TestListenerNameWildcard, TestHostWildcard).
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
					Value: TestIPAddressOne,
				},
				{
					Type:  ptr.To(gatewayapiv1.IPAddressType),
					Value: TestIPAddressTwo,
				},
			}
			gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
				{
					Name:           TestListenerNameOne,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           TestListenerNameWildcard,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
			}
			return k8sClient.Status().Update(ctx, gateway)
		}, TestTimeoutMedium, TestRetryIntervalMedium).Should(Succeed())

		recordName = fmt.Sprintf("%s-%s", TestGatewayName, TestListenerNameOne)
		wildcardRecordName = fmt.Sprintf("%s-%s", TestGatewayName, TestListenerNameWildcard)
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
		DeleteNamespaceCallbackWithContext(ctx, &testNamespace)
	})

	Context("simple routing strategy", func() {

		BeforeEach(func() {
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(TestGatewayName).
				WithRoutingStrategy(v1alpha1.SimpleRoutingStrategy)
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should create dns records", func() {
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
										"DNSName":       Equal(TestHostOne),
										"Targets":       ContainElements(TestIPAddressOne, TestIPAddressTwo),
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
										"DNSName":       Equal(TestHostWildcard),
										"Targets":       ContainElements(TestIPAddressOne, TestIPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
								),
							}),
						}),
					))
			}, TestTimeoutMedium, TestRetryIntervalMedium, ctx).Should(Succeed())
		})

	})

	Context("loadbalanced routing strategy", func() {

		BeforeEach(func() {
			dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
				WithTargetGateway(TestGatewayName).
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
									"Targets":       ConsistOf(TestIPAddressOne, TestIPAddressTwo),
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
									"DNSName":       Equal(TestHostOne),
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
									"Targets":       ConsistOf(TestIPAddressOne, TestIPAddressTwo),
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
									"DNSName":       Equal(TestHostWildcard),
									"Targets":       ConsistOf("klb.example.com"),
									"RecordType":    Equal("CNAME"),
									"SetIdentifier": Equal(""),
									"RecordTTL":     Equal(externaldns.TTL(300)),
								})),
							),
						}),
					}),
				)
			}, TestTimeoutMedium, TestRetryIntervalMedium, ctx).Should(Succeed())
		})

	})
})
