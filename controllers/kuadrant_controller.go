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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/kuadrantcontrollermanifests"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const (
	kuadrantFinalizer     = "kuadrant.kuadrant.io/finalizer"
	extAuthorizerName     = "kuadrant-authorization"
	envLimitadorNamespace = "LIMITADOR_NAMESPACE"
	envLimitadorName      = "LIMITADOR_NAME"
)

var (
	limitadorName = common.FetchEnv(envLimitadorName, "limitador")
)

// KuadrantReconciler reconciles a Kuadrant object
type KuadrantReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.kuadrant.io,resources=kuadrants/finalizers,verbs=update

//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;clusterroles;rolebindings;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apim.kuadrant.io",resources=authpolicies;ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apim.kuadrant.io",resources=authpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="apim.kuadrant.io",resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="apim.kuadrant.io",resources=authpolicies/status,verbs=get;patch;update
//+kubebuilder:rbac:groups="apim.kuadrant.io",resources=ratelimitpolicies/status,verbs=get;patch;update
//+kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=gateways,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=httproutes,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=operator.authorino.kuadrant.io,resources=authorinos,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups="networking.istio.io",resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="security.istio.io",resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=install.istio.io,resources=istiooperators,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;delete;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *KuadrantReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("kuadrant", req.NamespacedName)
	logger.Info("Reconciling")
	ctx := logr.NewContext(eventCtx, logger)

	kObj := &kuadrantv1beta1.Kuadrant{}
	if err := r.Client().Get(ctx, req.NamespacedName, kObj); err != nil {
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
		if err := r.Client().Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
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
		if err := r.Client().Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
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
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
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
	if err := r.Client().Update(ctx, iop); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) registerExternalAuthorizer(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: iopName(), Namespace: iopNamespace()}
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
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
	envoyExtAuthzGrpc["service"] = fmt.Sprintf("authorino-authorino-authorization.%s.svc.cluster.local", kObj.Namespace)

	kuadrantExtensionProvider := make(map[string]interface{})
	kuadrantExtensionProvider["name"] = extAuthorizerName
	kuadrantExtensionProvider["envoyExtAuthzGrpc"] = envoyExtAuthzGrpc

	iop.Spec.MeshConfig["extensionProviders"] = append(extensionProviders, kuadrantExtensionProvider)
	logger.Info("adding external authorizer to meshconfig")
	if err := r.Client().Update(ctx, iop); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) reconcileSpec(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (ctrl.Result, error) {
	if err := r.registerExternalAuthorizer(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLimitador(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileKuadrantController(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAuthorino(ctx, kObj); err != nil {
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

func (r *KuadrantReconciler) reconcileLimitador(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	limitador := &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      limitadorName,
			Namespace: kObj.Namespace,
		},
		Spec: limitadorv1alpha1.LimitadorSpec{},
	}

	err := r.SetOwnerReference(kObj, limitador)
	if err != nil {
		return err
	}

	return r.ReconcileResource(ctx, &limitadorv1alpha1.Limitador{}, limitador, reconcilers.CreateOnlyMutator)
}

func (r *KuadrantReconciler) reconcileKuadrantController(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger := logr.FromContext(ctx)
	kuadrantControllerVersion, err := common.KuadrantControllerImage(ctx, r.Scheme)
	if err != nil {
		return err
	}
	logger.Info("Deploying kuadrant controller", "version", kuadrantControllerVersion)

	data, err := kuadrantcontrollermanifests.Content()
	if err != nil {
		return err
	}

	return common.DecodeFile(ctx, data, r.Scheme, r.createOnlyInKuadrantNSCb(ctx, kObj))
}

func (r *KuadrantReconciler) createOnlyInKuadrantNSCb(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) common.DecodeCallback {
	return func(obj runtime.Object) error {
		logger := logr.FromContext(ctx)
		k8sObj, ok := obj.(client.Object)
		if !ok {
			return errors.New("runtime.Object could not be type asserted to client.Object")
		}

		// Create in Kuadrant CR's namespace
		k8sObj.SetNamespace(kObj.Namespace)
		err := r.SetOwnerReference(kObj, k8sObj)
		if err != nil {
			return err
		}

		var newObj client.Object
		newObj = k8sObj

		switch obj := k8sObj.(type) {
		case *appsv1.Deployment: // If it's a Deployment obj, it adds the required env vars
			obj.Spec.Template.Spec.Containers[0].Env = append(
				obj.Spec.Template.Spec.Containers[0].Env,
				v1.EnvVar{Name: envLimitadorNamespace, Value: kObj.Namespace},
				v1.EnvVar{Name: envLimitadorName, Value: limitadorName},
				// env var name taken from https://github.com/Kuadrant/kuadrant-controller/blob/4e9763bbabc8a7b5f7695aa4f53d9edc0c376ba3/pkg/rlptools/wasm_utils.go#L18
				v1.EnvVar{Name: "WASM_FILTER_IMAGE", Value: common.GetWASMShimImageVersion()},
			)
			newObj = obj
		// TODO: DRY the following 2 case switches
		case *rbacv1.RoleBinding:
			if obj.Name == "kuadrant-leader-election-rolebinding" {
				for i, subject := range obj.Subjects {
					if subject.Name == "kuadrant-controller-manager" {
						obj.Subjects[i].Namespace = kObj.Namespace
					}
				}
			}
			newObj = obj
		case *rbacv1.ClusterRoleBinding:
			if obj.Name == "kuadrant-manager-rolebinding" {
				for i, subject := range obj.Subjects {
					if subject.Name == "kuadrant-controller-manager" {
						obj.Subjects[i].Namespace = kObj.Namespace
					}
				}
			}
			newObj = obj
		default:
		}
		newObjCloned := newObj.DeepCopyObject()
		err = r.Client().Create(ctx, newObj)

		k8sObjKind := newObjCloned.GetObjectKind()
		logger.V(1).Info("create resource", "GKV", k8sObjKind.GroupVersionKind(), "name", newObj.GetName(), "error", err)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Omit error
				logger.Info("Already exists", "GKV", k8sObjKind.GroupVersionKind(), "name", newObj.GetName())
			} else {
				return err
			}
		}
		return nil
	}
}

func (r *KuadrantReconciler) reconcileAuthorino(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	tmpFalse := false
	authorino := &authorinov1beta1.Authorino{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Authorino",
			APIVersion: "operator.authorino.kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authorino",
			Namespace: kObj.Namespace,
		},
		Spec: authorinov1beta1.AuthorinoSpec{
			ClusterWide: true,
			Listener: authorinov1beta1.Listener{
				Tls: authorinov1beta1.Tls{
					Enabled: &tmpFalse,
				},
			},
			OIDCServer: authorinov1beta1.OIDCServer{
				Tls: authorinov1beta1.Tls{
					Enabled: &tmpFalse,
				},
			},
		},
	}

	err := r.SetOwnerReference(kObj, authorino)
	if err != nil {
		return err
	}

	return r.ReconcileResource(ctx, &authorinov1beta1.Authorino{}, authorino, reconcilers.CreateOnlyMutator)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KuadrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Owns(&appsv1.Deployment{}).
		Owns(&limitadorv1alpha1.Limitador{}).
		Owns(&authorinov1beta1.Authorino{}).
		Complete(r)
}
