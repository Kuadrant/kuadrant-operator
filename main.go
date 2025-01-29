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

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	consolev1 "github.com/openshift/api/console/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	istioextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/controllers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = k8sruntime.NewScheme()
	logLevel = env.GetString("LOG_LEVEL", "info")
	logMode  = env.GetString("LOG_MODE", "production")
	gitSHA   string // value injected in compilation-time
	dirty    string // value injected in compilation-time
	version  string // value injected in compilation-time
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(limitadorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authorinoopapi.AddToScheme(scheme))
	utilruntime.Must(authorinoapi.AddToScheme(scheme))
	utilruntime.Must(istionetworkingv1alpha3.AddToScheme(scheme))
	utilruntime.Must(gatewayapiv1.Install(scheme))
	utilruntime.Must(istioextensionv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1beta1.AddToScheme(scheme))
	utilruntime.Must(kuadrantdnsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(certmanv1.AddToScheme(scheme))
	utilruntime.Must(egv1alpha1.AddToScheme(scheme))
	utilruntime.Must(consolev1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	logger := log.NewLogger(
		log.SetLevel(log.ToLevel(logLevel)),
		log.SetMode(log.ToMode(logMode)),
		log.WriteTo(os.Stdout),
	).WithName("kuadrant-operator")
	log.SetLogger(logger)
}

func printControllerMetaInfo() {
	setupLog := log.Log

	setupLog.Info(fmt.Sprintf("go version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("go os/arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	setupLog.Info("base logger", "log level", logLevel, "log mode", logMode)
	setupLog.Info("build information", "version", version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	printControllerMetaInfo()

	setupLog := log.Log

	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		err                  error
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	options := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f139389e.kuadrant.io",
	}

	if env.GetString("OPERATOR_NAMESPACE", "") == "" {
		panic("OPERATOR_NAMESPACE environment variable must be set")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	client, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create client")
		os.Exit(1)
	}

	stateOfTheWorld := controllers.NewPolicyMachineryController(mgr, client, log.Log)
	if err = stateOfTheWorld.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "unable to start stateOfTheWorld controller")
		os.Exit(1)
	}
}
