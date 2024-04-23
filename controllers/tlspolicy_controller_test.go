//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	k8certsv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

var _ = Describe("TLSPolicy controller", Ordered, func() {

	var testNamespace string
	var gatewayClass *gatewayapiv1.GatewayClass
	var issuer *certmanv1.Issuer
	var issuerRef *certmanmetav1.ObjectReference
	var gateway *gatewayapiv1.Gateway
	var tlsPolicy *v1alpha1.TLSPolicy
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()
		gatewayClass = testBuildGatewayClass("foo", "default", "kuadrant.io/bar")
		Expect(k8sClient.Create(ctx, gatewayClass)).To(Succeed())
	})

	BeforeEach(func() {
		CreateNamespace(&testNamespace)
		issuer, issuerRef = testBuildSelfSignedIssuer("testissuer", testNamespace)
		Expect(k8sClient.Create(ctx, issuer)).To(BeNil())
	})

	AfterEach(func() {
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
		DeleteNamespaceCallback(&testNamespace)()
	})

	AfterAll(func() {
		err := k8sClient.Delete(ctx, gatewayClass)
		Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

	})

	Context("invalid target", func() {
		BeforeEach(func() {
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway("test-gateway").
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should have accepted condition with status false and correct reason", func() {
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
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should have accepted condition with status true", func() {
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
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			By("creating a valid Gateway")
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
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
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

	})

	Context("valid target, issuer and policy", func() {
		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should have accepted condition with status true", func() {
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
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("should set gateway back reference and policy affected status", func() {
			policyBackRefValue := testNamespace + "/" + tlsPolicy.Name
			refs, _ := json.Marshal([]client.ObjectKey{{Name: tlsPolicy.Name, Namespace: testNamespace}})
			policiesBackRefValue := string(refs)

			Eventually(func(g Gomega) {
				gw := &gatewayapiv1.Gateway{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: gateway.Name, Namespace: testNamespace}, gw)
				//Check annotations
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.TLSPolicyDirectReferenceAnnotationName, policyBackRefValue))
				g.Expect(gw.Annotations).To(HaveKeyWithValue(v1alpha1.TLSPolicyBackReferenceAnnotationName, policiesBackRefValue))
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})
	})

	Context("valid target, clusterissuer and policy", func() {
		var clusterIssuer *certmanv1.ClusterIssuer
		var clusterIssuerRef *certmanmetav1.ObjectReference

		BeforeEach(func() {
			clusterIssuer, clusterIssuerRef = testBuildSelfSignedClusterIssuer("testclusterissuer", testNamespace)
			Expect(k8sClient.Create(ctx, clusterIssuer)).To(BeNil())
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*clusterIssuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		AfterEach(func() {
			err := k8sClient.Delete(ctx, clusterIssuer)
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		})

		It("should have accepted condition with status true", func() {
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
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})
	})

	Context("with http listener", func() {
		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPListener("test-listener", "test.example.com").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should not create any certificates when TLS is not present", func() {
			Consistently(func() []certmanv1.Certificate {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				Expect(err).ToNot(HaveOccurred())
				return certList.Items
			}, time.Second*10, time.Second).Should(BeEmpty())
		})

		It("should create certificate when TLS is present", func() {
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
						HaveField("Name", "test-tls-secret"),
					))
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
		})

	})

	Context("with https listener", func() {
		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test.example.com", "test-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificate", func() {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-tls-secret"),
					))
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
		})
	})

	Context("with multiple https listener and some shared secrets", func() {

		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test-tls-secret").
				WithHTTPSListener("test2.example.com", "test-tls-secret").
				WithHTTPSListener("test3.example.com", "test2-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificates", func() {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(2))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-tls-secret"),
						HaveField("Name", "test2-tls-secret"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-tls-secret", Namespace: testNamespace}, cert1)
				Expect(err).ToNot(HaveOccurred())

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test2-tls-secret", Namespace: testNamespace}, cert2)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert1.Spec.DNSNames).To(ConsistOf("test1.example.com", "test2.example.com"))
				Expect(cert2.Spec.DNSNames).To(ConsistOf("test3.example.com"))
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
		})
	})

	Context("with multiple https listener", func() {
		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test1.example.com", "test1-tls-secret").
				WithHTTPSListener("test2.example.com", "test2-tls-secret").
				WithHTTPSListener("test3.example.com", "test3-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
				WithTargetGateway(gateway.Name).
				WithIssuerRef(*issuerRef)
			Expect(k8sClient.Create(ctx, tlsPolicy)).To(BeNil())
		})

		It("should create tls certificates", func() {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(3))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test1-tls-secret"),
						HaveField("Name", "test2-tls-secret"),
						HaveField("Name", "test3-tls-secret"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test1-tls-secret", Namespace: testNamespace}, cert1)
				Expect(err).ToNot(HaveOccurred())

				cert2 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test2-tls-secret", Namespace: testNamespace}, cert2)
				Expect(err).ToNot(HaveOccurred())

				cert3 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test3-tls-secret", Namespace: testNamespace}, cert3)
				Expect(err).ToNot(HaveOccurred())

				Expect(cert1.Spec.DNSNames).To(ConsistOf("test1.example.com"))
				Expect(cert2.Spec.DNSNames).To(ConsistOf("test2.example.com"))
				Expect(cert3.Spec.DNSNames).To(ConsistOf("test3.example.com"))
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
		})

		It("should delete tls certificate when listener is removed", func() {
			//confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())

			//remove a listener
			patch := client.MergeFrom(gateway.DeepCopy())
			gateway.Spec.Listeners = gateway.Spec.Listeners[1:]
			Expect(k8sClient.Patch(ctx, gateway, patch)).To(BeNil())

			//confirm a certificate has been deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 2 {
					return fmt.Errorf("expected 2 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*120, time.Second).Should(BeNil())
		})

		It("should delete all tls certificates when tls policy is removed even if gateway is already removed", func() {
			//confirm all expected certificates are present
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 3 {
					return fmt.Errorf("expected 3 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*10, time.Second).Should(BeNil())

			// delete the gateway
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, gateway))).ToNot(HaveOccurred())

			//delete the tls policy
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, tlsPolicy))).ToNot(HaveOccurred())

			//confirm all certificates have been deleted
			Eventually(func() error {
				certificateList := &certmanv1.CertificateList{}
				Expect(k8sClient.List(ctx, certificateList, &client.ListOptions{Namespace: testNamespace})).To(BeNil())
				if len(certificateList.Items) != 0 {
					return fmt.Errorf("expected 0 certificates, found: %v", len(certificateList.Items))
				}
				return nil
			}, time.Second*60, time.Second).Should(BeNil())
		})
	})

	Context("with https listener and multiple issuer configurations", func() {
		BeforeEach(func() {
			gateway = NewGatewayBuilder("test-gateway", gatewayClass.Name, testNamespace).
				WithHTTPSListener("test.example.com", "test-tls-secret").Gateway
			Expect(k8sClient.Create(ctx, gateway)).To(BeNil())
			tlsPolicy = v1alpha1.NewTLSPolicy("test-tls-policy", testNamespace).
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

		It("should create tls certificate", func() {
			Eventually(func(g Gomega, ctx context.Context) {
				certList := &certmanv1.CertificateList{}
				err := k8sClient.List(ctx, certList, &client.ListOptions{Namespace: testNamespace})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(certList.Items).To(HaveLen(1))
				g.Expect(certList.Items).To(
					ContainElements(
						HaveField("Name", "test-tls-secret"),
					))

				cert1 := &certmanv1.Certificate{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-tls-secret", Namespace: testNamespace}, cert1)
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
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())
		})

	})
})
