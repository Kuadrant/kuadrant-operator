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
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/multicluster"
	"github.com/kuadrant/kuadrant-operator/test"
)

var _ = Describe("DNSPolicy Multi Cluster", func() {

	var gatewayClass *gatewayapiv1.GatewayClass
	var managedZone *kuadrantdnsv1alpha1.ManagedZone
	var testNamespace string
	var gateway *gatewayapiv1.Gateway
	var dnsPolicy *v1alpha1.DNSPolicy
	var ownerID, recordName, wildcardRecordName, clusterTwoIDHash, clusterOneIDHash, gwHash string
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = test.CreateNamespaceWithContext(ctx, k8sClient)

		var err error
		ownerID, err = utils.GetClusterUID(ctx, k8sClient)
		Expect(err).To(BeNil())

		gatewayClass = test.BuildGatewayClass("gwc-"+testNamespace, "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		managedZone = test.BuildManagedZone("mz-example-com", testNamespace, "example.com")
		Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())

		gateway = test.NewGatewayBuilder(test.GatewayName, gatewayClass.Name, testNamespace).
			WithHTTPListener(test.ListenerNameOne, test.HostOne).
			WithHTTPListener(test.ListenerNameWildcard, test.HostWildcard).
			Gateway
		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

		ownerID = common.ToBase36HashLen(fmt.Sprintf("%s-%s-%s", ownerID, gateway.Name, gateway.Namespace), utils.ClusterIDLength)

		clusterOneIDHash = common.ToBase36HashLen(test.ClusterNameOne, utils.ClusterIDLength)
		clusterTwoIDHash = common.ToBase36HashLen(test.ClusterNameTwo, utils.ClusterIDLength)

		gwHash = common.ToBase36HashLen(gateway.Name+"-"+gateway.Namespace, 6)

		//Set multi cluster gateway status
		Eventually(func() error {
			gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
				{
					Type:  ptr.To(multicluster.MultiClusterIPAddressType),
					Value: test.ClusterNameOne + "/" + test.IPAddressOne,
				},
				{
					Type:  ptr.To(multicluster.MultiClusterIPAddressType),
					Value: test.ClusterNameTwo + "/" + test.IPAddressTwo,
				},
			}
			gateway.Status.Listeners = []gatewayapiv1.ListenerStatus{
				{
					Name:           test.ClusterNameOne + "." + test.ListenerNameOne,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           test.ClusterNameTwo + "." + test.ListenerNameOne,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           test.ClusterNameOne + "." + test.ListenerNameWildcard,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
				{
					Name:           test.ClusterNameTwo + "." + test.ListenerNameWildcard,
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 1,
					Conditions:     []metav1.Condition{},
				},
			}
			return k8sClient.Status().Update(ctx, gateway)
		}, test.TimeoutMedium, test.RetryIntervalMedium).ShouldNot(HaveOccurred())

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

		Context("geo+weighted with matching default geo", func() {

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
										"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"Targets":       ConsistOf(test.IPAddressOne),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.test.example.com"),
										"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.test.example.com"),
										"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"Targets":       ConsistOf(test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
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
								"Endpoints": ConsistOf(
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"Targets":       ConsistOf(test.IPAddressOne),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.example.com"),
										"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.example.com"),
										"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"Targets":       ConsistOf(test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
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
										"DNSName":          Equal("klb.example.com"),
										"Targets":          ConsistOf("ie.klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal("default"),
										"RecordTTL":        Equal(externaldns.TTL(300)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
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

		Context("geo+weighted with custom weights", func() {

			BeforeEach(func() {

				dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
					WithTargetGateway(test.GatewayName).
					WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
					WithLoadBalancingFor(120, []*v1alpha1.CustomWeight{
						{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kuadrant.io/my-custom-weight-attr": "FOO",
								},
							},
							Weight: 100,
						},
						{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kuadrant.io/my-custom-weight-attr": "BAR",
								},
							},
							Weight: 160,
						},
					}, "IE")
				Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

				Eventually(func() error {
					gateway.Labels = map[string]string{}
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameOne+"_my-custom-weight-attr"] = "FOO"
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameTwo+"_my-custom-weight-attr"] = "BAR"
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameOne+"_lb-attribute-geo-code"] = "IE"
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameTwo+"_lb-attribute-geo-code"] = "ES"
					return k8sClient.Update(ctx, gateway)
				}, test.TimeoutMedium, test.RetryIntervalMedium).ShouldNot(HaveOccurred())

				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-control_my-custom-weight-attr", "FOO"))
				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-control_lb-attribute-geo-code", "IE"))
				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-workload-1_my-custom-weight-attr", "BAR"))
				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-workload-1_lb-attribute-geo-code", "ES"))
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
											"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
											"Targets":       ConsistOf(test.IPAddressTwo),
											"RecordType":    Equal("A"),
											"SetIdentifier": Equal(""),
											"RecordTTL":     Equal(externaldns.TTL(60)),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":          Equal("es.klb.test.example.com"),
											"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
											"RecordTTL":        Equal(externaldns.TTL(60)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "160"}}),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":          Equal("ie.klb.test.example.com"),
											"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
											"RecordTTL":        Equal(externaldns.TTL(60)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "100"}}),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
											"Targets":       ConsistOf(test.IPAddressOne),
											"RecordType":    Equal("A"),
											"SetIdentifier": Equal(""),
											"RecordTTL":     Equal(externaldns.TTL(60)),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":          Equal("klb.test.example.com"),
											"Targets":          ConsistOf("es.klb.test.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal("ES"),
											"RecordTTL":        Equal(externaldns.TTL(300)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "ES"}}),
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
							MatchFields(IgnoreExtras, Fields{
								"ObjectMeta": HaveField("Name", wildcardRecordName),
								"Spec": MatchFields(IgnoreExtras, Fields{
									"OwnerID":        Equal(&ownerID),
									"ManagedZoneRef": HaveField("Name", "mz-example-com"),
									"Endpoints": ConsistOf(
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
											"Targets":       ConsistOf(test.IPAddressTwo),
											"RecordType":    Equal("A"),
											"SetIdentifier": Equal(""),
											"RecordTTL":     Equal(externaldns.TTL(60)),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":          Equal("es.klb.example.com"),
											"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
											"RecordTTL":        Equal(externaldns.TTL(60)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "160"}}),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":          Equal("ie.klb.example.com"),
											"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
											"RecordTTL":        Equal(externaldns.TTL(60)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "100"}}),
										})),
										PointTo(MatchFields(IgnoreExtras, Fields{
											"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
											"Targets":       ConsistOf(test.IPAddressOne),
											"RecordType":    Equal("A"),
											"SetIdentifier": Equal(""),
											"RecordTTL":     Equal(externaldns.TTL(60)),
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
											"DNSName":          Equal("klb.example.com"),
											"Targets":          ConsistOf("es.klb.example.com"),
											"RecordType":       Equal("CNAME"),
											"SetIdentifier":    Equal("ES"),
											"RecordTTL":        Equal(externaldns.TTL(300)),
											"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "ES"}}),
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
											"DNSName":       Equal(test.HostWildcard),
											"Targets":       ConsistOf("klb.example.com"),
											"RecordType":    Equal("CNAME"),
											"SetIdentifier": Equal(""),
											"RecordTTL":     Equal(externaldns.TTL(300)),
										})),
									),
								}),
							}),
						))
				}, test.TimeoutMedium, test.RetryIntervalMedium, ctx).Should(Succeed())
			})

		})

		Context("geo+weighted with no matching default geo", func() {

			BeforeEach(func() {
				dnsPolicy = v1alpha1.NewDNSPolicy("test-dns-policy", testNamespace).
					WithTargetGateway(test.GatewayName).
					WithRoutingStrategy(v1alpha1.LoadBalancedRoutingStrategy).
					WithLoadBalancingFor(120, nil, "cat")
				Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())

				Eventually(func() error {
					gateway.Labels = map[string]string{}
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameOne+"_lb-attribute-geo-code"] = "IE"
					gateway.Labels["clusters.kuadrant.io/"+test.ClusterNameTwo+"_lb-attribute-geo-code"] = "ES"
					return k8sClient.Update(ctx, gateway)
				}, test.TimeoutMedium, test.RetryIntervalMedium).ShouldNot(HaveOccurred())

				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-control_lb-attribute-geo-code", "IE"))
				Expect(gateway.Labels).To(HaveKeyWithValue("clusters.kuadrant.io/test-placed-workload-1_lb-attribute-geo-code", "ES"))
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
										"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"Targets":       ConsistOf(test.IPAddressOne),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.test.example.com"),
										"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("es.klb.test.example.com"),
										"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.test.example.com"),
										"Targets":       ConsistOf(test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(test.HostOne),
										"Targets":       ConsistOf("klb.test.example.com"),
										"RecordType":    Equal("CNAME"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(300)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("klb.test.example.com"),
										"Targets":          ConsistOf("es.klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal("ES"),
										"RecordTTL":        Equal(externaldns.TTL(300)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "ES"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("klb.test.example.com"),
										"Targets":          ConsistOf("ie.klb.test.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal("IE"),
										"RecordTTL":        Equal(externaldns.TTL(300)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
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
								"Endpoints": ConsistOf(
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"Targets":       ConsistOf(test.IPAddressOne),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("ie.klb.example.com"),
										"Targets":          ConsistOf(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterOneIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("es.klb.example.com"),
										"Targets":          ConsistOf(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"RecordTTL":        Equal(externaldns.TTL(60)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(clusterTwoIDHash + "-" + gwHash + ".klb.example.com"),
										"Targets":       ConsistOf(test.IPAddressTwo),
										"RecordType":    Equal("A"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(60)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":       Equal(test.HostWildcard),
										"Targets":       ConsistOf("klb.example.com"),
										"RecordType":    Equal("CNAME"),
										"SetIdentifier": Equal(""),
										"RecordTTL":     Equal(externaldns.TTL(300)),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("klb.example.com"),
										"Targets":          ConsistOf("es.klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal("ES"),
										"RecordTTL":        Equal(externaldns.TTL(300)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "ES"}}),
									})),
									PointTo(MatchFields(IgnoreExtras, Fields{
										"DNSName":          Equal("klb.example.com"),
										"Targets":          ConsistOf("ie.klb.example.com"),
										"RecordType":       Equal("CNAME"),
										"SetIdentifier":    Equal("IE"),
										"RecordTTL":        Equal(externaldns.TTL(300)),
										"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
									})),
								),
							}),
						}),
					)
				}, test.TimeoutMedium, test.RetryIntervalMedium, ctx).Should(Succeed())
			})
		})
	})
})
