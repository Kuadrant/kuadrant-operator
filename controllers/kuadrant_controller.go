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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/env"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	maistrav1 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v1"
	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"golang.org/x/sync/errgroup"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const (
	kuadrantFinalizer = "kuadrant.io/finalizer"
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
//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=gateways,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=httproutes,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=operator.authorino.kuadrant.io,resources=authorinos,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=install.istio.io,resources=istiooperators,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshcontrolplanes,verbs=get;list;watch;update;use;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshmembers,verbs=get;list;watch;create;update;delete;patch

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
	logger, _ := logr.FromContext(ctx)

	isIstioInstalled, err := r.unregisterExternalAuthorizerIstio(ctx, kObj)

	if err == nil && !isIstioInstalled {
		err = r.unregisterExternalAuthorizerOSSM(ctx, kObj)
	}

	if err != nil {
		logger.Error(err, "failed fo get service mesh control plane")
	}

	return err
}

func (r *KuadrantReconciler) unregisterExternalAuthorizerIstio(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := r.getIstioConfigObjects(ctx, logger)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := common.NewKuadrantAuthorizer(kObj.Namespace)

	for _, config := range configsToUpdate {
		hasKuadrantAuthorizer, err := common.HasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if hasKuadrantAuthorizer {
			if err = common.UnregisterKuadrantAuthorizer(config, kuadrantAuthorizer); err != nil {
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
		return err
	}

	smcpWrapper := istio.NewOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := common.NewKuadrantAuthorizer(kObj.Namespace)

	hasKuadrantAuthorizer, err := common.HasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if hasKuadrantAuthorizer {
		err = common.UnregisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
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
	logger, _ := logr.FromContext(ctx)

	isIstioInstalled, err := r.registerExternalAuthorizerIstio(ctx, kObj)

	if err == nil && !isIstioInstalled {
		err = r.registerExternalAuthorizerOSSM(ctx, kObj)
	}

	if err != nil {
		logger.Error(err, "failed fo get service mesh control plane")
	}

	return err
}

func (r *KuadrantReconciler) registerExternalAuthorizerIstio(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) (bool, error) {
	logger, _ := logr.FromContext(ctx)
	configsToUpdate, err := r.getIstioConfigObjects(ctx, logger)
	isIstioInstalled := configsToUpdate != nil

	if !isIstioInstalled || err != nil {
		return isIstioInstalled, err
	}

	kuadrantAuthorizer := common.NewKuadrantAuthorizer(kObj.Namespace)
	for _, config := range configsToUpdate {
		hasKuadrantAuthorizer, err := common.HasKuadrantAuthorizer(config, *kuadrantAuthorizer)
		if err != nil {
			return true, err
		}
		if !hasKuadrantAuthorizer {
			err = common.RegisterKuadrantAuthorizer(config, kuadrantAuthorizer)
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

	if err := r.registerServiceMeshMember(ctx, kObj); err != nil {
		return err
	}

	smcp := &maistrav2.ServiceMeshControlPlane{}

	smcpKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.GetResource(ctx, smcpKey, smcp); err != nil {
		logger.V(1).Info("failed to get servicemeshcontrolplane object", "key", smcp, "err", err)
		return err
	}
	smcpWrapper := istio.NewOSSMControlPlaneWrapper(smcp)
	kuadrantAuthorizer := common.NewKuadrantAuthorizer(kObj.Namespace)

	hasKuadrantAuthorizer, err := common.HasKuadrantAuthorizer(smcpWrapper, *kuadrantAuthorizer)
	if err != nil {
		return err
	}
	if !hasKuadrantAuthorizer {
		err = common.RegisterKuadrantAuthorizer(smcpWrapper, kuadrantAuthorizer)
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

func (r *KuadrantReconciler) getIstioConfigObjects(ctx context.Context, logger logr.Logger) ([]common.ConfigWrapper, error) {
	var configsToUpdate []common.ConfigWrapper

	iop := &iopv1alpha1.IstioOperator{}
	iopKey := client.ObjectKey{Name: controlPlaneProviderName(), Namespace: controlPlaneProviderNamespace()}
	if err := r.GetResource(ctx, iopKey, iop); err != nil {
		logger.V(1).Info("failed to get istiooperator object", "key", iopKey, "err", err)
		if apimeta.IsNoMatchError(err) {
			// return nil and nil if there's no istiooperator CRD, means istio is not installed
			return nil, nil
		} else if !apierrors.IsNotFound(err) {
			// return nil and err if there's an error other than not found (no istiooperator CR)
			return nil, err
		}
	} else {
		configsToUpdate = append(configsToUpdate, istio.NewOperatorWrapper(iop))
	}

	istioConfigMap := &corev1.ConfigMap{}
	if err := r.GetResource(ctx, client.ObjectKey{Name: controlPlaneConfigMapName(), Namespace: controlPlaneProviderNamespace()}, istioConfigMap); err != nil {
		logger.V(1).Info("failed to get istio configMap", "key", iopKey, "err", err)
		return configsToUpdate, err
	}
	configsToUpdate = append(configsToUpdate, istio.NewConfigMapWrapper(istioConfigMap))
	return configsToUpdate, nil
}

func (r *KuadrantReconciler) registerServiceMeshMember(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	member := buildServiceMeshMember(kObj)
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

func controlPlaneProviderName() string {
	return env.GetString("ISTIOOPERATOR_NAME", "istiocontrolplane")
}

func controlPlaneConfigMapName() string {
	return env.GetString("ISTIOCONFIGMAP_NAME", "istio")
}

func controlPlaneProviderNamespace() string {
	return env.GetString("ISTIOOPERATOR_NAMESPACE", "istio-system")
}

func buildServiceMeshMember(kObj *kuadrantv1beta1.Kuadrant) *maistrav1.ServiceMeshMember {
	return &maistrav1.ServiceMeshMember{
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
}

func (r *KuadrantReconciler) reconcileClusterGateways(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	// TODO: After the RFC defined, we might want to get the gw to label/annotate from Kuadrant.Spec or manual labeling/annotation
	gwList := &gatewayapiv1.GatewayList{}
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

	return errGroup.Wait()
}

func (r *KuadrantReconciler) removeAnnotationFromGateways(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	gwList := &gatewayapiv1.GatewayList{}
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

	return errGroup.Wait()
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Owns(&appsv1.Deployment{}).
		Owns(&limitadorv1alpha1.Limitador{}).
		Owns(&authorinov1beta1.Authorino{}).
		Complete(r)
}
