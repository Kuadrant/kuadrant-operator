package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/utils/env"
)

const defaultWasmServerPort = 8082

var wasmFilePath = env.GetString("WASM_SERVER_FILE_PATH", "/wasm/plugin.wasm")

type WasmServer struct {
	server *http.Server
	logger logr.Logger
}

func NewWasmServer(logger logr.Logger) *WasmServer {
	return &WasmServer{logger: logger.WithName("WasmServer")}
}

func (s *WasmServer) Run(stopCh <-chan struct{}) {
	port, err := env.GetInt("WASM_SERVER_PORT", defaultWasmServerPort)
	if err != nil {
		s.logger.Error(err, "invalid WASM_SERVER_PORT, using default", "default", defaultWasmServerPort)
		port = defaultWasmServerPort
	}

	if _, err := os.Stat(wasmFilePath); err != nil {
		s.logger.Error(err, "wasm file not found, server will not start", "path", wasmFilePath)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /plugin.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, wasmFilePath)
	})

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		s.logger.Info("starting wasm server", "port", port, "file", wasmFilePath)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error(err, "wasm server failed")
		}
	}()

	<-stopCh

	s.logger.Info("stopping wasm server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error(err, "wasm server shutdown error")
	}
}

func (s *WasmServer) HasSynced() bool {
	return true
}

func wasmServerRunnable(logger logr.Logger) controller.RunnableBuilder {
	return func(*controller.Controller) controller.Runnable {
		return NewWasmServer(logger)
	}
}
