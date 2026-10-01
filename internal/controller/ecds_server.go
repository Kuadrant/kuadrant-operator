package controllers

import (
	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"

	"github.com/kuadrant/kuadrant-operator/internal/ecds"
)

// ECDSStore is the package-level handle used by the EnvoyFilter reconciler
// to push per-Gateway wasm plugin config to the running ECDS server, the
// same package-var pattern as WasmFileSHA256 above.
var ECDSStore *ecds.Store

func ecdsServerRunnable(logger logr.Logger) controller.RunnableBuilder {
	return func(*controller.Controller) controller.Runnable {
		srv := ecds.NewServer(logger)
		ECDSStore = srv.Store()
		return srv
	}
}
