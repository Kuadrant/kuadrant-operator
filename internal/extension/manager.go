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
	"time"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Manager struct {
	extensions []Extension
	service    extpb.HeartBeatServer
	logger     logr.Logger
}

type Extension interface {
	Start() error
	Stop() error
	Name() string
}

func NewManager(names []string, location string, logger logr.Logger) (Manager, error) {
	var extensions []Extension
	var err error

	service := newHeartBeatService()
	logger = logger.WithName("extension")

	for _, name := range names {
		if oopExtension, e := NewOOPExtension(name, location, service, logger); e != nil {
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
		extensions: extensions,
		service:    service,
		logger:     logger,
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

type heartBeatServer struct {
	extpb.UnimplementedHeartBeatServer
}

func (s *heartBeatServer) Ping(_ context.Context, req *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newHeartBeatService() extpb.HeartBeatServer {
	return &heartBeatServer{}
}
