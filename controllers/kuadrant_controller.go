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
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	maistrav1 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v1"
	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const (
	kuadrantFinalizer = "kuadrant.io/finalizer"
	extAuthorizerName = "kuadrant-authorization"
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
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshcontrolplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshmemberrolls,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=update

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

		if err := r.unregisterExternalAuthorizer(ctx, kObj); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.removeAnnotationFromGateways(ctx, kObj); err != nil {
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

	if gwErr := r.reconcileClusterGateways(ctx, kObj); gwErr != nil {
		logger.V(1).Error(gwErr, "Reconciling cluster gateways failed")
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

func (r *KuadrantReconciler) unregisterExternalAuthorizer(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	err := r.unregisterExternalAuthorizerIstio(ctx)

	if err != nil && apimeta.IsNoMatchError(err) {
		err = r.unregisterExternalAuthorizerOSSM(ctx, kObj)
	}

	if err != nil {
		logger.Error(err, "failed fo get service mesh control plane")
	}

	return err
}

func (r *KuadrantReconciler) unregisterExternalAuthorizerIstio(ctx context.Context) error {
	logger, _ := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
		logger.V(1).Info("failed to get istiooperator object", "key", iopKey, "err", err)
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

func (r *KuadrantReconciler) unregisterExternalAuthorizerOSSM(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	if err := r.unregisterFromServiceMeshMemberRoll(ctx, kObj); err != nil {
		return err
	}

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.Client().Get(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		return err
	}

	if smcp.Spec.TechPreview == nil {
		smcp.Spec.TechPreview = maistrav1.NewHelmValues(nil)
	}

	var meshConfig *istiomeshv1alpha1.MeshConfig

	if conf, found, err := smcp.Spec.TechPreview.GetMap("meshConfig"); err != nil {
		return err
	} else if found {
		meshConfigStruct, err := structpb.NewStruct(conf)
		if err != nil {
			return err
		}
		meshConfig, _ = meshConfigFromStruct(meshConfigStruct)
	} else {
		meshConfig = &istiomeshv1alpha1.MeshConfig{}
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
			smcp.Spec.TechPreview.SetField("meshConfig", meshConfigStruct.AsMap())
			break
		}
	}

	logger.Info("remove external authorizer from meshconfig")
	if err := r.Client().Update(ctx, smcp); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) unregisterFromServiceMeshMemberRoll(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	return r.ReconcileResource(ctx, &maistrav1.ServiceMeshMemberRoll{}, buildServiceMeshMemberRoll(kObj), func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*maistrav1.ServiceMeshMemberRoll)
		if !ok {
			return false, fmt.Errorf("%T is not a *maistrav1.ServiceMeshMemberRoll", existingObj)
		}
		desired, ok := desiredObj.(*maistrav1.ServiceMeshMemberRoll)
		if !ok {
			return false, fmt.Errorf("%T is not a *maistrav1.ServiceMeshMemberRoll", desiredObj)
		}
		desired.Spec.Members = []string{}

		update := false
		for _, member := range existing.Spec.Members {
			if member == kObj.Namespace {
				update = true
			} else {
				desired.Spec.Members = append(desired.Spec.Members, member)
			}
		}
		existing.Spec.Members = desired.Spec.Members
		return update, nil
	})
}

func (r *KuadrantReconciler) registerExternalAuthorizer(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	err := r.registerExternalAuthorizerIstio(ctx, kObj)

	if err != nil && apimeta.IsNoMatchError(err) {
		err = r.registerExternalAuthorizerOSSM(ctx, kObj)
	}

	if err != nil {
		logger.Error(err, "failed fo get service mesh control plane")
	}

	return err
}

func (r *KuadrantReconciler) registerExternalAuthorizerIstio(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)
	iop := &iopv1alpha1.IstioOperator{}

	iopKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.Client().Get(ctx, iopKey, iop); err != nil {
		logger.V(1).Info("failed to get istiooperator object", "key", iopKey, "err", err)
		return err
	}

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

func (r *KuadrantReconciler) registerExternalAuthorizerOSSM(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	if err := r.registerToServiceMeshMemberRoll(ctx, kObj); err != nil {
		return err
	}

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.Client().Get(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		return err
	}

	if smcp.Spec.TechPreview == nil {
		smcp.Spec.TechPreview = maistrav1.NewHelmValues(nil)
	}

	var meshConfig *istiomeshv1alpha1.MeshConfig

	if conf, found, err := smcp.Spec.TechPreview.GetMap("meshConfig"); err != nil {
		return err
	} else if found {
		meshConfigStruct, err := structpb.NewStruct(conf)
		if err != nil {
			return err
		}
		meshConfig, _ = meshConfigFromStruct(meshConfigStruct)
	} else {
		meshConfig = &istiomeshv1alpha1.MeshConfig{}
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
	smcp.Spec.TechPreview.SetField("meshConfig", meshConfigStruct.AsMap())
	logger.Info("adding external authorizer to meshconfig")
	if err := r.Client().Update(ctx, smcp); err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) registerToServiceMeshMemberRoll(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	return r.ReconcileResource(ctx, &maistrav1.ServiceMeshMemberRoll{}, buildServiceMeshMemberRoll(kObj), func(existingObj, _ client.Object) (bool, error) {
		existing, ok := existingObj.(*maistrav1.ServiceMeshMemberRoll)
		if !ok {
			return false, fmt.Errorf("%T is not a *maistrav1.ServiceMeshMemberRoll", existingObj)
		}

		for _, member := range existing.Spec.Members {
			if member == kObj.Namespace {
				return false, nil
			}
		}
		existing.Spec.Members = append(existing.Spec.Members, kObj.Namespace)
		return true, nil
	})
}

func (r *KuadrantReconciler) reconcileSpec(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (ctrl.Result, error) {
	if err := r.registerExternalAuthorizer(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLimitador(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAuthorino(ctx, kObj); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func controlPlaneProviderName() string {
	return common.FetchEnv("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

func controlPlaneProviderNamespace() string {
	return common.FetchEnv("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func buildServiceMeshMemberRoll(kObj *kuadrantv1beta1.Kuadrant) *maistrav1.ServiceMeshMemberRoll {
	return &maistrav1.ServiceMeshMemberRoll{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMeshMemberRoll",
			APIVersion: maistrav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: controlPlaneProviderNamespace(),
		},
		Spec: maistrav1.ServiceMeshMemberRollSpec{
			Members: []string{kObj.Namespace},
		},
	}
}

func hasKuadrantAuthorizer(extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) bool {
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

func (r *KuadrantReconciler) reconcileClusterGateways(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	// TODO: After the RFC defined, we might want to get the gw to label/annotate from Kuadrant.Spec or manual labeling/annotation
	gwList := &gatewayapiv1alpha2.GatewayList{}
	if err := r.Client().List(ctx, gwList); err != nil {
		return err
	}
	errGroup, gctx := errgroup.WithContext(ctx)

	for i := range gwList.Items {
		gw := &gwList.Items[i]
		if !common.IsKuadrantManaged(gw) {
			common.AnnotateObject(gw, kObj.Namespace)
			errGroup.Go(func() error {
				select {
				case <-gctx.Done():
					// context cancelled
					return nil
				default:
					if err := r.Client().Update(ctx, gw); err != nil {
						return err
					}
					return nil
				}
			})
		}
	}

	if err := errGroup.Wait(); err != nil {
		return err
	}
	return nil
}

func (r *KuadrantReconciler) removeAnnotationFromGateways(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	gwList := &gatewayapiv1alpha2.GatewayList{}
	if err := r.Client().List(ctx, gwList); err != nil {
		return err
	}
	errGroup, gctx := errgroup.WithContext(ctx)

	for i := range gwList.Items {
		gw := &gwList.Items[i]
		errGroup.Go(func() error {
			select {
			case <-gctx.Done():
				// context cancelled
				return nil
			default:
				// When RFC defined, we could indicate which gateways based on a specific annotation/label
				common.DeleteKuadrantAnnotationFromGateway(gw, kObj.Namespace)
				if err := r.Client().Update(ctx, gw); err != nil {
					return err
				}
				return nil
			}
		})
	}

	if err := errGroup.Wait(); err != nil {
		return err
	}
	return nil
}

func (r *KuadrantReconciler) reconcileLimitador(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	limitador := &limitadorv1alpha1.Limitador{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Limitador",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.LimitadorName,
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

// Builds the Istio/OSSM MeshConfig from a compatible structure:
//   meshConfig:
//     extensionProviders:
//       - envoyExtAuthzGrpc:
//           port: <port>
//           service: <authorino-service>
//         name: kuadrant-authorization
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
