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

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/env"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	maistrav1 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v1"
	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadranttools"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

const (
	kuadrantFinalizer = "kuadrant.io/finalizer"
)

const (
	// (Sail) The istio CR must be named default to process GW API resources
	istioCRName = "default"
)

func controlPlaneConfigMapName() string {
	return env.GetString("ISTIOCONFIGMAP_NAME", "istio")
}

func controlPlaneProviderNamespace() string {
	return env.GetString("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func controlPlaneProviderName() string {
	return env.GetString("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

// KuadrantReconciler reconciles a Kuadrant object
type KuadrantReconciler struct {
	*reconcilers.BaseReconciler
	RestMapper meta.RESTMapper
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/finalizers,verbs=update
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;delete;patch

//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=operator.authorino.kuadrant.io,resources=authorinos,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=install.istio.io,resources=istiooperators,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=operator.istio.io,resources=istios,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshcontrolplanes,verbs=get;list;watch;update;use;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshmembers,verbs=get;list;watch;create;update;delete;patch

// Common permissions required by policy controllers
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

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

	specErr := r.reconcileSpec(ctx, kObj)

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
	isIstioInstalled, err := r.unregisterExternalAuthorizerIstio(ctx, kObj)

	if err == nil && !isIstioInstalled {
		err = r.unregisterExternalAuthorizerOSSM(ctx, kObj)
	}

	return err
}

func (r *KuadrantReconciler) unregisterExternalAuthorizerIstio(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := r.getIstioConfigObjects(ctx)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := istio.NewKuadrantAuthorizer(kObj.Namespace)

	for _, config := range configsToUpdate {
		hasAuthorizer, err := istio.HasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if hasAuthorizer {
			if err = istio.UnregisterKuadrantAuthorizer(config, kuadrantAuthorizer); err != nil {
				return true, err
			}

			logger.Info("remove external authorizer from istio meshconfig")
			if err = r.UpdateResource(ctx, config.GetConfigObject()); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

func (r *KuadrantReconciler) unregisterExternalAuthorizerOSSM(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.Client().Get(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.Info("OSSM installation as GatewayAPI provider not found")
			return nil
		}
		return err
	}

	smcpWrapper := istio.NewOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := istio.NewKuadrantAuthorizer(kObj.Namespace)

	hasAuthorizer, err := istio.HasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if hasAuthorizer {
		err = istio.UnregisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
		if err != nil {
			return err
		}
		logger.Info("removing external authorizer from  OSSM meshconfig")
		if err := r.UpdateResource(ctx, smcpWrapper.GetConfigObject()); err != nil {
			return err
		}
	}

	return nil
}

func (r *KuadrantReconciler) registerExternalAuthorizer(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	isIstioInstalled, err := r.registerExternalAuthorizerIstio(ctx, kObj)

	if err == nil && !isIstioInstalled {
		err = r.registerExternalAuthorizerOSSM(ctx, kObj)
	}

	return err
}

func (r *KuadrantReconciler) registerExternalAuthorizerIstio(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := r.getIstioConfigObjects(ctx)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := istio.NewKuadrantAuthorizer(kObj.Namespace)
	for _, config := range configsToUpdate {
		hasKuadrantAuthorizer, err := istio.HasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if !hasKuadrantAuthorizer {
			err = istio.RegisterKuadrantAuthorizer(config, kuadrantAuthorizer)
			if err != nil {
				return true, err
			}
			logger.Info("adding external authorizer to istio meshconfig")
			if err = r.UpdateResource(ctx, config.GetConfigObject()); err != nil {
				return true, err
			}
		}
	}

	return true, nil
}

func (r *KuadrantReconciler) registerExternalAuthorizerOSSM(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, _ := logr.FromContext(ctx)

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.GetResource(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.Info("OSSM installation as GatewayAPI provider not found")
			return nil
		}
	}

	if err := r.registerServiceMeshMember(ctx, kObj); err != nil {
		return err
	}

	smcpWrapper := istio.NewOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := istio.NewKuadrantAuthorizer(kObj.Namespace)

	hasAuthorizer, err := istio.HasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if !hasAuthorizer {
		err = istio.RegisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
		if err != nil {
			return err
		}
		logger.Info("adding external authorizer to OSSM meshconfig")
		if err := r.UpdateResource(ctx, smcpWrapper.GetConfigObject()); err != nil {
			return err
		}
	}

	return nil
}

func (r *KuadrantReconciler) getIstioConfigObjects(ctx context.Context) ([]istio.ConfigWrapper, error) {
	logger, _ := logr.FromContext(ctx)
	var configsToUpdate []istio.ConfigWrapper

	iop := &iopv1alpha1.IstioOperator{}
	istKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	err := r.GetResource(ctx, istKey, iop)
	// TODO(eguzki): 🔥 this spaghetti code 🔥
	if err == nil {
		configsToUpdate = append(configsToUpdate, istio.NewOperatorWrapper(iop))
	} else if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
		// IstioOperator not existing or not CRD not found, so check for Istio CR instead
		ist := &istiov1alpha1.Istio{}
		istKey := client.ObjectKey{Name: istioCRName}
		if err := r.GetResource(ctx, istKey, ist); err != nil {
			logger.V(1).Info("failed to get istio object", "key", istKey, "err", err)
			if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
				// return nil and nil if there's no istiooperator or istio CR
				logger.Info("Istio installation as GatewayAPI provider not found")
				return nil, nil
			}
			// return nil and err if there's an error other than not found (no istio CR)
			return nil, err
		}
		configsToUpdate = append(configsToUpdate, istio.NewSailWrapper(ist))
	} else {
		logger.V(1).Info("failed to get istiooperator object", "key", istKey, "err", err)
		return nil, err
	}

	istioConfigMap := &corev1.ConfigMap{}
	if err := r.GetResource(ctx, client.ObjectKey{Name: controlPlaneConfigMapName(), Namespace: controlPlaneProviderNamespace()}, istioConfigMap); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.V(1).Info("failed to get istio configMap", "key", istKey, "err", err)
			return configsToUpdate, err
		}
	} else {
		configsToUpdate = append(configsToUpdate, istio.NewConfigMapWrapper(istioConfigMap))
	}
	return configsToUpdate, nil
}

func (r *KuadrantReconciler) registerServiceMeshMember(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	member := &maistrav1.ServiceMeshMember{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMeshMember",
			APIVersion: maistrav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: kObj.Namespace,
		},
		Spec: maistrav1.ServiceMeshMemberSpec{
			ControlPlaneRef: maistrav1.ServiceMeshControlPlaneRef{
				Name:      controlPlaneProviderName(),
				Namespace: controlPlaneProviderNamespace(),
			},
		},
	}

	err := r.SetOwnerReference(kObj, member)
	if err != nil {
		return err
	}

	return r.ReconcileResource(ctx, &maistrav1.ServiceMeshMember{}, member, reconcilers.CreateOnlyMutator)
}

func (r *KuadrantReconciler) reconcileSpec(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	if err := r.registerExternalAuthorizer(ctx, kObj); err != nil {
		return err
	}

	if err := r.reconcileLimitador(ctx, kObj); err != nil {
		return err
	}

	return r.reconcileAuthorino(ctx, kObj)
}

func (r *KuadrantReconciler) reconcileLimitador(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kObj.Namespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err := r.Client().Get(ctx, limitadorKey, limitador)
	if err != nil {
		if apierrors.IsNotFound(err) {
			limitador = &limitadorv1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Limitador",
					APIVersion: "limitador.kuadrant.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.LimitadorName,
					Namespace: kObj.Namespace,
				},
				Spec: limitadorv1alpha1.LimitadorSpec{
					RateLimitHeaders: &[]limitadorv1alpha1.RateLimitHeadersType{limitadorv1alpha1.RateLimitHeadersTypeDraft03}[0],
					Telemetry:        &[]limitadorv1alpha1.Telemetry{limitadorv1alpha1.TelemetryExhaustive}[0],
				},
			}
		} else {
			return err
		}
	}

	if kObj.Spec.Limitador != nil {
		if kObj.Spec.Limitador.Affinity != nil {
			limitador.Spec.Affinity = kObj.Spec.Limitador.Affinity
		}
		if kObj.Spec.Limitador.PodDisruptionBudget != nil {
			limitador.Spec.PodDisruptionBudget = kObj.Spec.Limitador.PodDisruptionBudget
		}
		if kObj.Spec.Limitador.Replicas != nil {
			limitador.Spec.Replicas = kObj.Spec.Limitador.Replicas
		}
		if kObj.Spec.Limitador.ResourceRequirements != nil {
			limitador.Spec.ResourceRequirements = kObj.Spec.Limitador.ResourceRequirements
		}
		if kObj.Spec.Limitador.Storage != nil {
			limitador.Spec.Storage = kObj.Spec.Limitador.Storage
		}
	}

	err = r.SetOwnerReference(kObj, limitador)
	if err != nil {
		return err
	}

	return r.ReconcileResource(ctx, &limitadorv1alpha1.Limitador{}, limitador, kuadranttools.LimitadorMutator)
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
			ClusterWide:            true,
			SupersedingHostSubsets: true,
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
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Kuadrant controller disabled. GatewayAPI was not found")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Owns(&limitadorv1alpha1.Limitador{}).
		Owns(&authorinov1beta1.Authorino{}).
		Complete(r)
}
