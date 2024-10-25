//go:build integration

package dnspolicy

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/rand"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/tests"
)

func getClusterUID(ctx context.Context, c client.Client) (string, error) {
	ns := &corev1.Namespace{}
	err := c.Get(ctx, client.ObjectKey{Name: "kube-system"}, ns)
	if err != nil {
		return "", err
	}

	return string(ns.UID), nil
}

var _ = Describe("DNSPolicy Single Cluster", func() {
	const (
		testTimeOut      = SpecTimeout(1 * time.Minute)
		afterEachTimeOut = NodeTimeout(2 * time.Minute)
	)

	var gatewayClass *gatewayapiv1.GatewayClass
	var dnsProviderSecret *corev1.Secret
	var testNamespace string
	var gateway *gatewayapiv1.Gateway
	var dnsPolicy *v1alpha1.DNSPolicy
	var clusterHash, gwHash, recordName, wildcardRecordName string
	var domain = fmt.Sprintf("example-%s.com", rand.String(6))

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		var err error
		clusterUID, err := getClusterUID(ctx, k8sClient)
		Expect(err).To(BeNil())

		gatewayClass = tests.BuildGatewayClass("gwc-"+testNamespace, "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		dnsProviderSecret = tests.BuildInMemoryCredentialsSecret("inmemory-credentials", testNamespace, domain)
		Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())

		gateway = tests.NewGatewayBuilder(tests.GatewayName, gatewayClass.Name, testNamespace).
			WithHTTPListener("foo", fmt.Sprintf("foo.%s", domain)).
			WithHTTPListener(tests.ListenerNameOne, tests.HostOne(domain)).
			WithHTTPListener(tests.ListenerNameWildcard, tests.HostWildcard(domain)).
			Gateway
		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

		clusterHash = common.ToBase36HashLen(clusterUID, utils.ClusterIDLength)

		gwHash = common.ToBase36HashLen(gateway.Name+"-"+gateway.Namespace, 6)

		// refresh gateway
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
		//Set single cluster gateway status
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
					Name:           "foo",
					SupportedKinds: []gatewayapiv1.RouteGroupKind{},
					AttachedRoutes: 0,
					Conditions:     []metav1.Condition{},
				},
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

	Context("simple routing strategy", func() {

		BeforeEach(func(ctx SpecContext) {
			dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
				WithProviderSecret(*dnsProviderSecret).
				WithTargetGateway(tests.GatewayName)
			Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
		})

		It("should create dns records", func(ctx SpecContext) {
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

				g.Expect(dnsRecord.Name).To(Equal(recordName))
				g.Expect(dnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
				g.Expect(dnsRecord.Spec.Endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(tests.HostOne(domain)),
						"Targets":       ContainElements(tests.IPAddressOne, tests.IPAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldns.TTL(60)),
					})),
				))
				g.Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
				g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))
				g.Expect(tests.EndpointsTraversable(dnsRecord.Spec.Endpoints, tests.HostOne(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())

				g.Expect(wildcardDnsRecord.Name).To(Equal(wildcardRecordName))
				g.Expect(wildcardDnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
				g.Expect(wildcardDnsRecord.Spec.Endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName":       Equal(tests.HostWildcard(domain)),
						"Targets":       ContainElements(tests.IPAddressOne, tests.IPAddressTwo),
						"RecordType":    Equal("A"),
						"SetIdentifier": Equal(""),
						"RecordTTL":     Equal(externaldns.TTL(60)),
					})),
				))
				g.Expect(wildcardDnsRecord.Status.OwnerID).ToNot(BeEmpty())
				g.Expect(wildcardDnsRecord.Status.OwnerID).To(Equal(wildcardDnsRecord.GetUIDHash()))
				g.Expect(tests.EndpointsTraversable(wildcardDnsRecord.Spec.Endpoints, tests.HostWildcard(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())
			}, tests.TimeoutMedium, tests.RetryIntervalMedium, ctx).Should(Succeed())
		}, testTimeOut)

	})

	Context("loadbalanced routing strategy", func() {

		Context("with default geo", func() {
			BeforeEach(func(ctx SpecContext) {
				dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
					WithProviderSecret(*dnsProviderSecret).
					WithTargetGateway(tests.GatewayName).
					WithLoadBalancingFor(120, "IE", true)
				Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			})

			It("should create dns records", func(ctx SpecContext) {
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

					g.Expect(dnsRecord.Name).To(Equal(recordName))
					g.Expect(dnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
					g.Expect(dnsRecord.Spec.Endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(tests.IPAddressOne, tests.IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb.test." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(externaldns.TTL(60)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("IE"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(tests.HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(300)),
						})),
					))
					g.Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
					g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))
					g.Expect(tests.EndpointsTraversable(dnsRecord.Spec.Endpoints, tests.HostOne(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())

					g.Expect(wildcardDnsRecord.Name).To(Equal(wildcardRecordName))
					g.Expect(wildcardDnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
					g.Expect(wildcardDnsRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"Targets":       ConsistOf(tests.IPAddressOne, tests.IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordTTL":        Equal(externaldns.TTL(60)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("default"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "*"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("IE"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(tests.HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(300)),
						})),
					))
					g.Expect(wildcardDnsRecord.Status.OwnerID).ToNot(BeEmpty())
					g.Expect(wildcardDnsRecord.Status.OwnerID).To(Equal(wildcardDnsRecord.GetUIDHash()))
					g.Expect(tests.EndpointsTraversable(wildcardDnsRecord.Spec.Endpoints, tests.HostWildcard(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())
				}, tests.TimeoutMedium, tests.RetryIntervalMedium, ctx).Should(Succeed())
			}, testTimeOut)

		})

		Context("without default geo", func() {
			BeforeEach(func(ctx SpecContext) {
				dnsPolicy = tests.NewDNSPolicy("test-dns-policy", testNamespace).
					WithProviderSecret(*dnsProviderSecret).
					WithTargetGateway(tests.GatewayName).
					WithLoadBalancingFor(120, "IE", false)
				Expect(k8sClient.Create(ctx, dnsPolicy)).To(Succeed())
			})

			It("should create dns records", func(ctx SpecContext) {
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

					g.Expect(dnsRecord.Name).To(Equal(recordName))
					g.Expect(dnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
					g.Expect(dnsRecord.Spec.Endpoints).To(ConsistOf(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"Targets":       ConsistOf(tests.IPAddressOne, tests.IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb.test." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb.test." + domain),
							"RecordTTL":        Equal(externaldns.TTL(60)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb.test." + domain),
							"Targets":          ConsistOf("ie.klb.test." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("IE"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(tests.HostOne(domain)),
							"Targets":       ConsistOf("klb.test." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(300)),
						})),
					))
					g.Expect(dnsRecord.Status.OwnerID).ToNot(BeEmpty())
					g.Expect(dnsRecord.Status.OwnerID).To(Equal(dnsRecord.GetUIDHash()))
					g.Expect(tests.EndpointsTraversable(dnsRecord.Spec.Endpoints, tests.HostOne(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())

					g.Expect(wildcardDnsRecord.Name).To(Equal(wildcardRecordName))
					g.Expect(wildcardDnsRecord.Spec.ProviderRef.Name).To(Equal(dnsProviderSecret.Name))
					g.Expect(wildcardDnsRecord.Spec.Endpoints).To(ContainElements(
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"Targets":       ConsistOf(tests.IPAddressOne, tests.IPAddressTwo),
							"RecordType":    Equal("A"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(60)),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("ie.klb." + domain),
							"Targets":          ConsistOf(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal(clusterHash + "-" + gwHash + "." + "klb." + domain),
							"RecordTTL":        Equal(externaldns.TTL(60)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "weight", Value: "120"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":          Equal("klb." + domain),
							"Targets":          ConsistOf("ie.klb." + domain),
							"RecordType":       Equal("CNAME"),
							"SetIdentifier":    Equal("IE"),
							"RecordTTL":        Equal(externaldns.TTL(300)),
							"ProviderSpecific": Equal(externaldns.ProviderSpecific{{Name: "geo-code", Value: "IE"}}),
						})),
						PointTo(MatchFields(IgnoreExtras, Fields{
							"DNSName":       Equal(tests.HostWildcard(domain)),
							"Targets":       ConsistOf("klb." + domain),
							"RecordType":    Equal("CNAME"),
							"SetIdentifier": Equal(""),
							"RecordTTL":     Equal(externaldns.TTL(300)),
						})),
					))
					g.Expect(wildcardDnsRecord.Status.OwnerID).ToNot(BeEmpty())
					g.Expect(wildcardDnsRecord.Status.OwnerID).To(Equal(wildcardDnsRecord.GetUIDHash()))
					g.Expect(tests.EndpointsTraversable(wildcardDnsRecord.Spec.Endpoints, tests.HostWildcard(domain), []string{tests.IPAddressOne, tests.IPAddressTwo})).To(BeTrue())
				}, tests.TimeoutMedium, tests.RetryIntervalMedium, ctx).Should(Succeed())
			}, testTimeOut)

		})
	})
})
