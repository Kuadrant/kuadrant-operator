//go:build integration

/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"os"
	"path/filepath"
	"testing"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	istioapis "istio.io/istio/operator/pkg/apis"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var appNamespace string

func testClient() client.Client { return k8sClient }

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &[]bool{true}[0],
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = kuadrantdnsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = gatewayapiv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = authorinoopapi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = authorinoapi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = istioapis.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = istiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = istiosecurityv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = limitadorv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = istioclientnetworkingv1alpha3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = istioclientgoextensionv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = certmanv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).ToNot(HaveOccurred())

	authPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("authpolicy"),
		mgr.GetEventRecorderFor("AuthPolicy"),
	)

	err = (&AuthPolicyReconciler{
		BaseReconciler:      authPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
		AffectedPolicyMap:   kuadrant.NewAffectedPolicyMap(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	rateLimitPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy"),
		mgr.GetEventRecorderFor("RateLimitPolicy"),
	)

	err = (&RateLimitPolicyReconciler{
		BaseReconciler:      rateLimitPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
		AffectedPolicyMap:   kuadrant.NewAffectedPolicyMap(),
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	tlsPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("tlspolicy"),
		mgr.GetEventRecorderFor("TLSPolicy"),
	)

	err = (&TLSPolicyReconciler{
		BaseReconciler:      tlsPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	dnsPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("dnspolicy"),
		mgr.GetEventRecorderFor("DNSPolicy"),
	)

	err = (&DNSPolicyReconciler{
		BaseReconciler:      dnsPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	kuadrantBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("kuadrant-controller"),
		mgr.GetEventRecorderFor("Kuadrant"),
	)

	err = (&KuadrantReconciler{
		BaseReconciler: kuadrantBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	limitadorClusterEnvoyFilterBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("envoyfilter"),
		mgr.GetEventRecorderFor("LimitadorClusterEnvoyFilter"),
	)

	err = (&LimitadorClusterEnvoyFilterReconciler{
		BaseReconciler: limitadorClusterEnvoyFilterBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	gatewayKuadrantBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("kuadrant").WithName("gateway"),
		mgr.GetEventRecorderFor("GatewayKuadrant"),
	)

	err = (&GatewayKuadrantReconciler{
		BaseReconciler: gatewayKuadrantBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	rateLimitingWASMPluginBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("wasmplugin"),
		mgr.GetEventRecorderFor("RateLimitingWASMPlugin"),
	)

	err = (&RateLimitingWASMPluginReconciler{
		BaseReconciler: rateLimitingWASMPluginBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	targetStatusBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("targetstatus"),
		mgr.GetEventRecorderFor("PolicyTargetStatus"),
	)

	err = (&TargetStatusReconciler{
		BaseReconciler: targetStatusBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	CreateNamespaceWithContext(ctx, &appNamespace)

	err = ApplyResources(filepath.Join("..", "examples", "toystore", "toystore.yaml"), k8sClient, appNamespace)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

})

var _ = AfterSuite(func(ctx SpecContext) {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	DeleteNamespaceCallbackWithContext(ctx, &appNamespace)
})

func TestMain(m *testing.M) {
	logger := log.NewLogger(
		log.SetLevel(log.DebugLevel),
		log.SetMode(log.ModeDev),
		log.WriteTo(GinkgoWriter),
	).WithName("controller_test")
	log.SetLogger(logger)
	os.Exit(m.Run())
}
