/*
Copyright 2025 Red Hat, Inc.

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

package extension

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"
)

type Manager struct {
	extensions []Extension
	service    extpb.ExtensionServiceServer
	dag        *nilGuardedPointer[StateAwareDAG]
	logger     logr.Logger
	sync       io.Writer
}

type Extension interface {
	Start() error
	Stop() error
	Name() string
}

func NewManager(names []string, location string, logger logr.Logger, sync io.Writer) (Manager, error) {
	var extensions []Extension
	var err error

	service := newExtensionService(BlockingDAG)
	logger = logger.WithName("extension")

	for _, name := range names {
		if oopExtension, e := NewOOPExtension(name, location, service, logger, sync); e == nil {
			extensions = append(extensions, &oopExtension)
		} else {
			if err == nil {
				err = fmt.Errorf("%s: %w", name, e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, name, e)
			}
		}
	}

	return Manager{
		extensions,
		service,
		BlockingDAG,
		logger,
		sync,
	}, err
}

func (m *Manager) Start() error {
	var err error

	for _, extension := range m.extensions {
		if e := extension.Start(); e != nil {
			if err == nil {
				err = fmt.Errorf("%s: %w", extension.Name(), e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, extension.Name(), e)
			}
		}
	}

	return err
}

func (m *Manager) Stop() error {
	var err error

	for _, extension := range m.extensions {
		if e := extension.Stop(); e != nil {
			if err == nil {
				err = fmt.Errorf("%s: %w", extension.Name(), e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, extension.Name(), e)
			}
		}
	}

	return err
}

func (m *Manager) Run(stopCh <-chan struct{}) {
	if err := m.Start(); err != nil {
		m.logger.Error(err, "unable to start extension manager")
		os.Exit(1)
	}
	go func() {
		<-stopCh
		if err := m.Stop(); err != nil {
			m.logger.Error(err, "unable to stop extension manager")
		}
	}()
}

func (m *Manager) HasSynced() bool {
	return true
}

type extensionService struct {
	dag *nilGuardedPointer[StateAwareDAG]
	extpb.UnimplementedExtensionServiceServer
}

func (s *extensionService) Ping(_ context.Context, _ *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newExtensionService(dag *nilGuardedPointer[StateAwareDAG]) extpb.ExtensionServiceServer {
	return &extensionService{dag: dag}
}

func (d *StateAwareDAG) FindGatewaysFor(targetRefs []*extpb.TargetRef) ([]*extpb.Gateway, error) {
	chain := d.topology.Objects().Items(func(o machinery.Object) bool {
		return len(lo.Filter(targetRefs, func(t *extpb.TargetRef, _ int) bool {
			return t.Name == o.GetName() && t.Kind == o.GroupVersionKind().Kind && t.Group == o.GroupVersionKind().Group
		})) > 0
	})

	gateways := make([]*extpb.Gateway, 0)
	chainSize := len(chain)

	for i := 0; i < chainSize; i++ {
		object := chain[i]
		parents := d.topology.Objects().Parents(object)
		chain = append(chain, parents...)
		chainSize = len(chain)
		if gw, ok := object.(*machinery.Gateway); ok && gw != nil {
			gateways = append(gateways, toGw(*gw))
		}
	}

	return gateways, nil
}

func toGw(gw machinery.Gateway) *extpb.Gateway {
	return &extpb.Gateway{
		Metadata: &extpb.Metadata{
			Name:      gw.Gateway.Name,
			Namespace: gw.Gateway.Namespace,
		},
		GatewayClassName: string(gw.Gateway.Spec.GatewayClassName),
		Listeners:        toListeners(gw.Gateway.Spec.Listeners),
	}
}

func toListeners(listeners []v1.Listener) []*extpb.Listener {
	ls := make([]*extpb.Listener, len(listeners))
	for i, l := range listeners {
		listener := extpb.Listener{}
		if l.Hostname != nil {
			listener.Hostname = string(*l.Hostname)
		}
		ls[i] = &listener
	}
	return ls
}

func (s *extensionService) Subscribe(_ *emptypb.Empty, stream grpc.ServerStreamingServer[extpb.Event]) error {
	for {
		time.Sleep(time.Second * 5)
		if err := stream.Send(&extpb.Event{}); err != nil {
			return err
		}
	}
}
