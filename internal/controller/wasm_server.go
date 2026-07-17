package controllers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/utils/env"
)

const (
	defaultWasmServerPort = 8082
	WasmServerClusterName = "kuadrant-operator-wasm"
)

var wasmFilePath = env.GetString("WASM_SERVER_FILE_PATH", "/wasm/plugin.wasm")

var WasmFileSHA256 string

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

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

	sha, err := computeFileSHA256(wasmFilePath)
	if err != nil {
		s.logger.Error(err, "failed to compute wasm file SHA256", "path", wasmFilePath)
		return
	}
	WasmFileSHA256 = sha

	mux := http.NewServeMux()
	mux.HandleFunc("GET /plugin.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, wasmFilePath)
	})

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		Protocols:         &http.Protocols{},
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.server.Protocols.SetHTTP1(true)
	s.server.Protocols.SetUnencryptedHTTP2(true)

	go func() {
		s.logger.Info("starting wasm server (h2c)", "port", port, "file", wasmFilePath, "sha256", WasmFileSHA256)
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
