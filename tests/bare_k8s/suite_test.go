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

package bare_k8s_test

import (
	"encoding/json"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

// This test suite will be run on bare k8s env without GatewayAPI CRDs, just Kuadrant CRDs installed

var k8sClient client.Client
var testEnv *envtest.Environment

func testClient() client.Client { return k8sClient }

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller suite on bare k8s")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: &[]bool{true}[0],
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	controllers.SetupKuadrantOperatorForTest(controllers.BootstrapScheme(), cfg)

	data := controllers.MarshalConfig(cfg)

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

	// Create new scheme for each client
	s := controllers.BootstrapScheme()

	// Set the shared configuration
	var err error
	k8sClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = SynchronizedAfterSuite(func() {}, func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func TestMain(m *testing.M) {
	logger := log.NewLogger(
		log.SetLevel(log.DebugLevel),
		log.SetMode(log.ModeDev),
		log.WriteTo(GinkgoWriter),
	).WithName("bare_k8s_controller_test")
	log.SetLogger(logger)
	os.Exit(m.Run())
}
