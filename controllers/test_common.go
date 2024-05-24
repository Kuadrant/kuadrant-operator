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
	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	istioapis "istio.io/istio/operator/pkg/apis"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	maistraapis "github.com/kuadrant/kuadrant-operator/api/external/maistra"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

func SetupKuadrantOperatorForTest(s *runtime.Scheme, cfg *rest.Config) {
	err := kuadrantdnsv1alpha1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1alpha1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1beta1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = kuadrantv1beta2.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = gatewayapiv1.Install(s)
	Expect(err).NotTo(HaveOccurred())

	err = authorinoopapi.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = authorinoapi.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = istioapis.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = istiov1alpha1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = istiosecurityv1beta1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = limitadorv1alpha1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = istioclientnetworkingv1alpha3.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = istioclientgoextensionv1alpha1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = maistraapis.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	err = certmanv1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

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

	policyStatusBaseReconciler := reconcilers.NewBaseReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
		log.Log.WithName("ratelimitpolicy").WithName("status"),
		mgr.GetEventRecorderFor("RateLimitPolicyStatus"),
	)
	err = (&RateLimitPolicyEnforcedStatusReconciler{
		BaseReconciler: policyStatusBaseReconciler,
	}).SetupWithManager(mgr)

	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()
}
