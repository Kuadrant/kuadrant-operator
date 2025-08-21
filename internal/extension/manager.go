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
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/cel/ext"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

var ErrNoExtensionsFound = errors.New("no extensions found")

type ChangeNotifier func(reason string) error

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

func NewManager(location string, logger logr.Logger, sync io.Writer) (Manager, error) {
	names := discoverExtensions(logger, location)
	if len(names) == 0 {
		return Manager{}, ErrNoExtensionsFound
	}

	var extensions []Extension
	var err error

	service := newExtensionService(BlockingDAG, logger)
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

func (m *Manager) SetChangeNotifier(notifier ChangeNotifier) {
	if service, ok := m.service.(*extensionService); ok {
		service.changeNotifier = notifier
	}
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

// discoverExtensions scans the given directory for valid extensions.
// An extension is considered valid if it has its own subdirectory under the extensions directory
// with an executable file of the same name.
func discoverExtensions(logger logr.Logger, extensionsDir string) []string {
	extensionNames := []string{}

	if _, err := os.Stat(extensionsDir); os.IsNotExist(err) {
		logger.Info("Extensions directory does not exist", "directory", extensionsDir)
		return extensionNames
	}

	entries, err := os.ReadDir(extensionsDir)
	if err != nil {
		logger.Error(err, "unable to read extensions directory", "directory", extensionsDir)
		return extensionNames
	}

	for _, entry := range entries {
		if entry.IsDir() {
			extensionName := entry.Name()
			executablePath := filepath.Join(extensionsDir, extensionName, extensionName)
			if stat, err := os.Stat(executablePath); err == nil {
				if !stat.IsDir() && stat.Mode()&0111 != 0 {
					extensionNames = append(extensionNames, extensionName)
					logger.Info("Discovered extension", "name", extensionName, "path", executablePath)
				} else {
					logger.Info("Extension found but not executable", "name", extensionName, "path", executablePath)
				}
			} else {
				logger.Info("Extension directory found but no executable", "name", extensionName, "expectedPath", executablePath)
			}
		}
	}

	return extensionNames
}

func (m *Manager) HasSynced() bool {
	return true
}

type extensionService struct {
	dag            *nilGuardedPointer[StateAwareDAG]
	registeredData *RegisteredDataStore
	changeNotifier ChangeNotifier
	logger         logr.Logger
	extpb.UnimplementedExtensionServiceServer
}

func (s *extensionService) Ping(_ context.Context, _ *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newExtensionService(dag *nilGuardedPointer[StateAwareDAG], logger logr.Logger) extpb.ExtensionServiceServer {
	service := &extensionService{
		dag:            dag,
		registeredData: NewRegisteredDataStore(),
		logger:         logger.WithName("extensionService"),
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

		if env, err := cel.NewEnv(opts...); err == nil {
			subscriptions := s.registeredData.GetSubscriptionsForPolicyKind(request.PolicyKind)
			for key, sub := range subscriptions {
				if prg, err := env.Program(sub.CAst); err == nil {
					if newVal, _, err := prg.Eval(sub.Input); err == nil {
						if equalResult := celtypes.Equal(newVal, sub.Val); !celtypes.IsBool(equalResult) || equalResult != celtypes.True {
							s.registeredData.UpdateSubscriptionValue(key.Policy, key.Expression, newVal)
							if err := stream.Send(&extpb.SubscribeResponse{Event: &extpb.Event{
								Metadata: sub.Input["self"].(*extpb.Policy).Metadata,
							}}); err != nil {
								return err
							}
						}
					}
				}
			}
		}
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
		policyID := ResourceID{
			Kind:      request.Policy.Metadata.Kind,
			Namespace: request.Policy.Metadata.Namespace,
			Name:      request.Policy.Metadata.Name,
		}
		s.registeredData.SetSubscription(policyID, request.Expression, Subscription{
			CAst:       cAst,
			Input:      input,
			Val:        val,
			PolicyKind: request.Policy.Metadata.Kind,
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
	if request.Policy == nil {
		return nil, errors.New("policy cannot be nil")
	}
	if request.Policy.Metadata == nil {
		return nil, errors.New("policy metadata cannot be nil")
	}
	if request.Policy.Metadata.Kind == "" || request.Policy.Metadata.Namespace == "" || request.Policy.Metadata.Name == "" {
		return nil, errors.New("policy kind, namespace, and name must be specified")
	}
	if request.Domain == extpb.Domain_DOMAIN_UNSPECIFIED {
		return nil, errors.New("domain must be specified")
	}
	if len(request.Policy.TargetRefs) == 0 {
		return nil, errors.New("policy must have target references")
	}

	policyID := ResourceID{
		Kind:      request.Policy.Metadata.Kind,
		Namespace: request.Policy.Metadata.Namespace,
		Name:      request.Policy.Metadata.Name,
	}

	entry := DataProviderEntry{
		Policy:     policyID,
		Binding:    request.Binding,
		Expression: request.Expression,
		CAst:       nil, //todo
	}

	for _, pbTargetRef := range request.Policy.TargetRefs {
		targetRefLocator := createLocatorFromProtobuf(pbTargetRef)
		s.registeredData.Set(policyID, targetRefLocator, request.Domain, request.Binding, entry)
	}

	// Trigger notifier when mutators are registered
	if s.changeNotifier != nil {
		reason := fmt.Sprintf("mutator registered for policy %s/%s", request.Policy.Metadata.Namespace, request.Policy.Metadata.Name)
		if err := s.changeNotifier(reason); err != nil {
			// Do not fail if triggering fails - mutator is registered
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

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

	policyID := ResourceID{
		Kind:      request.Policy.Metadata.Kind,
		Namespace: request.Policy.Metadata.Namespace,
		Name:      request.Policy.Metadata.Name,
	}

	clearedMutators, clearedSubscriptions := s.registeredData.ClearPolicyData(policyID)

	// Trigger notifier when mutators are cleared
	if clearedMutators > 0 && s.changeNotifier != nil {
		reason := fmt.Sprintf("mutators cleared for policy %s/%s", request.Policy.Metadata.Namespace, request.Policy.Metadata.Name)
		if err := s.changeNotifier(reason); err != nil {
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

	return &extpb.ClearPolicyResponse{
		ClearedMutators:      int32(clearedMutators),      // #nosec G115
		ClearedSubscriptions: int32(clearedSubscriptions), // #nosec G115
	}, nil
}

// Creates a locator matching the definition in policy-machinery
func createLocatorFromProtobuf(pbTargetRef *extpb.TargetRef) string {
	groupKind := pbTargetRef.Kind
	if pbTargetRef.Group != "" {
		groupKind = pbTargetRef.Kind + "." + pbTargetRef.Group
	}
	name := pbTargetRef.Name
	if pbTargetRef.Namespace != "" {
		name = pbTargetRef.Namespace + "/" + pbTargetRef.Name
	}
	return fmt.Sprintf("%s:%s", strings.ToLower(groupKind), name)
}
