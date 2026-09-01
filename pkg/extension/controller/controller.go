package controller

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimectrl "sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlruntimeevent "sigs.k8s.io/controller-runtime/pkg/event"
	ctrlruntimehandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	ctrlruntimesrc "sigs.k8s.io/controller-runtime/pkg/source"

	basereconciler "github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	extutils "github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

const (
	// ExtensionFinalizer is added to extension managed policies so that final
	// cleanup (e.g. mutator/subscription deregistration) can occur prior to
	// object deletion.
	ExtensionFinalizer = "kuadrant.io/extensions"

	handshakeTimeout         = 10 * time.Second
	defaultHeartbeatInterval = 15 * time.Second
	releaseSessionTimeout    = 5 * time.Second
	initialReconnectBackoff  = 1 * time.Second
	maxReconnectBackoff      = 30 * time.Second
)

func defaultReconnectBackoff() wait.Backoff {
	return wait.Backoff{
		Duration: initialReconnectBackoff,
		Factor:   2.0,
		Jitter:   0.2,
		Cap:      maxReconnectBackoff,
		Steps:    math.MaxInt32,
	}
}

// ExtensionConfig captures the immutable configuration for a controller
// instance constructed by the Builder. It determines:
//
//	Name:        controller name (also used for logging prefix)
//	PolicyKind:  the Kind of the primary policy CRD managed
//	ForType:     the primary object type reconciled
//	Reconcile:   the user provided reconcile function
//	WatchSources: dynamic sources watched (primary, additional and owned)
type ExtensionConfig struct {
	Name         string
	PolicyKind   string
	ForType      client.Object
	Reconcile    exttypes.ReconcileFn
	WatchSources []ctrlruntimesrc.Source
}

// ExtensionController is a thin wrapper around controller-runtime's manager
// and controller that wires gRPC event subscriptions with the reconcile loop
// and exposes helper methods (Resolve, ReconcileObject, etc.) via the
// KuadrantCtx interface passed to user code.
type ExtensionController struct {
	config ExtensionConfig

	logger            logr.Logger
	manager           ctrlruntime.Manager
	extensionClient   *extensionClient
	tokenSource       tokenSource
	heartbeatInterval time.Duration
	reconnectBackoff  wait.Backoff
	eventCache        *EventTypeCache

	*basereconciler.BaseReconciler // TODO(didierofrivia): Next iteration, use policy machinery
}

// Start runs the controller manager and a background session supervisor. The
// manager (and its health probes) must come up regardless of session state, so
// a successful handshake is deliberately not a precondition for starting it.
func (ec *ExtensionController) Start(ctx context.Context) error {
	// todo(adam-cattermole): how big do we make the reconcile event channel?
	//	 how many should we queue before we block?
	reconcileChan := make(chan ctrlruntimeevent.GenericEvent, 50)

	channelSource := ctrlruntimesrc.Channel(reconcileChan, &ctrlruntimehandler.EnqueueRequestForObject{})
	watchSources := append(ec.config.WatchSources, channelSource)

	// test path: supervise runs blocking in the foreground
	if ec.manager == nil {
		ec.superviseSession(ctx, reconcileChan)
		return nil
	}

	ctrl, err := ctrlruntimectrl.New(ec.config.Name, ec.manager, ctrlruntimectrl.Options{Reconciler: ec})
	if err != nil {
		return fmt.Errorf("error creating controller: %w", err)
	}
	for _, source := range watchSources {
		if err := ctrl.Watch(source); err != nil {
			return fmt.Errorf("error watching resource: %w", err)
		}
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	supervisorDone := make(chan struct{})
	go func() {
		defer close(supervisorDone)
		ec.superviseSession(sessionCtx, reconcileChan)
	}()

	err = ec.manager.Start(ctx)
	cancel()
	<-supervisorDone
	if err != nil {
		return fmt.Errorf("error starting manager: %w", err)
	}
	return nil
}

func (ec *ExtensionController) superviseSession(ctx context.Context, reconcileChan chan ctrlruntimeevent.GenericEvent) {
	defer ec.shutdown()
	for {
		if err := ec.handshakeWithBackoff(ctx); err != nil {
			return
		}
		ec.logger.Info("handshake accepted", "extension", ec.config.Name, "policyKind", ec.config.PolicyKind)
		if ctx.Err() != nil {
			return
		}

		streamCtx, cancel := context.WithCancel(ctx)
		go ec.heartbeat(streamCtx, cancel)
		ec.streamSession(streamCtx, reconcileChan)
		cancel()

		if ctx.Err() != nil {
			return
		}
	}
}

func (ec *ExtensionController) handshakeWithBackoff(ctx context.Context) error {
	backoff := ec.newReconnectBackoff()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := ec.attemptHandshake(ctx)
		if err == nil {
			return nil
		}
		ec.logger.Error(err, "handshake attempt failed, retrying")
		if !waitBackoff(ctx, &backoff) {
			return ctx.Err()
		}
	}
}

func waitBackoff(ctx context.Context, backoff *wait.Backoff) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(backoff.Step()):
		return true
	}
}

func (ec *ExtensionController) attemptHandshake(ctx context.Context) error {
	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	token, err := ec.tokenSource(handshakeCtx)
	if err != nil {
		return fmt.Errorf("failed to obtain handshake credential: %w", err)
	}
	if err := ec.extensionClient.handshake(handshakeCtx, token, ec.config.PolicyKind); err != nil {
		return fmt.Errorf("extension handshake failed: %w", err)
	}
	return nil
}

// streamSession rides out transient Unavailable errors on the same session, and
// returns on Unauthenticated (session gone) or ctx cancellation.
func (ec *ExtensionController) streamSession(ctx context.Context, reconcileChan chan ctrlruntimeevent.GenericEvent) {
	backoff := ec.newReconnectBackoff()
	for {
		err := ec.subscribeEvents(ctx, reconcileChan)
		if ctx.Err() != nil {
			return
		}
		if isSessionLost(err) {
			ec.extensionClient.session.setToken("")
			return
		}
		if err != nil {
			ec.logger.Error(err, "subscribe stream ended, retrying on same session")
		}
		if !waitBackoff(ctx, &backoff) {
			return
		}
	}
}

func (ec *ExtensionController) newReconnectBackoff() wait.Backoff {
	if ec.reconnectBackoff.Duration <= 0 {
		return defaultReconnectBackoff()
	}
	return ec.reconnectBackoff
}

func isSessionLost(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.Unauthenticated
}

func resolveHeartbeatInterval(logger logr.Logger) time.Duration {
	value := os.Getenv("KUADRANT_EXTENSION_HEARTBEAT_INTERVAL")
	if value == "" {
		return defaultHeartbeatInterval
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		logger.Info("invalid KUADRANT_EXTENSION_HEARTBEAT_INTERVAL, using default", "value", value, "default", defaultHeartbeatInterval)
		return defaultHeartbeatInterval
	}
	return interval
}

// subscribeEvents opens a long‑lived gRPC stream for events related to the policy
// kind and enqueues reconcile requests for received events.
func (ec *ExtensionController) subscribeEvents(ctx context.Context, reconcileChan chan ctrlruntimeevent.GenericEvent) error {
	return ec.extensionClient.subscribe(ctx, ec.config.PolicyKind, func(response *extpb.SubscribeResponse) {
		ec.logger.Info("received response", "response", response)
		// todo(adam-cattermole): how might we inform of an error from subscribe responses?
		if response.Error != nil && response.Error.Code != 0 {
			ec.logger.Error(fmt.Errorf("got error from stream: code=%d msg=%s", response.Error.Code, response.Error.Message), "error", response.Error.Message)
			return
		}
		trigger := &unstructured.Unstructured{}
		if response.Event != nil && response.Event.Metadata != nil {
			trigger.SetName(response.Event.Metadata.Name)
			trigger.SetNamespace(response.Event.Metadata.Namespace)
			trigger.SetKind(response.Event.Metadata.Kind)
			select {
			case reconcileChan <- ctrlruntimeevent.GenericEvent{Object: trigger}:
			case <-ctx.Done():
			}
		}
	})
}

func (ec *ExtensionController) heartbeat(ctx context.Context, cancel context.CancelFunc) {
	interval := ec.heartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, interval)
			_, err := ec.extensionClient.ping(pingCtx)
			pingCancel()
			if err != nil {
				ec.logger.Error(err, "heartbeat ping failed")
				if isSessionLost(err) {
					cancel()
					return
				}
			}
		}
	}
}

// shutdown releases the session and closes the connection. The parent context
// is already cancelled at this point, so a fresh timeout context is used.
func (ec *ExtensionController) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), releaseSessionTimeout)
	defer cancel()
	if err := ec.extensionClient.releaseSession(ctx); err != nil {
		ec.logger.Error(err, "failed to release session on shutdown")
	}
	if err := ec.extensionClient.close(); err != nil {
		ec.logger.Error(err, "failed to close extension client")
	}
}

// Reconcile implements the controller-runtime reconcile loop. It ensures
// finalizers, dispatches the configured user Reconcile function and performs
// post‑reconcile cleanup.
func (ec *ExtensionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	eventType, exists := ec.eventCache.popEvent(request.Namespace, request.Name)
	if !exists {
		eventType = EventTypeUnknown
	}

	ctx = ec.setupContext(ctx)

	// Ensure finalizer exists for both create and updates
	if eventType == EventTypeCreate || eventType == EventTypeUpdate {
		if err := ec.ensureFinalizer(ctx, request); err != nil {
			if errors.IsNotFound(err) {
				return reconcile.Result{}, err
			}
			return reconcile.Result{RequeueAfter: time.Second}, err
		}
	}

	// Call user reconcile function
	ec.logger.Info("reconciling request", "namespace", request.Namespace, "name", request.Name, "event", eventType)
	result, err := ec.config.Reconcile(ctx, request, ec)
	if err != nil {
		return result, err
	}

	if eventType == EventTypeUpdate {
		if err := ec.cleanupFinalizer(ctx, request); err != nil {
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{RequeueAfter: time.Second}, err
		}
	}

	return result, nil
}

func (ec *ExtensionController) setupContext(ctx context.Context) context.Context {
	// todo(adam-cattermole): the ctx passed here is a different one created by ctrlruntime for each reconcile so we
	//  have to inject here instead of in Start(). Is there any benefit to us storing this in the context for it be
	//  retrieved by the user in their Reconcile method, or should it just pass them as parameters?
	ctx = context.WithValue(ctx, logr.Logger{}, ec.logger)
	ctx = context.WithValue(ctx, extutils.SchemeKey, ec.manager.GetScheme())
	ctx = context.WithValue(ctx, extutils.ClientKey, ec.manager.GetClient())
	return ctx
}

func (ec *ExtensionController) ensureFinalizer(ctx context.Context, request reconcile.Request) error {
	obj := ec.config.ForType.DeepCopyObject().(client.Object)
	if err := ec.Client().Get(ctx, types.NamespacedName{Namespace: request.Namespace, Name: request.Name}, obj); err != nil {
		return err
	}
	return ec.AddFinalizer(ctx, obj, ExtensionFinalizer)
}

func (ec *ExtensionController) cleanupFinalizer(ctx context.Context, request reconcile.Request) error {
	obj := ec.config.ForType.DeepCopyObject().(client.Object)
	if err := ec.Client().Get(ctx, types.NamespacedName{Namespace: request.Namespace, Name: request.Name}, obj); err != nil {
		return err
	}
	if obj.GetDeletionTimestamp() != nil {
		if err := ec.ClearPolicy(ctx, request.Namespace, request.Name, ec.config.PolicyKind); err != nil {
			return err
		}
		return ec.RemoveFinalizer(ctx, obj, ExtensionFinalizer)
	}
	return nil
}

func (ec *ExtensionController) resolveExpression(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (*extpb.ResolveResponse, error) {
	pbPolicy := convertPolicyToProtobuf(policy)

	resp, err := ec.extensionClient.client.Resolve(ctx, &extpb.ResolveRequest{
		Policy:     pbPolicy,
		Expression: expression,
		Subscribe:  subscribe,
	})
	if err != nil {
		return nil, fmt.Errorf("error resolving expression: %w", err)
	}

	if resp == nil || resp.GetCelResult() == nil {
		return nil, fmt.Errorf("empty response from extension service")
	}

	return resp, nil
}

// Resolve evaluates a CEL expression against the provided policy returning the
// raw CEL result as a ref.Val. If subscribe is true the extension service will
// stream future changes (triggering new reconciliations).
func (ec *ExtensionController) Resolve(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
	resp, err := ec.resolveExpression(ctx, policy, expression, subscribe)
	if err != nil {
		return ref.Val(nil), err
	}

	val, err := cel.ValueToRefValue(celtypes.DefaultTypeAdapter, resp.GetCelResult())
	if err != nil {
		return ref.Val(nil), fmt.Errorf("error converting cel result: %w", err)
	}

	return val, nil
}

// ResolvePolicy evaluates a CEL expression that must return a Policy protobuf
// which is then adapted to the generic Policy interface. Used when expressions
// transform or select policies.
func (ec *ExtensionController) ResolvePolicy(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error) {
	resp, err := ec.resolveExpression(ctx, policy, expression, subscribe)
	if err != nil {
		return nil, err
	}

	celResult := resp.GetCelResult()

	// Handle object values that should be protobuf policies
	if celResult.GetObjectValue() != nil {
		// Unmarshal the protobuf object directly
		pbPolicyResult := &extpb.Policy{}
		if err := celResult.GetObjectValue().UnmarshalTo(pbPolicyResult); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CEL object result to protobuf Policy: %w", err)
		}
		return extpb.NewPolicyAdapter(pbPolicyResult), nil
	}

	return nil, fmt.Errorf("CEL result is not an object value that can be converted to Policy")
}

// AddDataTo registers a mutator expression that will inject computed data into
// a policy under the provided domain and binding.
func (ec *ExtensionController) AddDataTo(ctx context.Context, policy exttypes.Policy, domain exttypes.Domain, binding string, expression string) error {
	pbPolicy := convertPolicyToProtobuf(policy)
	pbDomain := convertDomainToProtobuf(domain)

	_, err := ec.extensionClient.client.RegisterMutator(ctx, &extpb.RegisterMutatorRequest{
		Policy:     pbPolicy,
		Domain:     pbDomain,
		Binding:    binding,
		Expression: expression,
	})
	return err
}

// ReconcileObject performs a create/update patch against the API server for a
// desired object applying the provided mutate function on differences.
func (ec *ExtensionController) ReconcileObject(ctx context.Context, obj client.Object, desired client.Object, mutateFn exttypes.MutateFn) (client.Object, error) {
	obj, err := ec.ReconcileResource(ctx, obj, desired, basereconciler.MutateFn(mutateFn)) // TODO(didierofrivia): Next iteration, use policy machinery
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// ClearPolicy removes server side state associated with a policy (mutators,
// subscriptions) after deletion.
func (ec *ExtensionController) ClearPolicy(ctx context.Context, namespace, name, kind string) error {
	pbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
		},
	}

	resp, err := ec.extensionClient.client.ClearPolicy(ctx, &extpb.ClearPolicyRequest{
		Policy: pbPolicy,
	})

	ec.logger.Info("cleared policy", "subscriptions", resp.GetClearedSubscriptions(), "mutators", resp.GetClearedMutators())
	return err
}

// RegisterActionMethod registers an external gRPC service with the operator
// so that it becomes available to the data plane. The operator performs a
// reachability check and, if successful, creates the necessary Envoy cluster
// and wasm service entries.
func (ec *ExtensionController) RegisterActionMethod(ctx context.Context, policy exttypes.Policy, svc exttypes.ActionMethodConfig) error {
	pbPolicy := convertPolicyToProtobuf(policy)

	_, err := ec.extensionClient.client.RegisterActionMethod(ctx, &extpb.RegisterActionMethodRequest{
		Policy:          pbPolicy,
		Name:            svc.Name,
		Url:             svc.URL,
		Service:         svc.Service,
		Method:          svc.Method,
		MessageTemplate: svc.MessageTemplate,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
			return fmt.Errorf("%w: %s", exttypes.ErrUpstreamUnreachable, st.Message())
		}
		return err
	}
	return nil
}

// NewPipeline creates a Pipeline bound to the given policy. No I/O is performed;
// the returned Pipeline captures the policy and gRPC client for later use.
func (ec *ExtensionController) NewPipeline(policy exttypes.Policy) exttypes.Pipeline {
	return &PipelineImpl{
		policy:        policy,
		client:        ec.extensionClient.client,
		populatedVars: make(map[string]bool),
	}
}

type pipelinePhase = string

const (
	phaseRequest  pipelinePhase = "request"
	phaseResponse pipelinePhase = "response"
)

type pipelineEntry struct {
	action exttypes.Action
	phase  pipelinePhase
}

// PipelineImpl implements Pipeline by accumulating actions locally with
// ordering validation. Commit sends all actions to the operator atomically.
type PipelineImpl struct {
	policy        exttypes.Policy
	client        extpb.ExtensionServiceClient
	actions       []pipelineEntry
	populatedVars map[string]bool
}

func (p *PipelineImpl) OnHTTPRequest(actions ...exttypes.Action) error {
	for _, entry := range p.actions {
		if entry.phase == phaseResponse {
			return fmt.Errorf("cannot add request actions after response actions have been added")
		}
	}
	return p.validateAndAppend(phaseRequest, actions)
}

func (p *PipelineImpl) OnHTTPResponse(actions ...exttypes.Action) error {
	return p.validateAndAppend(phaseResponse, actions)
}

func (p *PipelineImpl) validateAndAppend(phase string, actions []exttypes.Action) error {
	batchVars := make(map[string]bool)
	for _, action := range actions {
		if grpc, ok := action.(exttypes.GRPCMethodAction); ok && grpc.Var != "" {
			batchVars[grpc.Var] = true
		}
	}

	varPatterns := make(map[string]*regexp.Regexp, len(batchVars)+len(p.populatedVars))
	for varName := range p.populatedVars {
		varPatterns[varName] = regexp.MustCompile(`\b` + regexp.QuoteMeta(varName) + `\b`)
	}
	for varName := range batchVars {
		if _, exists := varPatterns[varName]; !exists {
			varPatterns[varName] = regexp.MustCompile(`\b` + regexp.QuoteMeta(varName) + `\b`)
		}
	}

	localPopulated := make(map[string]bool, len(p.populatedVars))
	for k := range p.populatedVars {
		localPopulated[k] = true
	}

	for _, action := range actions {
		if _, ok := action.(exttypes.FailAction); ok {
			refsVar := false
			for _, expr := range action.CelExpressions() {
				for _, pattern := range varPatterns {
					if pattern.MatchString(expr) {
						refsVar = true
						break
					}
				}
				if refsVar {
					break
				}
			}
			if !refsVar {
				return fmt.Errorf("fail action must reference a gRPC response variable")
			}
		}

		exprs := action.CelExpressions()
		for _, expr := range exprs {
			for varName, pattern := range varPatterns {
				if !localPopulated[varName] && pattern.MatchString(expr) {
					return fmt.Errorf("action references variable %q before it is populated", varName)
				}
			}
		}

		if grpc, ok := action.(exttypes.GRPCMethodAction); ok && grpc.Var != "" {
			if localPopulated[grpc.Var] {
				return fmt.Errorf("duplicate variable name %q", grpc.Var)
			}
			localPopulated[grpc.Var] = true
		}
	}

	for k := range localPopulated {
		p.populatedVars[k] = true
	}
	for _, action := range actions {
		p.actions = append(p.actions, pipelineEntry{action: action, phase: phase})
	}
	return nil
}

func (p *PipelineImpl) Commit(ctx context.Context) error {
	entries := make([]*extpb.ActionEntry, 0, len(p.actions))
	for _, pe := range p.actions {
		entries = append(entries, convertAction(pe))
	}
	_, err := p.client.PipelineCommit(ctx, &extpb.PipelineCommitRequest{
		Policy:  convertPolicyToProtobuf(p.policy),
		Actions: entries,
	})
	return err
}

func convertAction(pe pipelineEntry) *extpb.ActionEntry {
	entry := &extpb.ActionEntry{Phase: pe.phase}
	pe.action.PopulateProtobuf(entry)
	return entry
}

// Manager returns the underlying controller-runtime Manager.
func (ec *ExtensionController) Manager() ctrlruntime.Manager {
	return ec.manager
}
