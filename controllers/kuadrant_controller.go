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

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const (
	kuadrantFinalizer     = "kuadrant.io/finalizer"
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

//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/finalizers,verbs=update

//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;clusterroles;rolebindings;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="kuadrant.io",resources=authpolicies;ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="kuadrant.io",resources=authpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="kuadrant.io",resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="kuadrant.io",resources=authpolicies/status,verbs=get;patch;update
//+kubebuilder:rbac:groups="kuadrant.io",resources=ratelimitpolicies/status,verbs=get;patch;update
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
	logger, _ := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: iopName(), Namespace: iopNamespace()}
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
		// It should exists, NotFound also considered as error
		logger.Error(err, "failed to get istiooperator object", "key", iopKey)
		return err
	}

	meshConfig, err := meshConfigFromStruct(iop.Spec.MeshConfig)
	if err != nil {
		return err
	}
	extensionProviders := extensionProvidersFromMeshConfig(meshConfig)

	if !hasKuadrantAuthorizer(extensionProviders) {
		return nil
	}

	for idx, extensionProvider := range extensionProviders {
		name := extensionProvider.Name
		if name == extAuthorizerName {
			// deletes the element in the array
			extensionProviders = append(extensionProviders[:idx], extensionProviders[idx+1:]...)
			meshConfig.ExtensionProviders = extensionProviders
			meshConfigStruct, err := meshConfigToStruct(meshConfig)
			if err != nil {
				return err
			}
			iop.Spec.MeshConfig = meshConfigStruct
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
	logger, _ := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: iopName(), Namespace: iopNamespace()}
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
		// It should exists, NotFound also considered as error
		logger.Error(err, "failed to get istiooperator object", "key", iopKey)
		return err
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

	meshConfig, err := meshConfigFromStruct(iop.Spec.MeshConfig)
	if err != nil {
		return err
	}
	extensionProviders := extensionProvidersFromMeshConfig(meshConfig)

	if hasKuadrantAuthorizer(extensionProviders) {
		return nil
	}

	meshConfig.ExtensionProviders = append(meshConfig.ExtensionProviders, createKuadrantAuthorizer(kObj.Namespace))
	meshConfigStruct, err := meshConfigToStruct(meshConfig)
	if err != nil {
		return err
	}
	iop.Spec.MeshConfig = meshConfigStruct
	logger.Info("adding external authorizer to meshconfig")
	if err := r.Client().Update(ctx, iop); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) reconcileSpec(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (ctrl.Result, error) {
	var reconcileAuthorino = true

	if err := r.registerExternalAuthorizer(ctx, kObj); err != nil {
		if apierrors.IsNotFound(err) {
			reconcileAuthorino = false
		} else {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileLimitador(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if reconcileAuthorino {
		if err := r.reconcileAuthorino(ctx, kObj); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func iopName() string {
	return common.FetchEnv("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

func iopNamespace() string {
	return common.FetchEnv("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func hasKuadrantAuthorizer(extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) bool {
	// IstioOperator
	//
	//meshConfig:
	//    extensionProviders:
	//      - envoyExtAuthzGrpc:
	//          port: POST
	//          service: AUTHORINO SERVICE
	//        name: kuadrant-authorization

	if len(extensionProviders) == 0 {
		return false
	}

	for _, extensionProvider := range extensionProviders {
		if extensionProvider.Name == extAuthorizerName {
			return true
		}
	}

	return false
}

func createKuadrantAuthorizer(namespace string) *istiomeshv1alpha1.MeshConfig_ExtensionProvider {
	envoyExtAuthGRPC := &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
		EnvoyExtAuthzGrpc: &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
			Port:    50051,
			Service: fmt.Sprintf("authorino-authorino-authorization.%s.svc.cluster.local", namespace),
		},
	}
	return &istiomeshv1alpha1.MeshConfig_ExtensionProvider{
		Name:     extAuthorizerName,
		Provider: envoyExtAuthGRPC,
	}
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

func meshConfigFromStruct(structure *structpb.Struct) (*istiomeshv1alpha1.MeshConfig, error) {
	if structure == nil {
		return &istiomeshv1alpha1.MeshConfig{}, nil
	}

	meshConfigJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	meshConfig := &istiomeshv1alpha1.MeshConfig{}
	// istiomeshv1alpha1.MeshConfig doesn't implement JSON/Yaml marshalling, only protobuf
	if err = protojson.Unmarshal(meshConfigJSON, meshConfig); err != nil {
		return nil, err
	}

	return meshConfig, nil
}

func meshConfigToStruct(config *istiomeshv1alpha1.MeshConfig) (*structpb.Struct, error) {
	configJSON, err := protojson.Marshal(config)
	if err != nil {
		return nil, err
	}
	configStruct := &structpb.Struct{}

	if err = configStruct.UnmarshalJSON(configJSON); err != nil {
		return nil, err
	}
	return configStruct, nil
}

func extensionProvidersFromMeshConfig(config *istiomeshv1alpha1.MeshConfig) (extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) {
	extensionProviders = config.ExtensionProviders
	if len(extensionProviders) == 0 {
		extensionProviders = make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	}
	return
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
