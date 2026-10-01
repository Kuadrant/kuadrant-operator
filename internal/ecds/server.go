package ecds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	extensionv3 "github.com/envoyproxy/go-control-plane/envoy/service/extension/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"k8s.io/utils/env"
)

// DefaultECDSServerPort is the default ECDS gRPC server port, used when
// ECDS_SERVICE_PORT is unset.
const DefaultECDSServerPort = 50053

// ServerClusterName is the Envoy CLUSTER name this server is reached by,
// referenced from the EnvoyFilter's config_discovery patch.
const ServerClusterName = "kuadrant-ecds"

// Server is a minimal Envoy ExtensionConfigDiscoveryService (ECDS) server.
// Registers only the ECDS gRPC service (not the full ADS/LDS/CDS/RDS/EDS
// surface go-control-plane's generic Server interface supports) - any other
// xDS RPC Envoy might send gets gRPC's standard "unknown service" error,
// which is the intended, minimal surface for this PoC.
type Server struct {
	store      *Store
	grpcServer *grpc.Server
	logger     logr.Logger
}

func NewServer(logger logr.Logger) *Server {
	return &Server{
		store:  NewStore(),
		logger: logger.WithName("ECDSServer"),
	}
}

// Store returns the snapshot store callers should Push updates to.
func (s *Server) Store() *Store {
	return s.store
}

func (s *Server) Run(stopCh <-chan struct{}) {
	port, err := env.GetInt("ECDS_SERVICE_PORT", DefaultECDSServerPort)
	if err != nil {
		s.logger.Error(err, "invalid ECDS_SERVICE_PORT, using default", "default", DefaultECDSServerPort)
		port = DefaultECDSServerPort
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.logger.Error(err, "failed to listen", "port", port)
		return
	}

	xdsServer := serverv3.NewServer(context.Background(), s.store.Cache(), serverv3.CallbackFuncs{})

	grpcServer := grpc.NewServer()
	extensionv3.RegisterExtensionConfigDiscoveryServiceServer(grpcServer, xdsServer)
	s.grpcServer = grpcServer

	go func() {
		s.logger.Info("starting ECDS server", "port", port)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.logger.Error(err, "ECDS server failed")
		}
	}()

	<-stopCh

	s.logger.Info("stopping ECDS server")
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("ECDS server stopped gracefully")
	case <-time.After(5 * time.Second):
		s.logger.Info("ECDS server graceful stop timed out, forcing stop")
		grpcServer.Stop()
	}
}

func (s *Server) HasSynced() bool {
	return true
}
