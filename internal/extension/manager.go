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
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
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

type ResourceMutator[TResource any, TPolicy machinery.Policy] interface {
	Mutate(resource TResource, policy TPolicy) error
}

type AuthConfigMutator = ResourceMutator[*authorinov1beta3.AuthConfig, *kuadrantv1.AuthPolicy]

type MutatorRegistry struct {
	authConfigMutators []AuthConfigMutator
	mutex              sync.RWMutex
}

var GlobalMutatorRegistry = &MutatorRegistry{}

func (r *MutatorRegistry) RegisterAuthConfigMutator(mutator AuthConfigMutator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.authConfigMutators = append(r.authConfigMutators, mutator)
}

func (r *MutatorRegistry) ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for _, mutator := range r.authConfigMutators {
		if err := mutator.Mutate(authConfig, policy); err != nil {
			return err
		}
	}
	return nil
}

func ApplyAuthConfigMutators(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	return GlobalMutatorRegistry.ApplyAuthConfigMutators(authConfig, policy)
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

type RegisteredDataStore struct {
	data  map[string]map[string]RegisteredDataEntry
	mutex sync.RWMutex
}

func NewRegisteredDataStore() *RegisteredDataStore {
	return &RegisteredDataStore{
		data: make(map[string]map[string]RegisteredDataEntry),
	}
}

func (r *RegisteredDataStore) Set(target, requester, binding string, entry RegisteredDataEntry) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	entryKey := fmt.Sprintf("%s#%s", requester, binding)

	if _, exists := r.data[target]; !exists {
		r.data[target] = make(map[string]RegisteredDataEntry)
	}

	r.data[target][entryKey] = entry
}

func (r *RegisteredDataStore) GetAllForTarget(target string) []RegisteredDataEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	entries, exists := r.data[target]
	if !exists || len(entries) == 0 {
		return nil
	}

	result := make([]RegisteredDataEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result
}

func (r *RegisteredDataStore) Delete(target, requester, binding string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	entryKey := fmt.Sprintf("%s#%s", requester, binding)

	if targetMap, exists := r.data[target]; exists {
		if _, entryExists := targetMap[entryKey]; entryExists {
			delete(targetMap, entryKey)
			if len(targetMap) == 0 {
				delete(r.data, target)
			}
			return true
		}
	}
	return false
}

type extensionService struct {
	dag            *nilGuardedPointer[StateAwareDAG]
	mutex          sync.Mutex
	subscriptions  map[string]subscription
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
		subscriptions:  make(map[string]subscription),
		registeredData: NewRegisteredDataStore(),
	}

	mutator := &RegisteredDataMutator{
		registeredData: service.registeredData,
	}
	GlobalMutatorRegistry.RegisterAuthConfigMutator(mutator)

	return service
}

func (s *extensionService) Subscribe(_ *emptypb.Empty, stream grpc.ServerStreamingServer[extpb.SubscribeResponse]) error {
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
							if err := stream.Send(&extpb.SubscribeResponse{Event: &extpb.Event{
								Metadata: sub.input["self"].(*extpb.Policy).Metadata,
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

	value, err := cel.RefValueToValue(val)
	if err != nil {
		return nil, err
	}
	return &extpb.ResolveResponse{
		CelResult: value,
	}, nil
}

type subscription struct {
	cAst  *cel.Ast
	input map[string]any
	val   ref.Val
}

type RegisteredDataMutator struct {
	registeredData *RegisteredDataStore
}

func (m *RegisteredDataMutator) Mutate(authConfig *authorinov1beta3.AuthConfig, policy *kuadrantv1.AuthPolicy) error {
	policyKey := fmt.Sprintf("%s/%s/%s", policy.GetObjectKind().GroupVersionKind().Kind, policy.GetNamespace(), policy.GetName())

	registeredEntries := m.registeredData.GetAllForTarget(policyKey)
	if len(registeredEntries) == 0 {
		return nil
	}

	if authConfig.Spec.Response == nil {
		authConfig.Spec.Response = &authorinov1beta3.ResponseSpec{
			Success: authorinov1beta3.WrappedSuccessResponseSpec{
				DynamicMetadata: make(map[string]authorinov1beta3.SuccessResponseSpec),
			},
		}
	} else if authConfig.Spec.Response.Success.DynamicMetadata == nil {
		authConfig.Spec.Response.Success.DynamicMetadata = make(map[string]authorinov1beta3.SuccessResponseSpec)
	}

	// Collect all properties from all entries
	properties := make(map[string]authorinov1beta3.ValueOrSelector)
	for _, entry := range registeredEntries {
		properties[entry.Binding] = authorinov1beta3.ValueOrSelector{
			Expression: authorinov1beta3.CelExpression(entry.Expression),
		}
	}

	// Set all properties at once
	authConfig.Spec.Response.Success.DynamicMetadata["kuadrant"] = authorinov1beta3.SuccessResponseSpec{
		AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
			Json: &authorinov1beta3.JsonAuthResponseSpec{
				Properties: properties,
			},
		},
	}

	return nil
}

type RegisteredDataEntry struct {
	Requester  string
	Binding    string
	Expression string
	CAst       *cel.Ast
}

func (s *extensionService) RegisterMutator(_ context.Context, request *extpb.RegisterMutatorRequest) (*emptypb.Empty, error) {
	// we should probably parse / check the cel expression here
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
