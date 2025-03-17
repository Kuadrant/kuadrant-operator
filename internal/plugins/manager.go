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

package plugins

import (
	"context"
	"fmt"
	"time"

	extension "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const DefaultPluginsDir = "/plugins"

type PluginManager struct {
	plugins []Plugin
	service extension.HeartBeatServer
	logger  logr.Logger
}

type Plugin interface {
	Start() error
	Stop() error
	Name() string
}

func NewPluginManager(names []string, location string, logger logr.Logger) (PluginManager, error) {
	var plugins []Plugin
	var err error

	service := newHeartBeatService()
	logger = logger.WithName("plugins")

	for _, name := range names {
		if embeddedPlugin, e := NewEmbeddedPlugin(name, location, service, logger); e != nil {
			plugins = append(plugins, &embeddedPlugin)
		} else {
			if err == nil {
				err = fmt.Errorf("%s: %w", name, e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, name, e)
			}
		}
	}

	return PluginManager{
		plugins: plugins,
		service: service,
		logger:  logger,
	}, err
}

func (m *PluginManager) Start() error {
	var err error

	for _, plugin := range m.plugins {
		if e := plugin.Start(); e != nil {
			if err == nil {
				err = fmt.Errorf("%s: %w", plugin.Name(), e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, plugin.Name(), e)
			}
		}
	}

	return err
}

func (m *PluginManager) Stop() error {
	var err error

	for _, plugin := range m.plugins {
		if e := plugin.Stop(); e != nil {
			if err == nil {
				err = fmt.Errorf("%s: %w", plugin.Name(), e)
			} else {
				err = fmt.Errorf("%w; %s: %w", err, plugin.Name(), e)
			}
		}
	}

	return err
}

type heartBeatServer struct {
	extension.UnimplementedHeartBeatServer
}

func (s *heartBeatServer) Ping(_ context.Context, req *extension.PingRequest) (*extension.PongResponse, error) {
	return &extension.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newHeartBeatService() extension.HeartBeatServer {
	return &heartBeatServer{}
}
