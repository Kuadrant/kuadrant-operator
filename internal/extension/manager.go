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
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/cel/ext"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
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
	dag            *nilGuardedPointer[StateAwareDAG]
	mutex          sync.Mutex
	registeredData *RegisteredDataStore
	extpb.UnimplementedExtensionServiceServer
}

func (s *extensionService) Ping(_ context.Context, _ *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newExtensionService(dag *nilGuardedPointer[StateAwareDAG]) extpb.ExtensionServiceServer {
	service := &extensionService{
		dag:            dag,
		registeredData: NewRegisteredDataStore(),
	}

	mutator := NewRegisteredDataMutator(service.registeredData)
	GlobalMutatorRegistry.RegisterAuthConfigMutator(mutator)

	return service
}

func (s *extensionService) Subscribe(request *extpb.SubscribeRequest, stream grpc.ServerStreamingServer[extpb.SubscribeResponse]) error {
	if request.PolicyKind == "" {
		return fmt.Errorf("policy_kind is required for subscription")
	}

	channel := BlockingDAG.newUpdateChannel()
	for {
		dag := <-channel
		opts := []cel.EnvOption{
			kuadrant.CelExt(&dag),
		}

		s.mutex.Lock()
		if env, err := cel.NewEnv(opts...); err == nil {
			subscriptions := s.registeredData.GetAllSubscriptions()
			for key, sub := range subscriptions {
				if prg, err := env.Program(sub.CAst); err == nil {
					if newVal, _, err := prg.Eval(sub.Input); err == nil {
						if newVal != sub.Val {
							s.registeredData.UpdateSubscriptionValue(key, newVal)
							if err := stream.Send(&extpb.SubscribeResponse{Event: &extpb.Event{
								Metadata: sub.Input["self"].(*extpb.Policy).Metadata,
							}}); err != nil {
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
	dag, success := s.dag.getWaitWithTimeout(1 * time.Minute)
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
		policyKey := fmt.Sprintf("%s/%s/%s", request.Policy.Metadata.Kind, request.Policy.Metadata.Namespace, request.Policy.Metadata.Name)
		subscriptionKey := fmt.Sprintf("%s#%s", policyKey, request.Expression)
		s.registeredData.SetSubscription(subscriptionKey, Subscription{
			CAst:  cAst,
			Input: input,
			Val:   val,
		})
	}

	if err != nil {
		return nil, err
	}

	value, err := cel.RefValueToValue(val)
	if err != nil {
		return nil, err
	}
	return &extpb.ResolveResponse{
		CelResult: value,
	}, nil
}

func (s *extensionService) RegisterMutator(_ context.Context, request *extpb.RegisterMutatorRequest) (*emptypb.Empty, error) {
	// we should probably parse / check the cel expression here
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}
	if request.Requester == nil || request.Target == nil {
		return nil, errors.New("policy cannot be nil")
	}
	if request.Requester.Metadata == nil || request.Target.Metadata == nil {
		return nil, errors.New("policy metadata cannot be nil")
	}
	if request.Requester.Metadata.Kind == "" || request.Requester.Metadata.Namespace == "" || request.Requester.Metadata.Name == "" ||
		request.Target.Metadata.Kind == "" || request.Target.Metadata.Namespace == "" || request.Target.Metadata.Name == "" {
		return nil, errors.New("policy kind, namespace, and name must be specified")
	}
	targetKey := fmt.Sprintf("%s/%s/%s", request.Target.Metadata.Kind, request.Target.Metadata.Namespace, request.Target.Metadata.Name)
	requesterKey := fmt.Sprintf("%s/%s/%s", request.Requester.Metadata.Kind, request.Requester.Metadata.Namespace, request.Requester.Metadata.Name)

	entry := RegisteredDataEntry{
		Requester:  requesterKey,
		Binding:    request.Binding,
		Expression: request.Expression,
		CAst:       nil, //todo
	}

	s.registeredData.Set(targetKey, requesterKey, request.Binding, entry)

	return &emptypb.Empty{}, nil
}

func (s *extensionService) ClearPolicy(_ context.Context, request *extpb.ClearPolicyRequest) (*extpb.ClearPolicyResponse, error) {
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}
	if request.Policy == nil {
		return nil, errors.New("policy cannot be nil")
	}
	if request.Policy.Metadata == nil {
		return nil, errors.New("policy metadata cannot be nil")
	}
	if request.Policy.Metadata.Kind == "" || request.Policy.Metadata.Namespace == "" || request.Policy.Metadata.Name == "" {
		return nil, errors.New("policy kind, namespace, and name must be specified")
	}

	policyKey := fmt.Sprintf("%s/%s/%s", request.Policy.Metadata.Kind, request.Policy.Metadata.Namespace, request.Policy.Metadata.Name)

	clearedMutators, clearedSubscriptions := s.registeredData.ClearPolicyData(policyKey)

	return &extpb.ClearPolicyResponse{
		ClearedMutators:      int32(clearedMutators),      // #nosec G115
		ClearedSubscriptions: int32(clearedSubscriptions), // #nosec G115
	}, nil
}
