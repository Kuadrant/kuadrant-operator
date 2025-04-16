package main

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"

	"go.uber.org/zap/zapcore"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme = k8sruntime.NewScheme()
	logger = zap.New(
		zap.Level(zapcore.DebugLevel),
		zap.UseDevMode(false),
		zap.WriteTo(os.Stderr),
	).WithName("test-extension")
)

func init() {
	utilruntime.Must(corev1.AddToScheme(scheme))

	ctrl.SetLogger(logger)
	klog.SetLogger(logger)
}

func main() {
	options := ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	client, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		logger.Error(err, "unable to create client")
		os.Exit(1)
	}

	exampleReconciler := controllers.NewExampleReconciler(client, logger)
	if err = exampleReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to setup extension reconciler")
	}

	logger.Info("starting example-controller")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
