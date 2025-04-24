package main

import (
	"os"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/extensioncontroller"
)

var (
	scheme      = k8sruntime.NewScheme()
	logLevel, _ = zapcore.ParseLevel(env.GetString("LOG_LEVEL", "info"))
	logMode     = env.GetString("LOG_MODE", "production") != "production"
	logger      = zap.New(
		zap.Level(logLevel),
		zap.UseDevMode(logMode),
		zap.WriteTo(os.Stderr),
	).WithName("test-extension")
)

func init() {
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1.AddToScheme(scheme))

	ctrl.SetLogger(logger)
	klog.SetLogger(logger)
}

func main() {
	// todo(adam-cattermole): We should abstract all of this away from the user, it should be as simple as implementing
	//   the reconcile function, providing a name, and a list of watches for their extension
	// build the extension client from socket
	if len(os.Args) < 1 {
		logger.Error(nil, "no command line argument provided")
		os.Exit(1)
	}
	socketPath := os.Args[1]

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

	exampleReconciler := controllers.NewExampleReconciler(client)
	controller, err := extensioncontroller.NewExtensionControllerBuilder().
		WithName("example-extension-controller").
		WithManager(mgr).
		WithLogger(logger).
		WithClient(client).
		WithReconciler(exampleReconciler.Reconcile).
		WithSocketPath(socketPath).
		Watches(&kuadrantv1.AuthPolicy{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}

	logger.Info("starting example-controller")
	if err = controller.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
