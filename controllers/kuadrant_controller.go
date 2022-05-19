/*
Copyright 2021.

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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

const (
	kuadrantFinalizer = "kuadrant.kuadrant.io/finalizer"
	extAuthorizerName = "kuadrant-authorization"
)

// KuadrantReconciler reconciles a Kuadrant object
type KuadrantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants/finalizers,verbs=update
//+kubebuilder:rbac:groups=install.istio.io,resources=istiooperators,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *KuadrantReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("kuadrant", req.NamespacedName)
	logger.Info("Reconciling")
	ctx := logr.NewContext(eventCtx, logger)

	kObj := &kuadrantv1beta1.Kuadrant{}
	if err := r.Get(ctx, req.NamespacedName, kObj); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no kuadrant object found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get kuadrant object")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(kObj, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if kObj.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(kObj, kuadrantFinalizer) {
		logger.V(1).Info("Handling removal of kuadrant object")

		if err := r.unregisterExternalAuthorizer(ctx); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("removing finalizer")
		controllerutil.RemoveFinalizer(kObj, kuadrantFinalizer)
		if err := r.Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if kObj.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(kObj, kuadrantFinalizer) {
		controllerutil.AddFinalizer(kObj, kuadrantFinalizer)
		if err := r.Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	specResult, specErr := r.reconcileSpec(ctx, kObj)
	if specErr == nil && specResult.Requeue {
		logger.V(1).Info("Reconciling spec not finished. Requeueing.")
		return specResult, nil
	}

	statusResult, statusErr := r.reconcileStatus(ctx, kObj, specErr)

	if specErr != nil {
		return ctrl.Result{}, specErr
	}

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if statusResult.Requeue {
		logger.V(1).Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	logger.Info("successfully reconciled")
	return ctrl.Result{}, nil
}

func (r *KuadrantReconciler) unregisterExternalAuthorizer(ctx context.Context) error {
	logger := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: iopName(), Namespace: iopNamespace()}
	if err := r.Get(ctx, iopKey, iop); err != nil {
		// It should exists, NotFound also considered as error
		logger.Error(err, "failed to get istiooperator object", "key", iopKey)
		return err
	}

	if !hasKuadrantAuthorizer(iop) {
		return nil
	}

	obj, ok := iop.Spec.MeshConfig["extensionProviders"]
	if !ok || obj == nil {
		obj = make([]interface{}, 0)
	}

	extensionProviders, ok := obj.([]interface{})
	if !ok {
		return fmt.Errorf("istiooperator MeshConfig[extensionProviders] type assertion failed: %T", obj)
	}

	for idx := range extensionProviders {
		extensionProvider, ok := extensionProviders[idx].(map[string]interface{})
		if !ok {
			return fmt.Errorf("istiooperator MeshConfig[extensionProviders][idx] type assertion failed: %T", extensionProviders[idx])
		}
		name, ok := extensionProvider["name"]
		if !ok {
			continue
		}
		if name == extAuthorizerName {
			// deletes the element in the array
			extensionProviders = append(extensionProviders[:idx], extensionProviders[idx+1:]...)
			iop.Spec.MeshConfig["extensionProviders"] = extensionProviders
			break
		}
	}

	logger.Info("remove external authorizer from meshconfig")
	if err := r.Update(ctx, iop); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) registerExternalAuthorizer(ctx context.Context) error {
	logger := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: iopName(), Namespace: iopNamespace()}
	if err := r.Get(ctx, iopKey, iop); err != nil {
		// It should exists, NotFound also considered as error
		logger.Error(err, "failed to get istiooperator object", "key", iopKey)
		return err
	}

	if hasKuadrantAuthorizer(iop) {
		return nil
	}

	//meshConfig:
	//    extensionProviders:
	//      - envoyExtAuthzGrpc:
	//          port: POST
	//          service: AUTHORINO SERVICE
	//        name: kuadrant-authorization

	if iop.Spec == nil {
		iop.Spec = &istioapiv1alpha1.IstioOperatorSpec{}
	}

	if iop.Spec.MeshConfig == nil {
		iop.Spec.MeshConfig = make(map[string]interface{})
	}

	obj, ok := iop.Spec.MeshConfig["extensionProviders"]
	if !ok || obj == nil {
		obj = make([]interface{}, 0)
	}

	extensionProviders, ok := obj.([]interface{})
	if !ok {
		return fmt.Errorf("istiooperator extensionprovider type assertion failed: %T", obj)
	}

	envoyExtAuthzGrpc := make(map[string]interface{})
	envoyExtAuthzGrpc["port"] = 50051
	envoyExtAuthzGrpc["service"] = "authorino-authorino-authorization.kuadrant-system.svc.cluster.local"

	kuadrantExtensionProvider := make(map[string]interface{})
	kuadrantExtensionProvider["name"] = extAuthorizerName
	kuadrantExtensionProvider["envoyExtAuthzGrpc"] = envoyExtAuthzGrpc

	iop.Spec.MeshConfig["extensionProviders"] = append(extensionProviders, kuadrantExtensionProvider)
	logger.Info("adding external authorizer to meshconfig")
	if err := r.Update(ctx, iop); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) reconcileSpec(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (ctrl.Result, error) {
	if err := r.registerExternalAuthorizer(ctx); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func iopName() string {
	return common.FetchEnv("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

func iopNamespace() string {
	return common.FetchEnv("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func hasKuadrantAuthorizer(iop *iopv1alpha1.IstioOperator) bool {
	if iop == nil || iop.Spec == nil {
		return false
	}

	// IstioOperator
	//
	//meshConfig:
	//    extensionProviders:
	//      - envoyExtAuthzGrpc:
	//          port: POST
	//          service: AUTHORINO SERVICE
	//        name: kuadrant-authorization

	extensionProvidersObj, ok := iop.Spec.MeshConfig["extensionProviders"]
	if !ok || extensionProvidersObj == nil {
		return false
	}

	extensionProvidersList, ok := extensionProvidersObj.([]interface{})
	if !ok {
		return false
	}

	for idx := range extensionProvidersList {
		extensionProvider, ok := extensionProvidersList[idx].(map[string]interface{})
		if !ok {
			return false
		}
		name, ok := extensionProvider["name"]
		if !ok {
			continue
		}
		if name == extAuthorizerName {
			return true
		}
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *KuadrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Complete(r)
}
