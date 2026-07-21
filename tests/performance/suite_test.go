//go:build performance

package performance

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/log"
	"github.com/kuadrant/kuadrant-operator/tests"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var testNamespace string
var perfLogger logr.Logger

func init() {
	if os.Getenv("PERF_LOG_ERRORS") == "true" {
		perfLogger = log.NewLogger(
			log.SetLevel(log.ErrorLevel),
			log.WriteTo(os.Stderr),
		)
	} else {
		perfLogger = logr.Discard()
	}
	log.SetLogger(perfLogger)
	logf.SetLogger(perfLogger)
	klog.SetLogger(perfLogger)
}

func testClient() client.Client { return k8sClient }

func TestPerformance(t *testing.T) {
	RegisterFailHandler(Fail)

	SetDefaultEventuallyPollingInterval(2 * time.Second)
	SetDefaultEventuallyTimeout(30 * time.Minute)

	RunSpecs(t, "Performance Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: &[]bool{true}[0],
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	s := controllers.BootstrapScheme()

	k8sClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())

	tests.GatewayClassName = os.Getenv("GATEWAYAPI_PROVIDER")
	Expect(tests.GatewayClassName).NotTo(BeZero(), "GATEWAYAPI_PROVIDER must be set")

	ctx := context.Background()
	testNamespace = tests.CreateNamespace(ctx, testClient())
	tests.ApplyKuadrantCR(ctx, testClient(), testNamespace)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 s,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
		Logger:                 perfLogger,
	})
	Expect(err).NotTo(HaveOccurred())

	dClient, err := dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	stateOfTheWorld, err := controllers.NewPolicyMachineryController(mgr, dClient, perfLogger)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = stateOfTheWorld.Start(ctrl.SetupSignalHandler())
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	tests.DeleteNamespace(context.Background(), k8sClient, testNamespace)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
