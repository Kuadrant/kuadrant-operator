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
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/kuadrant/policy-machinery/controller"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap/zapcore"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	istioextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurity "istio.io/client-go/pkg/apis/security/v1"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/internal/log"
	"github.com/kuadrant/kuadrant-operator/internal/metrics"
	kuadrantOtel "github.com/kuadrant/kuadrant-operator/internal/otel"
	"github.com/kuadrant/kuadrant-operator/internal/trace"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = k8sruntime.NewScheme()
	logLevel = env.GetString("LOG_LEVEL", "info")
	logMode  = env.GetString("LOG_MODE", "production")
	gitSHA   string              // value injected in compilation-time
	dirty    string              // value injected in compilation-time
	version  string              // value injected in compilation-time
	sync     zapcore.WriteSyncer // logger output sync
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
	utilruntime.Must(kuadrantv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1beta1.AddToScheme(scheme))
	utilruntime.Must(kuadrantdnsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(certmanv1.AddToScheme(scheme))
	utilruntime.Must(egv1alpha1.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	utilruntime.Must(consolev1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(istiosecurity.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	sync = zapcore.Lock(zapcore.AddSync(os.Stdout))

	// Create Zap logger (always used for console output)
	logger := log.NewLogger(
		log.SetLevel(log.ToLevel(logLevel)),
		log.SetMode(log.ToMode(logMode)),
		log.WriteTo(sync),
	).WithName("kuadrant-operator")
	log.SetLogger(logger)
	log.SetSync(sync)
}

func printControllerMetaInfo() {
	setupLog := log.Log

	setupLog.Info(fmt.Sprintf("go version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("go os/arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	setupLog.Info("base logger", "log level", logLevel, "log mode", logMode)
	setupLog.Info("build information", "version", version, "commit", gitSHA, "dirty", dirty)
}

func main() {
	var opts []controller.ControllerOption

	// Setup OpenTelemetry if enabled
	otelConfig := kuadrantOtel.NewConfig(gitSHA, dirty, version)

	// Setup OTel logging if endpoint is configured
	if otelConfig.LogsEndpoint() != "" {
		otelLogger, err := log.SetupOTelLogging(
			context.Background(),
			otelConfig,
			log.ToLevel(logLevel),
			log.ToMode(logMode),
			sync,
		)
		if err != nil {
			log.Log.Error(err, "Failed to setup OpenTelemetry logging, continuing with Zap only")
		} else {
			log.Log.Info("OpenTelemetry logging enabled",
				"endpoint", otelConfig.LogsEndpoint(),
				"gitSHA", gitSHA,
				"dirty", dirty)

			// Use OTel logger which internally uses:
			// - Zap exporter for console (respects LOG_LEVEL/LOG_MODE)
			// - OTLP exporter for remote collection
			log.SetLogger(otelLogger)

			// Ensure OTel logging is shut down gracefully on exit
			defer func() {
				// Create a fresh context with timeout for shutdown
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				if err := log.ShutdownOTelLogging(shutdownCtx); err != nil {
					log.Log.Error(err, "Failed to shutdown OpenTelemetry logging")
				}
			}()
		}
	}

	// Setup OTel tracing if endpoint is configured
	if otelConfig.TracesEndpoint() != "" {
		traceProvider, err := trace.NewProvider(context.Background(), otelConfig)
		if err != nil {
			log.Log.Error(err, "Failed to setup OpenTelemetry tracing, continuing without traces")
		} else {
			log.Log.Info("OpenTelemetry tracing enabled",
				"endpoint", otelConfig.TracesEndpoint(),
				"sampler", "AlwaysSample",
			)

			// Set global propagator for distributed tracing
			otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			))

			// Set as global tracer provider
			otel.SetTracerProvider(traceProvider.TracerProvider())

			// Ensure OTel tracing is shut down gracefully on exit
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				if err := traceProvider.Shutdown(shutdownCtx); err != nil {
					log.Log.Error(err, "Failed to shutdown OpenTelemetry tracing")
				}
			}()

			opts = append(opts, controller.WithTracer(traceProvider.TracerProvider().Tracer(otelConfig.ServiceName)))
		}
	}

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

	// Initialize OpenTelemetry metrics provider
	// This bridges all Prometheus metrics (controller-runtime + custom metrics)
	// to OTLP export without requiring any changes to existing metrics code
	metricsConfig := metrics.NewConfig(ctrlmetrics.Registry)
	metricsProvider, err := metrics.NewProvider(context.Background(), otelConfig, metricsConfig)
	if err != nil {
		setupLog.Error(err, "unable to create metrics provider")
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metricsProvider.Shutdown(shutdownCtx); err != nil {
			setupLog.Error(err, "failed to shutdown metrics provider")
		}
	}()

	if metricsProvider.IsOTLPEnabled() {
		setupLog.Info("OpenTelemetry metrics export enabled",
			"endpoint", otelConfig.MetricsEndpoint(),
			"interval", metricsConfig.ExportInterval,
		)
	}

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

	stateOfTheWorld, err := controllers.NewPolicyMachineryController(mgr, client, log.Log, opts...)
	if err != nil {
		setupLog.Error(err, "unable to setup policy controller")
		os.Exit(1)
	}
	if err = stateOfTheWorld.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "unable to start stateOfTheWorld controller")
		os.Exit(1)
	}
}
