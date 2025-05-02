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
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/cel/ext"
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
	dag           *nilGuardedPointer[StateAwareDAG]
	mutex         sync.Mutex
	subscriptions map[string]subscription
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

func (s *extensionService) Subscribe(_ *emptypb.Empty, stream grpc.ServerStreamingServer[extpb.Event]) error {
	channel := BlockingDAG.newUpdateChannel()
	for {
		dag := <-channel
		opts := []cel.EnvOption{
			kuadrant.CelExt(&dag),
		}

		s.mutex.Lock()
		if env, err := cel.NewEnv(opts...); err == nil {
			for key, sub := range s.subscriptions {
				if prg, err := env.Program(sub.cAst); err == nil {
					if newVal, _, err := prg.Eval(sub.input); err == nil {
						if newVal != sub.val {
							sub.val = newVal
							s.subscriptions[key] = sub
							if err := stream.Send(&extpb.Event{}); err != nil {
								s.mutex.Unlock()
								return err
							}
						}
					}
				}
			}
		}
		s.mutex.Unlock()
	}
}

func (s *extensionService) Resolve(_ context.Context, request *extpb.ResolveRequest) (*extpb.ResolveResponse, error) {
	dag, success := s.dag.getWaitWithTimeout(15 * time.Second)
	if !success {
		return nil, fmt.Errorf("unable to get to a dag in time")
	}

	opts := []cel.EnvOption{
		kuadrant.CelExt(dag),
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	pAst, issues := env.Parse(request.Expression)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	cAst, issues := env.Check(pAst)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	prg, err := env.Program(cAst)
	if err != nil {
		return nil, err
	}

	input := map[string]any{
		"self": request.Policy,
	}

	val, _, err := prg.Eval(input)

	if request.Subscribe {
		key := ""
		for _, targetRef := range request.Policy.TargetRefs {
			key += fmt.Sprintf("[%s/%s]%s/%s#%s\n", targetRef.Group, targetRef.Kind, targetRef.Namespace, targetRef.Name, targetRef.SectionName)
		}
		key += request.Expression
		s.mutex.Lock()
		s.subscriptions[key] = subscription{
			cAst,
			input,
			val,
		}
		s.mutex.Unlock()
	}

	if err != nil {
		return nil, err
	}

	// FIXME: This probably be sent back as a real `CelValue` as protobuf
	value, err := val.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return nil, err
	}
	return &extpb.ResolveResponse{
		CelResult: value.(string),
	}, nil
}

type subscription struct {
	cAst  *cel.Ast
	input map[string]any
	val   ref.Val
}
