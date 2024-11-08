//go:build integration

package tlspolicy

import (
	"context"
	"fmt"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/samber/lo"
	k8certsv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var _ = Describe("TLSPolicy controller", func() {
	const (
		testTimeOut      = SpecTimeout(1 * time.Minute)
		afterEachTimeOut = NodeTimeout(2 * time.Minute)
	)

	var gatewayClass *gatewayapiv1.GatewayClass
	var testNamespace string
	var issuer *certmanv1.Issuer
	var issuerRef *certmanmetav1.ObjectReference
	var gateway *gatewayapiv1.Gateway
	var tlsPolicy *kuadrantv1.TLSPolicy

	BeforeEach(func(ctx SpecContext) {
		testNamespace = tests.CreateNamespace(ctx, testClient())

		gatewayClass = tests.BuildGatewayClass("gwc-"+testNamespace, "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())

		issuer, issuerRef = tests.BuildSelfSignedIssuer("testissuer", testNamespace)
		Expect(k8sClient.Create(ctx, issuer)).To(BeNil())
	})

	AfterEach(func(ctx SpecContext) {
		if gateway != nil {
			err := k8sClient.Delete(ctx, gateway)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if tlsPolicy != nil {
			err := k8sClient.Delete(ctx, tlsPolicy)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if issuer != nil {
			err := k8sClient.Delete(ctx, issuer)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if gatewayClass != nil {
			err := k8sClient.Delete(ctx, gatewayClass)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		tests.DeleteNamespace(ctx, testClient(), testNamespace)
	}, afterEachTimeOut)

	Context("invalid target", func() {
		BeforeEach(func(ctx SpecContext) {
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway("test-gateway").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should have accepted condition with status false and correct reason", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonTargetNotFound)),
						"Message": Equal("TLSPolicy target test-gateway was not found"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(kuadrant.PolicyConditionEnforced)),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)

		It("should have accepted condition with status true", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonTargetNotFound)),
						"Message": Equal("TLSPolicy target test-gateway was not found"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(kuadrant.PolicyConditionEnforced)),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())

			By("creating a valid Gateway")
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("no-host", "").
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Message": Equal("TLSPolicy has been accepted"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyConditionEnforced)),
						"Message": Equal("TLSPolicy has been successfully enforced"),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)

	})

	Context("valid target, invalid issuer", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
		})

		It("invalid kind - should have accepted condition with status false and correct reason", func(ctx SpecContext) {
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(certmanmetav1.ObjectReference{Kind: "NotIssuer"})
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonInvalid)),
						"Message": Equal("TLSPolicy target is invalid: invalid value \"NotIssuer\" for issuerRef.kind. Must be empty, \"Issuer\" or \"ClusterIssuer\""),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(kuadrant.PolicyConditionEnforced)),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)

		It("unable to find issuer - should have accepted condition with status false and correct reason", func(ctx SpecContext) {
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(certmanmetav1.ObjectReference{Name: "DoesNotExist"})
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyReasonInvalid)),
						"Message": Equal("TLSPolicy target is invalid: unable to find issuer"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).ToNot(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type": Equal(string(kuadrant.PolicyConditionEnforced)),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)
	})

	Context("valid target, issuer and policy", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should have accepted condition with status true", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Message": Equal("TLSPolicy has been accepted"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyConditionEnforced)),
						"Message": Equal("TLSPolicy has been successfully enforced"),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)
	})

	Context("valid target, clusterissuer and policy", func() {
		var clusterIssuer *certmanv1.ClusterIssuer
		var clusterIssuerRef *certmanmetav1.ObjectReference

		BeforeEach(func(ctx SpecContext) {
			clusterIssuer, clusterIssuerRef = tests.BuildSelfSignedClusterIssuer("testclusterissuer", testNamespace)
			Expect(k8sClient.Create(ctx, clusterIssuer)).To(BeNil())
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*clusterIssuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		AfterEach(func(ctx SpecContext) {
			err := k8sClient.Delete(ctx, clusterIssuer)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}, afterEachTimeOut)

		It("should have accepted and enforced condition with status true", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(gatewayapiv1alpha2.PolicyConditionAccepted)),
						"Message": Equal("TLSPolicy has been accepted"),
					})),
				)
				g.Expect(tlsPolicy.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(kuadrant.PolicyConditionEnforced)),
						"Status":  Equal(metav1.ConditionTrue),
						"Reason":  Equal(string(kuadrant.PolicyConditionEnforced)),
						"Message": Equal("TLSPolicy has been successfully enforced"),
					})),
				)
			}, tests.TimeoutLong, time.Second).Should(Succeed())
		}, testTimeOut)
	})

	Context("with http listener", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should not create any certificates when TLS is not present", func(ctx SpecContext) {
			Consistently(func() []certmanv1.Certificate {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				Expect(err).ToNot(HaveOccurred())
				return certList.Items
			}, time.Second*10, time.Second).Should(BeEmpty())
		}, testTimeOut)

		It("should create certificate when TLS is present", func(ctx SpecContext) {
			certNS := gatewayapiv1.Namespace(testNamespace)
			patch := client.MergeFrom(gateway.DeepCopy())
			gateway.Spec.Listeners[0].Protocol = gatewayapiv1.HTTPSProtocolType
			gateway.Spec.Listeners[0].TLS = &gatewayapiv1.GatewayTLSConfig{
				Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
				CertificateRefs: []gatewayapiv1.SecretObjectReference{
					{
						Name:      "test-tls-secret",
						Namespace: &certNS,
					},
				},
			}
			Expect(k8sClient.Patch(ctx, gateway, patch)).To(BeNil())

			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test-listener"),
					))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)

	})

	Context("with https listener", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test.example.com", "test-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificate", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test.example.com"),
					))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("with multiple https listener and some shared secrets is not allowed", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test-tls-secret").
				WithHTTPSListener("test2.example.com", "test-tls-secret").
				WithHTTPSListener("test3.example.com", "test2-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
		})

		It("should create tls certificates but one cert will not be ready", func(ctx SpecContext) {
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())

			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(3))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-test2.example.com"),
						HaveField("Name", "test-gateway-test3.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert1)
				g.Expect(err).ToNot(HaveOccurred())

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test2.example.com", Namespace: testNamespace}, cert2)
				g.Expect(err).ToNot(HaveOccurred())

				cert3 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test3.example.com", Namespace: testNamespace}, cert3)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(cert1.Spec.DNSNames).To(ConsistOf("test1.example.com"))
				g.Expect(cert2.Spec.DNSNames).To(ConsistOf("test2.example.com"))
				g.Expect(cert3.Spec.DNSNames).To(ConsistOf("test3.example.com"))

				// Only 2 of the certs should be ready
				readyCertCount := 0
				for _, cert := range certList.Items {
					for _, cond := range cert.Status.Conditions {
						if cond.Type == certmanv1.CertificateConditionReady && cond.Status == certmanmetav1.ConditionTrue {
							readyCertCount++
							continue
						}
						// Unready cert
						g.Expect(cond.Reason).To(Equal("IncorrectCertificate"))
					}
				}
				g.Expect(readyCertCount).To(Equal(2))

				// Policy should not be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)).To(Succeed())
				enforcedCond := meta.FindStatusCondition(tlsPolicy.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
				g.Expect(enforcedCond).ToNot(BeNil())
				g.Expect(enforcedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(enforcedCond.Message).To(ContainSubstring("Shared TLS certificates refs between listeners not supported. Use unique certificates refs in the Gateway listeners to fully enforce policy"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("with multiple https listener", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").
				WithHTTPSListener("test2.example.com", "test2-tls-secret").
				WithHTTPSListener("test3.example.com", "test3-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificates", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(3))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-test2.example.com"),
						HaveField("Name", "test-gateway-test3.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert1)
				Expect(err).ToNot(HaveOccurred())

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test2.example.com", Namespace: testNamespace}, cert2)
				Expect(err).ToNot(HaveOccurred())

				cert3 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test3.example.com", Namespace: testNamespace}, cert3)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert1.Spec.DNSNames).To(ConsistOf("test1.example.com"))
				Expect(cert2.Spec.DNSNames).To(ConsistOf("test2.example.com"))
				Expect(cert3.Spec.DNSNames).To(ConsistOf("test3.example.com"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)

		It("should delete all tls certificates when policy is deleted", func(ctx SpecContext) {
			// confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			// delete the tls policy
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, tlsPolicy))).ToNot(HaveOccurred())

			// confirm all certificates have been deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				if err := k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace}); err != nil {
					return err
				}
				if len(certificateList.Items) != 0 {
					return fmt.Errorf("expected 0 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)

		It("should delete tls certificate when listener is removed", func(ctx SpecContext) {
			// confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			// remove a listener
			patch := client.MergeFrom(gateway.DeepCopy())
			gateway.Spec.Listeners = gateway.Spec.Listeners[1:]
			Expect(k8sClient.Patch(ctx, gateway, patch)).To(BeNil())

			// confirm a certificate has been deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				if err := k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace}); err != nil {
					return err
				}
				if len(certificateList.Items) != 2 {
					return fmt.Errorf("expected 2 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)

		It("should delete all tls certificates when gateway is deleted", func(ctx SpecContext) {
			// confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			// delete the gateway
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, gateway))).ToNot(HaveOccurred())

			// confirm all certificates have been deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 0 {
					return fmt.Errorf("expected 0 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)

		It("Should delete orphaned tls certificates when changing to valid target ref", func(ctx SpecContext) {
			// confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			// new gateway with one listener
			gateway2 := tests.NewGatewayBuilder("test-gateway-2", gatewayClass.Name, testNamespace).
				WithHTTPSListener("gateway2.example.com", "gateway2-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway2)).To(BeNil())

			// update tls policy target ref to new gateway
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)).To(Succeed())
				tlsPolicy.Spec.TargetRef.Name = gatewayapiv1.ObjectName(gateway2.Name)
				g.Expect(k8sClient.Update(ctx, tlsPolicy)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// confirm orphaned certs are deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 1 {
					return fmt.Errorf("expected 1 certificates, found: %v", len(certificateList.Items))
				}

				if certificateList.Items[0].Name != "test-gateway-2-gateway2.example.com" {
					return fmt.Errorf("expected certificate to be 'gateway2-tls-secret', found: %s", certificateList.Items[0].Name)

				}
				return nil
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)

		It("Should delete orphaned tls certificates when changing to invalid target ref", func(ctx SpecContext) {
			// confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			// update tlspolicy target ref to invalid reference
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy), tlsPolicy)).To(Succeed())
				tlsPolicy.Spec.TargetRef.Name = "does-not-exist"
				g.Expect(k8sClient.Update(ctx, tlsPolicy)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())

			// confirm orphaned certs are deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 0 {
					return fmt.Errorf("expected 0 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, tests.TimeoutLong, time.Second).Should(BeNil())
		}, testTimeOut)
	})

	Context("with https listener and multiple issuer configurations", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test.example.com", "test-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			tlsPolicy.Spec.CommonName = "example.com"
			tlsPolicy.Spec.Duration = &metav1.Duration{Duration: time.Minute * 120}
			tlsPolicy.Spec.PrivateKey = &certmanv1.CertificatePrivateKey{
				RotationPolicy: certmanv1.RotationPolicyAlways,
				Encoding:       certmanv1.PKCS1,
				Algorithm:      certmanv1.ECDSAKeyAlgorithm,
				Size:           256,
			}
			tlsPolicy.Spec.RenewBefore = &metav1.Duration{Duration: time.Minute * 60}
			tlsPolicy.Spec.Usages = []certmanv1.KeyUsage{
				certmanv1.UsageDigitalSignature,
				certmanv1.KeyUsage(k8certsv1.UsageKeyEncipherment),
				certmanv1.UsageCertSign,
			}
			tlsPolicy.Spec.RevisionHistoryLimit = ptr.To(int32(1))

			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificate", func(ctx SpecContext) {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test.example.com", Namespace: testNamespace}, cert1)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert1.Spec.DNSNames).To(ConsistOf("test.example.com"))
				Expect(cert1.Spec.CommonName).To(Equal("example.com"))
				Expect(cert1.Spec.Duration).To(PointTo(Equal(metav1.Duration{Duration: time.Minute * 120})))
				Expect(cert1.Spec.PrivateKey.RotationPolicy).To(Equal(certmanv1.RotationPolicyAlways))
				Expect(cert1.Spec.PrivateKey.Encoding).To(Equal(certmanv1.PKCS1))
				Expect(cert1.Spec.PrivateKey.Algorithm).To(Equal(certmanv1.ECDSAKeyAlgorithm))
				Expect(cert1.Spec.PrivateKey.Size).To(Equal(256))
				Expect(cert1.Spec.RenewBefore).To(PointTo(Equal(metav1.Duration{Duration: time.Minute * 60})))
				Expect(cert1.Spec.Usages).To(ConsistOf(
					certmanv1.UsageDigitalSignature,
					certmanv1.KeyUsage(k8certsv1.UsageKeyEncipherment),
					certmanv1.UsageCertSign,
				))
				Expect(cert1.Spec.RevisionHistoryLimit).To(PointTo(Equal(int32(1))))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)

	})

	Context("cel validation", func() {
		It("should error targeting invalid group", func(ctx SpecContext) {
			p := kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway("gateway")
			p.Spec.TargetRef.Group = "not-gateway.networking.k8s.io"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"))
		}, testTimeOut)

		It("should error targeting invalid kind", func(ctx SpecContext) {
			p := kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway("gateway")
			p.Spec.TargetRef.Kind = "TCPRoute"

			err := k8sClient.Create(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid targetRef.kind. The only supported values are 'Gateway'"))
		}, testTimeOut)
	})

	Context("section name", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").
				WithHTTPSListener("test2.example.com", "test2-tls-secret").
				WithHTTPSListener("test3.example.com", "test3-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
		})

		It("cert for only the targeted section is created", func(ctx SpecContext) {
			// Create first TLS Policy targeting one section
			tlsPolicy1 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-1", testNamespace).
				WithTargetGatewaySection(gateway.Name, "test1.example.com").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy1)).To(Succeed())

			// Only one cert
			Eventually(func(g Gomega, ctx context.Context) {
				// Policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
					))

				cert := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert.Spec.DNSNames).To(ConsistOf("test1.example.com"))

			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())

			// Create second TLS Policy targeting another section
			tlsPolicy2 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-2", testNamespace).
				WithTargetGatewaySection(gateway.Name, "test2.example.com").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy2)).To(Succeed())

			// Two certs
			Eventually(func(g Gomega, ctx context.Context) {
				// Both policies should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy2), tlsPolicy2)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy2.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(2))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-test2.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert1)
				Expect(err).ToNot(HaveOccurred())

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test2.example.com", Namespace: testNamespace}, cert2)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert1.Spec.DNSNames).To(ConsistOf("test1.example.com"))
				Expect(cert2.Spec.DNSNames).To(ConsistOf("test2.example.com"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		})

		It("section name policy and gateway policy", func(ctx SpecContext) {
			// Create first TLS Policy targeting one section
			tlsPolicy1 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-1", testNamespace).
				WithTargetGatewaySection(gateway.Name, "test1.example.com").
				WithIssuerRef(*issuerRef)
			tlsPolicy1.Spec.CommonName = "example.com"
			Expect(k8sClient.Create(ctx, tlsPolicy1)).To(Succeed())

			// Only one cert
			Eventually(func(g Gomega, ctx context.Context) {
				// Policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
					))
				cert := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(cert.Spec.DNSNames).To(ConsistOf("test1.example.com"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())

			// Create second TLS Policy targeting gateway
			tlsPolicy2 := kuadrantv1.NewTLSPolicy("test-tls-policy-gw", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy2)).To(Succeed())

			// 3 certs
			Eventually(func(g Gomega, ctx context.Context) {
				// Policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy2), tlsPolicy2)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy2.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(3))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-test2.example.com"),
						HaveField("Name", "test-gateway-test3.example.com"),
					))

				for _, cert := range certList.Items {
					// should be affected by section name policy
					if cert.Name == "test-gateway-test1.example.com" {
						g.Expect(cert.Spec.CommonName).To(Equal("example.com"))
					} else {
						// Should be affected by gw policy
						g.Expect(cert.Spec.CommonName).To(BeEmpty())
					}
				}
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("multiple gateway with a listener referencing the same tls cert ref", func() {
		It("should report duplication in affected policy - gateway policies", func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())

			gateway2 := tests.NewGatewayBuilder("test-gateway-2", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test2.example.com", "test1-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway2)).To(BeNil())

			// Create first TLS Policy targeting first gateway
			tlsPolicy1 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-1", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy1)).To(Succeed())

			// Only one cert
			Eventually(func(g Gomega, ctx context.Context) {
				// Policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
					))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())

			// Create second TLS Policy targeting second gateway
			tlsPolicy2 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-2", testNamespace).
				WithTargetGateway(gateway2.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy2)).To(Succeed())

			// Two certs
			Eventually(func(g Gomega, ctx context.Context) {
				// Only first policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy2), tlsPolicy2)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy2.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeFalse())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(2))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-2-test2.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert1)
				g.Expect(err).ToNot(HaveOccurred())
				cond, found := lo.Find(cert1.Status.Conditions, func(item certmanv1.CertificateCondition) bool {
					return item.Type == certmanv1.CertificateConditionReady
				})
				g.Expect(found).To(BeTrue())
				g.Expect(cond.Status).To(Equal(certmanmetav1.ConditionTrue))

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-2-test2.example.com", Namespace: testNamespace}, cert2)
				g.Expect(err).ToNot(HaveOccurred())
				cond, found = lo.Find(cert2.Status.Conditions, func(item certmanv1.CertificateCondition) bool {
					return item.Type == certmanv1.CertificateConditionReady
				})
				g.Expect(found).To(BeTrue())
				g.Expect(cond.Status).To(Equal(certmanmetav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("IncorrectCertificate"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)

		It("should report duplication in affected policy - section policies", func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").
				WithHTTPSListener("test2.example.com", "test2-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())

			gateway2 := tests.NewGatewayBuilder("test-gateway-2", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test3.example.com", "test1-tls-secret").
				WithHTTPSListener("test4.example.com", "test3-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway2)).To(BeNil())

			// Create first TLS Policy targeting first gateway section
			tlsPolicy1 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-1", testNamespace).
				WithTargetGatewaySection(gateway.Name, "test1.example.com").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy1)).To(Succeed())

			// Only one cert
			Eventually(func(g Gomega, ctx context.Context) {
				// Policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
					))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())

			// Create second TLS Policy targeting second gateway
			tlsPolicy2 := kuadrantv1.NewTLSPolicy("test-tls-policy-section-2", testNamespace).
				WithTargetGatewaySection(gateway2.Name, "test3.example.com").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy2)).To(Succeed())

			// Two certs
			Eventually(func(g Gomega, ctx context.Context) {
				// Only first policy should be enforced
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy1), tlsPolicy1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tlsPolicy2), tlsPolicy2)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(tlsPolicy2.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeFalse())

				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(2))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-gateway-test1.example.com"),
						HaveField("Name", "test-gateway-2-test3.example.com"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-test1.example.com", Namespace: testNamespace}, cert1)
				g.Expect(err).ToNot(HaveOccurred())
				cond, found := lo.Find(cert1.Status.Conditions, func(item certmanv1.CertificateCondition) bool {
					return item.Type == certmanv1.CertificateConditionReady
				})
				g.Expect(found).To(BeTrue())
				g.Expect(cond.Status).To(Equal(certmanmetav1.ConditionTrue))

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-gateway-2-test3.example.com", Namespace: testNamespace}, cert2)
				g.Expect(err).ToNot(HaveOccurred())
				cond, found = lo.Find(cert2.Status.Conditions, func(item certmanv1.CertificateCondition) bool {
					return item.Type == certmanv1.CertificateConditionReady
				})
				g.Expect(found).To(BeTrue())
				g.Expect(cond.Status).To(Equal(certmanmetav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("IncorrectCertificate"))
			}, tests.TimeoutLong, time.Second, ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("Multiple policies with same target ref", func() {
		BeforeEach(func(ctx SpecContext) {
			gateway = tests.NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
		})

		It("Should conflict on the second created policy", func(ctx context.Context) {
			p1 := kuadrantv1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, p1)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(p1), p1)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(p1.Status.Conditions, string(kuadrant.PolicyConditionEnforced))).To(BeTrue())
			}).WithContext(ctx).Should(Succeed())

			p2 := kuadrantv1.NewTLSPolicy("test-tls-policy-2", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, p2)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(p2), p2)).To(Succeed())
				cond := meta.FindStatusCondition(p2.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(gatewayapiv1alpha2.PolicyReasonConflicted)))
				g.Expect(cond.Message).To(Equal(fmt.Sprintf("TLSPolicy is conflicted by %s: conflicting policy", client.ObjectKeyFromObject(p1).String())))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
