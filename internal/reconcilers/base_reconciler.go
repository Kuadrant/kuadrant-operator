/*
Copyright 2021 Red Hat, Inc.

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

package reconcilers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

type StatusMeta struct {
	// ObservedGeneration reflects the generation of the most recently observed spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func (meta *StatusMeta) GetObservedGeneration() int64  { return meta.ObservedGeneration }
func (meta *StatusMeta) SetObservedGeneration(o int64) { meta.ObservedGeneration = o }

// StatusMutator is an interface to hold mutator functions for status updates.
type StatusMutator interface {
	Mutate(obj client.Object) (bool, error)
}

// StatusMutatorFunc is a function adaptor for StatusMutators.
type StatusMutatorFunc func(client.Object) (bool, error)

// Mutate adapts the MutatorFunc to fit through the StatusMutator interface.
func (s StatusMutatorFunc) Mutate(o client.Object) (bool, error) {
	if s == nil {
		return false, nil
	}

	return s(o)
}

// MutateFn is a function which mutates the existing object into it's desired state.
type MutateFn func(existing, desired client.Object) (bool, error)

func CreateOnlyMutator(_, _ client.Object) (bool, error) {
	return false, nil
}

type BaseReconciler struct {
	client          client.Client
	scheme          *runtime.Scheme
	apiClientReader client.Reader
}

// blank assignment to verify that BaseReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &BaseReconciler{}

func NewBaseReconciler(client client.Client, scheme *runtime.Scheme, apiClientReader client.Reader) *BaseReconciler {
	return &BaseReconciler{
		client:          client,
		scheme:          scheme,
		apiClientReader: apiClientReader,
	}
}

func (b *BaseReconciler) Reconcile(context.Context, ctrl.Request) (ctrl.Result, error) {
	return reconcile.Result{}, nil
}

// Client returns a split client that reads objects from
// the cache and writes to the Kubernetes APIServer
func (b *BaseReconciler) Client() client.Client {
	return b.client
}

// APIClientReader return a client that directly reads objects
// from the Kubernetes APIServer
func (b *BaseReconciler) APIClientReader() client.Reader {
	return b.apiClientReader
}

func (b *BaseReconciler) Scheme() *runtime.Scheme {
	return b.scheme
}

// ReconcileResource attempts to mutate the existing state
// in order to match the desired state. The object's desired state must be reconciled
// with the existing state inside the passed in callback MutateFn.
//
// obj: Object of the same type as the 'desired' object.
//
//	Used to read the resource from the kubernetes cluster.
//	Could be zero-valued initialized object.
//
// desired: Object representing the desired state
//
// It returns an error.
func (b *BaseReconciler) ReconcileResource(ctx context.Context, obj, desired client.Object, mutateFn MutateFn) error {
	key := client.ObjectKeyFromObject(desired)

	if err := b.Client().Get(ctx, key, obj); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// Not found
		if !utils.IsObjectTaggedToDelete(desired) {
			return b.CreateResource(ctx, desired)
		}

		// Marked for deletion and not found. Nothing to do.
		return nil
	}

	// item found successfully
	if utils.IsObjectTaggedToDelete(desired) {
		return b.DeleteResource(ctx, desired)
	}

	desired.SetResourceVersion(obj.GetResourceVersion())
	if err := b.Client().Update(ctx, desired, client.DryRunAll); err != nil {
		return err
	}

	update, err := mutateFn(obj, desired)
	if err != nil {
		return err
	}

	if update {
		return b.UpdateResource(ctx, obj)
	}

	return nil
}

func (b *BaseReconciler) ReconcileResourceStatus(ctx context.Context, objKey client.ObjectKey, obj client.Object, mutator StatusMutator) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	if err := b.Client().Get(ctx, objKey, obj); err != nil {
		return err
	}

	update, err := mutator.Mutate(obj)
	if err != nil {
		return err
	}

	if !update {
		// Steady state, early return 🎉
		logger.V(1).Info("status was not updated")
		return nil
	}

	updateErr := b.Client().Status().Update(ctx, obj)
	logger.V(1).Info("updating status", "err", updateErr)
	if updateErr != nil {
		return updateErr
	}

	return nil
}

func (b *BaseReconciler) GetResource(ctx context.Context, objKey types.NamespacedName, obj client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("get object", "kind", strings.Replace(fmt.Sprintf("%T", obj), "*", "", 1), "name", objKey.Name, "namespace", objKey.Namespace)
	return b.Client().Get(ctx, objKey, obj)
}

func (b *BaseReconciler) CreateResource(ctx context.Context, obj client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("create object", "kind", strings.Replace(fmt.Sprintf("%T", obj), "*", "", 1), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return b.Client().Create(ctx, obj)
}

func (b *BaseReconciler) UpdateResource(ctx context.Context, obj client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("update object", "kind", strings.Replace(fmt.Sprintf("%T", obj), "*", "", 1), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return b.Client().Update(ctx, obj)
}

func (b *BaseReconciler) DeleteResource(ctx context.Context, obj client.Object, options ...client.DeleteOption) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("delete object", "kind", strings.Replace(fmt.Sprintf("%T", obj), "*", "", 1), "name", obj.GetName(), "namespace", obj.GetNamespace())
	if obj.GetDeletionTimestamp() != nil {
		return nil
	}
	return b.Client().Delete(ctx, obj, options...)
}

func (b *BaseReconciler) UpdateResourceStatus(ctx context.Context, obj client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("update object status", "kind", strings.Replace(fmt.Sprintf("%T", obj), "*", "", 1), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return b.Client().Status().Update(ctx, obj)
}

func (b *BaseReconciler) AddFinalizer(ctx context.Context, obj client.Object, finalizer string) error {
	controllerutil.AddFinalizer(obj, finalizer)
	if err := b.UpdateResource(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func (b *BaseReconciler) RemoveFinalizer(ctx context.Context, obj client.Object, finalizer string) error {
	controllerutil.RemoveFinalizer(obj, finalizer)
	if err := b.UpdateResource(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}
