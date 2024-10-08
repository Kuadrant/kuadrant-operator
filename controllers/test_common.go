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
	"encoding/json"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	consolev1 "github.com/openshift/api/console/v1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	istioapis "istio.io/istio/operator/pkg/apis"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	maistraapis "github.com/kuadrant/kuadrant-operator/api/external/maistra"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

func SetupKuadrantOperatorForTest(s *runtime.Scheme, cfg *rest.Config) {
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 s,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).ToNot(HaveOccurred())

	err = fieldindexers.HTTPRouteIndexByGateway(
		mgr,
		log.Log.WithName("kuadrant").WithName("indexer").WithName("routeIndexByGateway"),
	)
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
		RestMapper:          mgr.GetRESTMapper(),
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
		RestMapper:     mgr.GetRESTMapper(),
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

	rateLimitingIstioWASMPluginBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("wasmplugin"),
		mgr.GetEventRecorderFor("RateLimitingIstioWASMPlugin"),
	)

	err = (&RateLimitingIstioWASMPluginReconciler{
		BaseReconciler: rateLimitingIstioWASMPluginBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	authPolicyIstioAuthorizationPolicyReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("authpolicy").WithName("istioauthorizationpolicy"),
		mgr.GetEventRecorderFor("AuthPolicyIstioAuthorizationPolicy"),
	)

	err = (&AuthPolicyIstioAuthorizationPolicyReconciler{
		BaseReconciler: authPolicyIstioAuthorizationPolicyReconciler,
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

	policyStatusBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("status"),
		mgr.GetEventRecorderFor("RateLimitPolicyStatus"),
	)
	err = (&RateLimitPolicyEnforcedStatusReconciler{
		BaseReconciler: policyStatusBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	authPolicyEnvoySecurityPolicyReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("authpolicy").WithName("securitypolicy"),
		mgr.GetEventRecorderFor("AuthPolicyEnvoySecurityPolicy"),
	)

	err = (&AuthPolicyEnvoySecurityPolicyReconciler{
		BaseReconciler: authPolicyEnvoySecurityPolicyReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	envoySecurityPolicyReferenceGrantReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("authpolicy").WithName("referencegrant"),
		mgr.GetEventRecorderFor("EnvoySecurityPolicyReferenceGrant"),
	)

	err = (&EnvoySecurityPolicyReferenceGrantReconciler{
		BaseReconciler: envoySecurityPolicyReferenceGrantReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	envoyGatewayWasmReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("envoyGatewayWasmReconciler"),
		mgr.GetEventRecorderFor("EnvoyGatewayWasmReconciler"),
	)

	err = (&EnvoyGatewayWasmReconciler{
		BaseReconciler: envoyGatewayWasmReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	envoyGatewayLimitadorClusterReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("envoyGatewayLimitadorClusterReconciler"),
		mgr.GetEventRecorderFor("EnvoyGatewayLimitadorClusterReconciler"),
	)

	err = (&EnvoyGatewayLimitadorClusterReconciler{
		BaseReconciler: envoyGatewayLimitadorClusterReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	dClient, err := dynamic.NewForConfig(mgr.GetConfig())
	Expect(err).NotTo(HaveOccurred())

	stateOfTheWorld := NewPolicyMachineryController(mgr, dClient, log.Log)

	go func() {
		defer GinkgoRecover()
		err = stateOfTheWorld.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()
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
		kuadrantdnsv1alpha1.AddToScheme,
		kuadrantv1alpha1.AddToScheme,
		kuadrantv1beta1.AddToScheme,
		kuadrantv1beta2.AddToScheme,
		kuadrantv1beta3.AddToScheme,
		gatewayapiv1.Install,
		gatewayapiv1beta1.Install,
		authorinoopapi.AddToScheme,
		authorinoapi.AddToScheme,
		istioapis.AddToScheme,
		istiov1alpha1.AddToScheme,
		istiosecurityv1beta1.AddToScheme,
		limitadorv1alpha1.AddToScheme,
		istioclientnetworkingv1alpha3.AddToScheme,
		istioclientgoextensionv1alpha1.AddToScheme,
		certmanv1.AddToScheme,
		maistraapis.AddToScheme,
		egv1alpha1.AddToScheme,
		consolev1.AddToScheme,
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
