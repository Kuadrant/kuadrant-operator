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
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
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
	client     dynamic.Interface
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
		extensions,
		service,
		BlockingDAG,
		logger,
		sync,
		client,
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
type ReflectionFetcher func(ctx context.Context, url, serviceName string) (*descriptorpb.FileDescriptorSet, error)

type extensionService struct {
	dag               *nilGuardedPointer[StateAwareDAG]
	registeredData    *RegisteredDataStore
	protoCache        *ProtoCache
	reflectionFetcher ReflectionFetcher
	changeNotifier    ChangeNotifier
	logger            logr.Logger
	extpb.UnimplementedExtensionServiceServer
}

func (s *extensionService) Ping(_ context.Context, _ *extpb.PingRequest) (*extpb.PongResponse, error) {
	return &extpb.PongResponse{
		In: timestamppb.New(time.Now()),
	}, nil
}

func newExtensionService(dag *nilGuardedPointer[StateAwareDAG], logger logr.Logger) extpb.ExtensionServiceServer {
	reflectionClient := NewReflectionClient()
	service := &extensionService{
		dag:               dag,
		registeredData:    NewRegisteredDataStore(),
		protoCache:        NewProtoCache(),
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

	upstreamsToCheck := s.registeredData.GetUpstreamsForPolicy(policyID)
	clearedMutators, clearedSubscriptions, clearedUpstreams := s.registeredData.ClearPolicyData(policyID)

	// Clean up proto cache for upstreams that are no longer referenced
	for _, upstream := range upstreamsToCheck {
		cacheKey := ProtoCacheKey{
			ClusterName: upstream.ClusterName,
			Service:     upstream.Service,
		}
		if !s.registeredData.HasUpstreamForCacheKey(cacheKey) {
			s.protoCache.Delete(cacheKey)
			s.logger.V(1).Info("removed cached descriptors",
				"clusterName", cacheKey.ClusterName,
				"service", cacheKey.Service)
		}
	}

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

func (s *extensionService) RegisterUpstreamMethod(ctx context.Context, request *extpb.RegisterUpstreamMethodRequest) (*emptypb.Empty, error) {
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

	// Fetch service descriptors via reflection
	fds, err := s.reflectionFetcher(ctx, parsed.Host, request.Service)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "failed to fetch service descriptors for %s: %v", request.Service, err)
	}

	// Store descriptors in cache
	cacheKey := ProtoCacheKey{
		ClusterName: clusterName,
		Service:     request.Service,
	}
	s.protoCache.Set(cacheKey, fds)

	policyID := ResourceID{
		Kind:      request.Policy.Metadata.Kind,
		Namespace: request.Policy.Metadata.Namespace,
		Name:      request.Policy.Metadata.Name,
	}

	// Use the first target ref from the policy
	pbTargetRef := request.Policy.TargetRefs[0]
	targetRef := TargetRef{
		Group:     pbTargetRef.Group,
		Kind:      pbTargetRef.Kind,
		Name:      pbTargetRef.Name,
		Namespace: pbTargetRef.Namespace,
	}

	key := RegisteredUpstreamKey{
		Policy: policyID,
		URL:    request.Url,
	}
	entry := RegisteredUpstreamEntry{
		ClusterName: clusterName,
		Host:        host,
		Port:        port,
		TargetRef:   targetRef,
		Service:     request.Service,
		Method:      request.Method,
		FailureMode: string(wasm.FailureModeDeny),
		Timeout:     "100ms",
	}

	s.registeredData.SetUpstream(key, entry)

	s.logger.Info("registered upstream",
		"policy", fmt.Sprintf("%s/%s", policyID.Namespace, policyID.Name),
		"url", request.Url,
		"service", request.Service,
		"method", request.Method,
		"clusterName", clusterName)

	// Trigger reconciliation
	if s.changeNotifier != nil {
		reason := fmt.Sprintf("upstream registered for policy %s/%s: %s", policyID.Namespace, policyID.Name, request.Url)
		if err := s.changeNotifier(reason); err != nil {
			s.logger.Error(err, "failed to trigger change notification", "reason", reason)
		}
	}

	return &emptypb.Empty{}, nil
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
