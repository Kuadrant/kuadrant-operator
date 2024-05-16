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

package limitador

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	authpolicy2 "github.com/kuadrant/kuadrant-operator/controllers/authpolicy"
	"github.com/kuadrant/kuadrant-operator/controllers/dnspolicy"
	kuadrantctlr "github.com/kuadrant/kuadrant-operator/controllers/kuadrant"
	"github.com/kuadrant/kuadrant-operator/controllers/ratelimit"
	"github.com/kuadrant/kuadrant-operator/controllers/targetstatus"
	"github.com/kuadrant/kuadrant-operator/controllers/tlspolicy"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/test"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var k8sClient client.Client
var testEnv *envtest.Environment
var kuadrantInstallationNS string

func testClient() client.Client { return k8sClient }

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Limitador Controller Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	cfg, client, env, s := test.BootstrapTestEnv()
	k8sClient = client
	testEnv = env

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 s,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).ToNot(HaveOccurred())

	authPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("authpolicy"),
		mgr.GetEventRecorderFor("AuthPolicy"),
	)

	err = (&authpolicy2.Reconciler{
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

	err = (&ratelimit.Reconciler{
		BaseReconciler:      rateLimitPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	tlsPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("tlspolicy"),
		mgr.GetEventRecorderFor("TLSPolicy"),
	)

	err = (&tlspolicy.Reconciler{
		BaseReconciler:      tlsPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	dnsPolicyBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("dnspolicy"),
		mgr.GetEventRecorderFor("DNSPolicy"),
	)

	err = (&dnspolicy.Reconciler{
		BaseReconciler:      dnsPolicyBaseReconciler,
		TargetRefReconciler: reconcilers.TargetRefReconciler{Client: mgr.GetClient()},
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	kuadrantBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("kuadrant-controller"),
		mgr.GetEventRecorderFor("Kuadrant"),
	)

	err = (&kuadrantctlr.Reconciler{
		BaseReconciler: kuadrantBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	limitadorClusterEnvoyFilterBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("envoyfilter"),
		mgr.GetEventRecorderFor("LimitadorClusterEnvoyFilter"),
	)

	err = (&ClusterEnvoyFilterReconciler{
		BaseReconciler: limitadorClusterEnvoyFilterBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	gatewayKuadrantBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("kuadrant").WithName("gateway"),
		mgr.GetEventRecorderFor("GatewayKuadrant"),
	)

	err = (&kuadrantctlr.GatewayKuadrantReconciler{
		BaseReconciler: gatewayKuadrantBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	rateLimitingWASMPluginBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("wasmplugin"),
		mgr.GetEventRecorderFor("RateLimitingWASMPlugin"),
	)

	err = (&ratelimit.RateLimitingWASMPluginReconciler{
		BaseReconciler: rateLimitingWASMPluginBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	targetStatusBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("targetstatus"),
		mgr.GetEventRecorderFor("PolicyTargetStatus"),
	)

	err = (&targetstatus.Reconciler{
		BaseReconciler: targetStatusBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	policyStatusBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("status"),
		mgr.GetEventRecorderFor("RateLimitPolicyStatus"),
	)
	err = (&ratelimit.PolicyEnforcedStatusReconciler{
		BaseReconciler: policyStatusBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	ns := test.CreateNamespaceWithContext(context.Background(), k8sClient)
	test.ApplyKuadrantCR(k8sClient, ns)

	data := test.MarshalConfig(cfg, test.WithKuadrantInstallNS(ns))

	return data
}, func(data []byte) {
	// Unmarshal the shared configuration struct
	var sharedCfg test.SharedConfig
	Expect(json.Unmarshal(data, &sharedCfg)).To(Succeed())

	// Create the rest.Config object from the shared configuration
	cfg := &rest.Config{
		Host: sharedCfg.Host,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: sharedCfg.TLSClientConfig.Insecure,
			CertData: sharedCfg.TLSClientConfig.CertData,
			KeyData:  sharedCfg.TLSClientConfig.KeyData,
			CAData:   sharedCfg.TLSClientConfig.CAData,
		},
	}

	kuadrantInstallationNS = sharedCfg.KuadrantNS

	// Create new scheme for each client
	s := test.BootstrapScheme()

	// Set the shared configuration
	var err error
	k8sClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = SynchronizedAfterSuite(func() {}, func() {
	By("tearing down the test environment")
	test.DeleteNamespaceCallbackWithContext(context.Background(), k8sClient, kuadrantInstallationNS)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
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
