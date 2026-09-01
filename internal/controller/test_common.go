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

// This is a test file to be imported from other golang packages
// Thus, it cannot be named _test.go
// go build tag "integration" prevents from being included in the final build.

package controllers

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurity "istio.io/client-go/pkg/apis/security/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/controlplane"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func SetupKuadrantOperatorForTest(s *runtime.Scheme, cfg *rest.Config) {
	ctx := context.Background()
	logger := log.Log

	operatorNamespace := "kuadrant-system"

	// Pre-manager: bootstrap child operator CRDs (mirrors cmd/main.go startup)
	deployer, err := controlplane.NewDeployer(cfg, operatorNamespace, logger)
	Expect(err).ToNot(HaveOccurred())
	Expect(deployer.ApplyCRDsForComponents(ctx, deployer.EnabledComponents())).To(Succeed())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 s,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).ToNot(HaveOccurred())

	// Register KuadrantControlPlane controller
	cpReconciler := controlplane.NewReconciler(mgr.GetClient(), deployer, mgr.GetEventRecorder("kuadrant-control-plane"), logger)
	Expect(cpReconciler.SetupWithManager(mgr)).To(Succeed())

	// Register bootstrap runnable (creates default KuadrantControlPlane CR + OLM cleanup)
	Expect(mgr.Add(controlplane.NewBootstrapRunnable(cfg, s, mgr.GetEventRecorder("kuadrant-control-plane"), "kuadrant-system", logger))).To(Succeed())

	dClient, err := dynamic.NewForConfig(mgr.GetConfig())
	Expect(err).NotTo(HaveOccurred())

	stateOfTheWorld, err := NewPolicyMachineryController(mgr, dClient, log.Log)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = stateOfTheWorld.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	// Wait for control plane to be ready before tests start.
	By("waiting for KuadrantControlPlane to be ready")
	waitClient, err := client.New(cfg, client.Options{Scheme: s})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func(g Gomega) {
		cp := &kuadrantv1alpha1.KuadrantControlPlane{}
		g.Expect(waitClient.Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())
		g.Expect(cp.Status.ObservedGeneration).To(BeNumerically(">", 0))
		cond := meta.FindStatusCondition(cp.Status.Conditions, kuadrantv1alpha1.ControlPlaneConditionReady)
		g.Expect(cond).ToNot(BeNil())
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

// SharedConfig contains minimum cluster connection config that can be safely marshalled as rest.Config is unsafe to marshall
type SharedConfig struct {
	Host                  string          `json:"host"`
	TLSClientConfig       TLSClientConfig `json:"tlsClientConfig"`
	KuadrantNS            string          `json:"kuadrantNS"`
	DNSProviderSecretName string          `json:"dnsProviderSecretName"`
}

type TLSClientConfig struct {
	Insecure bool    `json:"insecure"`
	CertData []uint8 `json:"certData,omitempty"`
	KeyData  []uint8 `json:"keyData,omitempty"`
	CAData   []uint8 `json:"caData,omitempty"`
}

func BootstrapScheme() *runtime.Scheme {
	s := runtime.NewScheme()

	sb := runtime.NewSchemeBuilder(
		scheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		kuadrantdnsv1alpha1.AddToScheme,
		kuadrantv1.AddToScheme,
		kuadrantv1alpha1.AddToScheme,
		kuadrantv1beta1.AddToScheme,
		gatewayapiv1.Install,
		authorinoopapi.AddToScheme,
		authorinoapi.AddToScheme,
		limitadorv1alpha1.AddToScheme,
		istioclientnetworkingv1alpha3.AddToScheme,
		istioclientgoextensionv1alpha1.AddToScheme,
		certmanv1.AddToScheme,
		egv1alpha1.AddToScheme,
		configv1.AddToScheme,
		consolev1.AddToScheme,
		monitoringv1.AddToScheme,
		istiosecurity.AddToScheme,
	)

	err := sb.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	return s
}

// MarshalConfig marshals the config to a shared configuration struct
func MarshalConfig(cfg *rest.Config, opts ...func(config *SharedConfig)) []byte {
	sharedCfg := &SharedConfig{
		Host: cfg.Host,
		TLSClientConfig: TLSClientConfig{
			Insecure: cfg.TLSClientConfig.Insecure,
			CertData: cfg.TLSClientConfig.CertData,
			KeyData:  cfg.TLSClientConfig.KeyData,
			CAData:   cfg.TLSClientConfig.CAData,
		},
	}

	for _, opt := range opts {
		opt(sharedCfg)
	}

	data, err := json.Marshal(sharedCfg)
	Expect(err).NotTo(HaveOccurred())

	return data
}

func WithKuadrantInstallNS(ns string) func(config *SharedConfig) {
	return func(cfg *SharedConfig) {
		cfg.KuadrantNS = ns
	}
}
