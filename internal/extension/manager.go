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
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/cel/ext"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

var ErrNoExtensionsFound = errors.New("no extensions found")

type ChangeNotifier func(reason string) error

type Manager struct {
	extensions       []Extension
	service          extpb.ExtensionServiceServer
	dag              *nilGuardedPointer[StateAwareDAG]
	logger           logr.Logger
	sync             io.Writer
	client           dynamic.Interface
	descriptorServer *grpc.Server
}

type Extension interface {
	Start() error
	Stop() error
	Name() string
}

func NewManager(location string, logger logr.Logger, sync io.Writer, client dynamic.Interface) (Manager, error) {
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
		extensions: extensions,
		service:    service,
		dag:        BlockingDAG,
		logger:     logger,
		sync:       sync,
		client:     client,
	}, err
}

func (m *Manager) Start() error {
	var err error

	if e := m.startDescriptorServer(); e != nil {
		m.logger.Error(e, "failed to start descriptor server")
		err = fmt.Errorf("descriptor server: %w", e)
	}

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

	m.stopDescriptorServer()

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

func (m *Manager) startDescriptorServer() error {
	const defaultPort = 50051
	descriptorPort, portErr := env.GetInt("EXTENSIONS_DESCRIPTOR_SERVICE_PORT", defaultPort)
	if portErr != nil {
		m.logger.Error(portErr, "invalid EXTENSIONS_DESCRIPTOR_SERVICE_PORT, using default", "default", defaultPort)
		descriptorPort = defaultPort
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", descriptorPort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", descriptorPort, err)
	}

	server := grpc.NewServer()

	if svc, ok := m.service.(extpb.DescriptorServiceServer); ok {
		extpb.RegisterDescriptorServiceServer(server, svc)
	} else {
		lis.Close()
		return fmt.Errorf("service does not implement DescriptorServiceServer")
	}

	m.descriptorServer = server

	go func() {
		m.logger.Info("starting descriptor service", "port", descriptorPort)
		if err := server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			m.logger.Error(err, "descriptor server failed")
		}
	}()

	return nil
}

func (m *Manager) stopDescriptorServer() {
	if m.descriptorServer == nil {
		return
	}

	m.logger.Info("stopping descriptor service")

	done := make(chan struct{})
	go func() {
		m.descriptorServer.GracefulStop()
		close(done)
	}()

	timeout := 5 * time.Second
	select {
	case <-done:
		m.logger.Info("descriptor service stopped gracefully")
	case <-time.After(timeout):
		m.logger.Info("descriptor service graceful stop timed out, forcing stop")
		m.descriptorServer.Stop()
	}

	m.descriptorServer = nil
}

func (m *Manager) SetChangeNotifier(notifier ChangeNotifier) {
	if service, ok := m.service.(*extensionService); ok {
		service.changeNotifier = notifier
	}
}

func (m *Manager) TriggerReconciliation(reason string) error {
	logger := m.logger.WithName("TriggerReconciliation")
	logger.V(1).Info("triggering reconciliation", "reason", reason)

	kuadrantResource := m.client.Resource(kuadrantv1beta1.KuadrantsResource)
	kuadrantList, err := kuadrantResource.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Kuadrant resources: %w", err)
	}
	if len(kuadrantList.Items) == 0 {
		return fmt.Errorf("no Kuadrant resources found in cluster")
	}

	for _, kuadrant := range kuadrantList.Items {
		if kuadrant.GetDeletionTimestamp() != nil {
			continue
		}

		annotations := kuadrant.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[TriggerTimeAnnotation] = time.Now().Format(time.RFC3339Nano)
		annotations[TriggerReasonAnnotation] = reason
		kuadrant.SetAnnotations(annotations)

		_, err := kuadrantResource.Namespace(kuadrant.GetNamespace()).Update(
			context.TODO(),
			&kuadrant,
			metav1.UpdateOptions{},
		)
		if err != nil {
			logger.Error(err, "failed to update Kuadrant resource",
				"namespace", kuadrant.GetNamespace(),
				"name", kuadrant.GetName())
			continue
		}

		logger.V(1).Info("successfully triggered reconciliation",
			"namespace", kuadrant.GetNamespace(),
			"name", kuadrant.GetName(),
			"reason", reason)
		return nil
	}
	return fmt.Errorf("failed to update any Kuadrant resource")
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

// ReflectionFetcher fetches service descriptors via gRPC reflection.
type ReflectionFetcher func(ctx context.Context, url, serviceName, methodName string) (*descriptorpb.FileDescriptorSet, error)

type extensionService struct {
	dag               *nilGuardedPointer[StateAwareDAG]
	registeredData    *RegisteredDataStore
	reflectionFetcher ReflectionFetcher
	changeNotifier    ChangeNotifier
	logger            logr.Logger
	extpb.UnimplementedExtensionServiceServer
	extpb.UnimplementedDescriptorServiceServer
}

func (s *extensionService) Ping(_ context.Context, _ *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func (s *extensionService) GetServiceDescriptors(_ context.Context, request *extpb.GetServiceDescriptorsRequest) (*extpb.GetServiceDescriptorsResponse, error) {
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}

	descriptors := make([]*extpb.ServiceDescriptor, 0, len(request.Services))

	for _, serviceRef := range request.Services {
		if serviceRef == nil {
			return nil, fmt.Errorf("service reference must be provided for all services")
		}
		if serviceRef.ClusterName == "" {
			return nil, fmt.Errorf("cluster_name must be specified for all services")
		}
		if serviceRef.Service == "" {
			return nil, fmt.Errorf("service must be specified for all services")
		}

		cacheKey := ProtoCacheKey{
			ClusterName: serviceRef.ClusterName,
			Service:     serviceRef.Service,
		}

		fds, found := s.registeredData.GetProtoDescriptor(cacheKey)
		if !found {
			return nil, fmt.Errorf("descriptors not found for cluster=%q service=%q", serviceRef.ClusterName, serviceRef.Service)
		}

		fdsBytes, err := proto.Marshal(fds)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal descriptors for cluster=%q service=%q: %w", serviceRef.ClusterName, serviceRef.Service, err)
		}

		descriptors = append(descriptors, &extpb.ServiceDescriptor{
			ClusterName:       serviceRef.ClusterName,
			Service:           serviceRef.Service,
			FileDescriptorSet: fdsBytes,
		})
	}

	return &extpb.GetServiceDescriptorsResponse{
		Descriptors: descriptors,
	}, nil
}

func newExtensionService(dag *nilGuardedPointer[StateAwareDAG], logger logr.Logger) extpb.ExtensionServiceServer {
	reflectionClient := NewReflectionClient()
	service := &extensionService{
		dag:               dag,
		registeredData:    NewRegisteredDataStore(),
		reflectionFetcher: reflectionClient.FetchServiceDescriptors,
		logger:            logger.WithName("extensionService"),
	}

	authMutator := NewRegisteredDataMutator[*authorinov1beta3.AuthConfig](service.registeredData)
	GlobalMutatorRegistry.RegisterAuthConfigMutator(authMutator)

	wasmMutator := NewRegisteredDataMutator[*wasm.Config](service.registeredData)
	GlobalMutatorRegistry.RegisterWasmConfigMutator(wasmMutator)

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

	clearedMutators, clearedSubscriptions, clearedUpstreams := s.registeredData.ClearPolicyData(policyID)

	// Trigger notifier when mutators or upstreams are cleared
	if (clearedMutators > 0 || clearedUpstreams > 0) && s.changeNotifier != nil {
		reason := fmt.Sprintf("data cleared for policy %s/%s (mutators: %d, upstreams: %d)", request.Policy.Metadata.Namespace, request.Policy.Metadata.Name, clearedMutators, clearedUpstreams)
		if err := s.changeNotifier(reason); err != nil {
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

	return &extpb.ClearPolicyResponse{
		ClearedMutators:      int32(clearedMutators),      // #nosec G115
		ClearedSubscriptions: int32(clearedSubscriptions), // #nosec G115
	}, nil
}

var invalidClusterNameChars = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// generateClusterName builds an Envoy cluster name from a host and optional port.
// Invalid characters are replaced with hyphens and the name is prefixed with "ext-".
func generateClusterName(host string, port int) string {
	clusterName := "ext-" + invalidClusterNameChars.ReplaceAllString(host, "-")
	if port != 0 {
		clusterName += "-" + strconv.Itoa(port)
	}
	return clusterName
}

func (s *extensionService) RegisterActionMethod(ctx context.Context, request *extpb.RegisterActionMethodRequest) (*emptypb.Empty, error) {
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
	if strings.TrimSpace(request.Name) == "" {
		return nil, errors.New("name must be specified")
	}
	if request.Url == "" {
		return nil, errors.New("url must be specified")
	}
	if request.Service == "" {
		return nil, errors.New("service must be specified")
	}
	if request.Method == "" {
		return nil, errors.New("method must be specified")
	}
	if len(request.Policy.TargetRefs) == 0 {
		return nil, errors.New("policy must have target references")
	}

	// Parse URL — expect grpc:// scheme
	parsed, err := url.Parse(request.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", request.Url, err)
	}
	if parsed.Scheme != "grpc" {
		return nil, fmt.Errorf("url scheme must be \"grpc\", got %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("url must contain a host: %q", request.Url)
	}
	var port int
	if portStr := parsed.Port(); portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port in url %q: %w", request.Url, err)
		}
	}

	clusterName := generateClusterName(host, port)

	policyID := ResourceID{
		Kind:      request.Policy.Metadata.Kind,
		Namespace: request.Policy.Metadata.Namespace,
		Name:      request.Policy.Metadata.Name,
	}

	key := RegisteredUpstreamKey{
		Policy:  policyID,
		Name:    request.Name,
		URL:     request.Url,
		Service: request.Service,
		Method:  request.Method,
	}

	// Fast-path rejection: avoid the expensive reflection call when the name is already taken
	if s.registeredData.IsUpstreamNameTaken(key) {
		return nil, grpcstatus.Errorf(codes.AlreadyExists, "action method name %q is already registered for policy %s/%s", request.Name, policyID.Namespace, policyID.Name)
	}

	// Fetch service descriptors via reflection and validate method exists
	fds, err := s.reflectionFetcher(ctx, parsed.Host, request.Service, request.Method)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "failed to fetch and validate service %s method %s: %v", request.Service, request.Method, err)
	}

	// Use the first target ref from the policy
	pbTargetRef := request.Policy.TargetRefs[0]
	if pbTargetRef == nil {
		return nil, errors.New("first target reference in policy is nil")
	}
	targetRef := TargetRef{
		Group:     pbTargetRef.Group,
		Kind:      pbTargetRef.Kind,
		Name:      pbTargetRef.Name,
		Namespace: pbTargetRef.Namespace,
	}

	entry := RegisteredUpstreamEntry{
		ClusterName:     clusterName,
		Host:            host,
		Port:            port,
		TargetRef:       targetRef,
		Service:         request.Service,
		Method:          request.Method,
		FailureMode:     string(wasm.FailureModeDeny),
		Timeout:         "100ms",
		MessageTemplate: request.MessageTemplate,
	}

	// Atomically check name uniqueness and store the upstream
	if !s.registeredData.SetUpstreamIfNameAvailable(key, entry, fds) {
		return nil, grpcstatus.Errorf(codes.AlreadyExists, "action method name %q is already registered for policy %s/%s", request.Name, policyID.Namespace, policyID.Name)
	}

	s.logger.Info("registered action method",
		"policy", fmt.Sprintf("%s/%s", policyID.Namespace, policyID.Name),
		"name", request.Name,
		"url", request.Url,
		"service", request.Service,
		"method", request.Method,
		"clusterName", clusterName)

	// Trigger reconciliation
	if s.changeNotifier != nil {
		reason := fmt.Sprintf("action method %q registered for policy %s/%s: %s", request.Name, policyID.Namespace, policyID.Name, request.Url)
		if err := s.changeNotifier(reason); err != nil {
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

	return &emptypb.Empty{}, nil
}

// validatePolicyRequest validates the common policy fields required by pipeline handlers.
func validatePolicyRequest(policy *extpb.Policy) (ResourceID, error) {
	if policy == nil {
		return ResourceID{}, errors.New("policy cannot be nil")
	}
	if policy.Metadata == nil {
		return ResourceID{}, errors.New("policy metadata cannot be nil")
	}
	if policy.Metadata.Kind == "" || policy.Metadata.Namespace == "" || policy.Metadata.Name == "" {
		return ResourceID{}, errors.New("policy kind, namespace, and name must be specified")
	}
	return ResourceID{
		Kind:      policy.Metadata.Kind,
		Namespace: policy.Metadata.Namespace,
		Name:      policy.Metadata.Name,
	}, nil
}

// celSyntaxEnv is a shared minimal CEL environment used only for syntactic
// validation. Runtime variables (request.*, auth.*, etc.) are not declared
// here — they are only available at wasm-shim runtime.
var celSyntaxEnv = func() *cel.Env {
	env, err := cel.NewEnv()
	if err != nil {
		panic(fmt.Sprintf("failed to create CEL validation environment: %v", err))
	}
	return env
}()

func validateCELExpression(expr string) error {
	_, issues := celSyntaxEnv.Parse(expr)
	if issues.Err() != nil {
		return fmt.Errorf("invalid CEL expression %q: %w", expr, issues.Err())
	}
	return nil
}

var varNameRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (s *extensionService) PipelineCommit(_ context.Context, request *extpb.PipelineCommitRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}

	policyID, err := validatePolicyRequest(request.Policy)
	if err != nil {
		return nil, err
	}

	entries, err := s.validateActions(policyID, request.Actions)
	if err != nil {
		return nil, err
	}

	if err := s.registeredData.ReplacePipelineActions(policyID, entries); err != nil {
		return nil, err
	}

	if len(request.Policy.TargetRefs) == 0 {
		return nil, fmt.Errorf("pipeline commit for policy %s/%s: policy must have target references", policyID.Namespace, policyID.Name)
	}
	targetRefs := make([]TargetRef, 0, len(request.Policy.TargetRefs))
	for _, ref := range request.Policy.TargetRefs {
		if ref == nil {
			return nil, fmt.Errorf("pipeline commit for policy %s/%s: target reference cannot be nil", policyID.Namespace, policyID.Name)
		}
		targetRefs = append(targetRefs, TargetRef{
			Group:     ref.Group,
			Kind:      ref.Kind,
			Name:      ref.Name,
			Namespace: ref.Namespace,
		})
	}
	s.registeredData.SetPipelineTargetRefs(policyID, targetRefs)

	s.logger.Info("pipeline committed",
		"policy", fmt.Sprintf("%s/%s", policyID.Namespace, policyID.Name),
		"actions", len(entries))

	if s.changeNotifier != nil {
		reason := fmt.Sprintf("pipeline committed for policy %s/%s (%d actions)",
			policyID.Namespace, policyID.Name, len(entries))
		if err := s.changeNotifier(reason); err != nil {
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

	return &emptypb.Empty{}, nil
}

type actionValidationCtx struct {
	store       *RegisteredDataStore
	policyID    ResourceID
	varToMethod map[string]string
}

type actionEntryValidator func(action *extpb.ActionEntry, index int, entry *PipelineActionEntry, vctx *actionValidationCtx) error

var actionEntryValidators = map[extpb.ActionType]actionEntryValidator{
	extpb.ActionType_ACTION_TYPE_GRPC_METHOD: validateGRPCMethodEntry,
	extpb.ActionType_ACTION_TYPE_DENY:        validateDenyEntry,
	extpb.ActionType_ACTION_TYPE_FAIL:        validateFailEntry,
	extpb.ActionType_ACTION_TYPE_ADD_HEADERS: validateAddHeadersEntry,
}

func validateGRPCMethodEntry(action *extpb.ActionEntry, index int, entry *PipelineActionEntry, vctx *actionValidationCtx) error {
	if action.Method == "" {
		return fmt.Errorf("actions[%d]: method must be specified for grpc_method actions", index)
	}
	if !vctx.store.HasUpstreamName(vctx.policyID, action.Method) {
		return fmt.Errorf("actions[%d]: method %q is not a registered action method for this policy", index, action.Method)
	}
	if action.Var != "" && !varNameRegexp.MatchString(action.Var) {
		return fmt.Errorf("actions[%d]: var %q must match [a-zA-Z_][a-zA-Z0-9_]*", index, action.Var)
	}
	entry.Method = action.Method
	entry.Var = action.Var
	if action.Var != "" {
		vctx.varToMethod[action.Var] = action.Method
	}
	return nil
}

func validateDenyEntry(action *extpb.ActionEntry, index int, entry *PipelineActionEntry, _ *actionValidationCtx) error {
	if action.WithStatus != 0 {
		if err := validateHTTPStatusCode(fmt.Sprintf("%d", action.WithStatus), fmt.Sprintf("actions[%d].with_status", index)); err != nil {
			return err
		}
	}
	entry.WithStatus = int(action.WithStatus)
	entry.WithHeaders = action.WithHeaders
	entry.WithBody = action.WithBody
	return nil
}

func validateFailEntry(action *extpb.ActionEntry, index int, entry *PipelineActionEntry, _ *actionValidationCtx) error {
	if strings.TrimSpace(action.LogMessage) == "" {
		return fmt.Errorf("actions[%d]: log_message must be specified for fail actions", index)
	}
	entry.LogMessage = action.LogMessage
	return nil
}

func validateAddHeadersEntry(action *extpb.ActionEntry, index int, entry *PipelineActionEntry, _ *actionValidationCtx) error {
	if action.HeadersToAdd == "" {
		return fmt.Errorf("actions[%d]: headers_to_add must be specified for add_headers actions", index)
	}
	if err := validateCELExpression(action.HeadersToAdd); err != nil {
		return fmt.Errorf("actions[%d].headers_to_add: %w", index, err)
	}
	entry.HeadersToAdd = action.HeadersToAdd
	return nil
}

func (s *extensionService) validateActions(policyID ResourceID, actions []*extpb.ActionEntry) ([]PipelineActionEntry, error) {
	entries := make([]PipelineActionEntry, 0, len(actions))
	vctx := actionValidationCtx{
		store:       s.registeredData,
		policyID:    policyID,
		varToMethod: make(map[string]string),
	}

	for i, action := range actions {
		if action == nil {
			return nil, fmt.Errorf("actions[%d]: entry cannot be nil", i)
		}
		if action.ActionType == extpb.ActionType_ACTION_TYPE_UNSPECIFIED {
			return nil, fmt.Errorf("actions[%d]: action_type must be specified", i)
		}
		if action.Phase != string(PipelinePhaseRequest) && action.Phase != string(PipelinePhaseResponse) {
			return nil, fmt.Errorf("actions[%d]: phase must be %q or %q, got %q", i, PipelinePhaseRequest, PipelinePhaseResponse, action.Phase)
		}
		if action.Predicate != "" {
			if err := validateCELExpression(action.Predicate); err != nil {
				return nil, fmt.Errorf("actions[%d].predicate: %w", i, err)
			}
		}

		entry := PipelineActionEntry{
			ActionType: action.ActionType,
			Predicate:  action.Predicate,
			Phase:      action.Phase,
		}

		validator, ok := actionEntryValidators[action.ActionType]
		if !ok {
			return nil, fmt.Errorf("actions[%d]: unknown action_type %s", i, action.ActionType)
		}
		if err := validator(action, i, &entry, &vctx); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	if len(vctx.varToMethod) > 0 {
		if err := s.validateCrossActionVars(policyID, actions, vctx.varToMethod); err != nil {
			return nil, err
		}
	}

	return entries, nil
}

func validateHTTPStatusCode(code, field string) error {
	if code == "" {
		return fmt.Errorf("%s: must be specified", field)
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return fmt.Errorf("%s: %q is not a valid integer", field, code)
	}
	if n < 100 || n > 599 {
		return fmt.Errorf("%s: must be between 100 and 599, got %d", field, n)
	}
	return nil
}

func (s *extensionService) validateCrossActionVars(policyID ResourceID, actions []*extpb.ActionEntry, varToMethod map[string]string) error {
	for i, action := range actions {
		exprs := collectCELExpressions(action)
		for _, expr := range exprs {
			fieldAccesses, err := extractVarFieldAccesses(expr, varToMethod)
			if err != nil {
				return fmt.Errorf("actions[%d]: %w", i, err)
			}
			for varName, chains := range fieldAccesses {
				methodName := varToMethod[varName]
				_, upstreamEntry, found := s.registeredData.GetUpstreamByName(policyID, methodName)
				if !found {
					continue
				}
				cacheKey := ProtoCacheKey{ClusterName: upstreamEntry.ClusterName, Service: upstreamEntry.Service}
				fds, ok := s.registeredData.GetProtoDescriptor(cacheKey)
				if !ok {
					s.logger.V(1).Info("proto descriptor not cached, skipping field validation", "var", varName, "method", methodName)
					continue
				}
				responseType := findResponseMessageType(fds, upstreamEntry.Service, upstreamEntry.Method)
				if responseType == "" {
					s.logger.V(1).Info("response message type not found, skipping field validation", "var", varName, "method", methodName, "service", upstreamEntry.Service)
					continue
				}
				for _, chain := range chains {
					if err := validateFieldAccess(fds, responseType, chain); err != nil {
						return fmt.Errorf("actions[%d]: variable %q: %w", i, varName, err)
					}
				}
			}
		}
	}
	return nil
}

func collectCELExpressions(action *extpb.ActionEntry) []string {
	var exprs []string
	if action.Predicate != "" {
		exprs = append(exprs, action.Predicate)
	}
	if action.HeadersToAdd != "" {
		exprs = append(exprs, action.HeadersToAdd)
	}
	return exprs
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
