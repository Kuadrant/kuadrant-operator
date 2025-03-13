//go:build integration

package envoygateway_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/log"
	"github.com/kuadrant/kuadrant-operator/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var kuadrantInstallationNS string

func testClient() client.Client { return k8sClient }

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite on Envoy Gateway")
}

const (
	TestGatewayName   = "test-placed-gateway"
	TestHTTPRouteName = "toystore-route"
)

var _ = SynchronizedBeforeSuite(func() []byte {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: &[]bool{true}[0],
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	s := controllers.BootstrapScheme()
	controllers.SetupKuadrantOperatorForTest(s, cfg)

	k8sClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	ctx := context.Background()
	ns := tests.CreateNamespace(ctx, testClient())
	tests.ApplyKuadrantCR(ctx, testClient(), ns)

	data := controllers.MarshalConfig(cfg, controllers.WithKuadrantInstallNS(ns))

	return data
}, func(data []byte) {
	// Unmarshal the shared configuration struct
	var sharedCfg controllers.SharedConfig
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
	s := controllers.BootstrapScheme()

	// Set the shared configuration
	var err error
	k8sClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	tests.GatewayClassName = os.Getenv("GATEWAYAPI_PROVIDER")
	Expect(tests.GatewayClassName).To(Equal("envoygateway"), "Please make sure GATEWAYAPI_PROVIDER is set correctly.")
})

var _ = SynchronizedAfterSuite(func() {}, func() {
	By("tearing down the test environment")
	tests.DeleteNamespace(context.Background(), k8sClient, kuadrantInstallationNS)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func TestMain(m *testing.M) {
	logger := log.NewLogger(
		log.SetLevel(log.DebugLevel),
		log.SetMode(log.ModeDev),
		log.WriteTo(GinkgoWriter),
	).WithName("envoygateway_controller_test")
	log.SetLogger(logger)
	os.Exit(m.Run())
}
