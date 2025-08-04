package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
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
	ExtensionFinalizer = "kuadrant.io/extensions"
)

type ExtensionConfig struct {
	Name         string
	PolicyKind   string
	ForType      client.Object
	Reconcile    exttypes.ReconcileFn
	WatchSources []ctrlruntimesrc.Source
}

type ExtensionController struct {
	config ExtensionConfig

	logger          logr.Logger
	manager         ctrlruntime.Manager
	extensionClient *extensionClient
	eventCache      *EventTypeCache

	*basereconciler.BaseReconciler // TODO(didierofrivia): Next iteration, use policy machinery
}

func (ec *ExtensionController) Start(ctx context.Context) error {
	stopCh := make(chan struct{})
	// todo(adam-cattermole): how big do we make the reconcile event channel?
	//	 how many should we queue before we block?
	reconcileChan := make(chan ctrlruntimeevent.GenericEvent, 50)

	// Add the channel source to our watch sources
	channelSource := ctrlruntimesrc.Channel(reconcileChan, &ctrlruntimehandler.EnqueueRequestForObject{})
	watchSources := append(ec.config.WatchSources, channelSource)

	if ec.manager != nil {
		ctrl, err := ctrlruntimectrl.New(ec.config.Name, ec.manager, ctrlruntimectrl.Options{Reconciler: ec})
		if err != nil {
			return fmt.Errorf("error creating controller: %w", err)
		}

		for _, source := range watchSources {
			err := ctrl.Watch(source)
			if err != nil {
				return fmt.Errorf("error watching resource: %w", err)
			}
		}

		go ec.Subscribe(ctx, reconcileChan)
		err = ec.manager.Start(ctx)
		if err != nil {
			return fmt.Errorf("error starting manager: %w", err)
		}
		return nil
	}

	// keep the thread alive
	ec.logger.Info("waiting until stop signal is received")
	wait.Until(func() {
		<-ctx.Done()
		close(stopCh)
	}, time.Second, stopCh)
	ec.logger.Info("stop signal received. finishing controller...")

	return nil
}

func (ec *ExtensionController) Subscribe(ctx context.Context, reconcileChan chan ctrlruntimeevent.GenericEvent) {
	err := ec.extensionClient.subscribe(ctx, ec.config.PolicyKind, func(response *extpb.SubscribeResponse) {
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
			reconcileChan <- ctrlruntimeevent.GenericEvent{Object: trigger}
		}
	})
	if err != nil {
		ec.logger.Error(err, "grpc subscribe failed")
	}
}

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

func (ec *ExtensionController) AddDataTo(ctx context.Context, requester exttypes.Policy, target exttypes.Policy, binding string, expression string) error {
	pbRequester := convertPolicyToProtobuf(requester)
	pbTarget := convertPolicyToProtobuf(target)

	_, err := ec.extensionClient.client.RegisterMutator(ctx, &extpb.RegisterMutatorRequest{
		Requester:  pbRequester,
		Target:     pbTarget,
		Binding:    binding,
		Expression: expression,
	})
	return err
}

func (ec *ExtensionController) ReconcileObject(ctx context.Context, obj client.Object, desired client.Object, mutateFn exttypes.MutateFn) (client.Object, error) {
	obj, err := ec.ReconcileResource(ctx, obj, desired, basereconciler.MutateFn(mutateFn)) // TODO(didierofrivia): Next iteration, use policy machinery
	if err != nil {
		return nil, err
	}
	return obj, nil
}

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

func (ec *ExtensionController) Manager() ctrlruntime.Manager {
	return ec.manager
}
