package controllers

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

type AuthorinoIstioIntegrationReconciler struct {
	*reconcilers.BaseReconciler

	Client *dynamic.DynamicClient
}

func NewAuthorinoIstioIntegrationReconciler(mgr controllerruntime.Manager, client *dynamic.DynamicClient) *AuthorinoIstioIntegrationReconciler {
	return &AuthorinoIstioIntegrationReconciler{
		Client: client,
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
	}
}

func (a *AuthorinoIstioIntegrationReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: a.Run, Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
			{Kind: ptr.To(kuadrantv1beta1.AuthorinoGroupKind)},
			// Effective policies impact on the istio integration
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
		},
	}
}

func (a *AuthorinoIstioIntegrationReconciler) Run(baseCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(baseCtx).WithName("AuthorinoIstioIntegrationReconciler")
	ctx := logr.NewContext(baseCtx, logger)
	logger.V(1).Info("reconciling authorino integration in istio", "status", "started")
	defer logger.V(1).Info("reconciling authorino integration in istio", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)

	if kObj == nil {
		// Nothing to be done. It is expected that the authorino resource managed by kuadrant
		// to be removed as well
		return nil
	}

	effectiveAuthPolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		return ErrMissingStateEffectiveAuthPolicies
	}
	effectiveAuthPoliciesMap := effectiveAuthPolicies.(EffectiveAuthPolicies)

	logger.V(1).Info("effective policies info", "effectiveAuthPolicies", len(effectiveAuthPoliciesMap))

	// Authorino deployment cannot be added to the topology without
	// adding all the cluster deployments to the topology because it does not have any label
	// Thus, deployment needs to be read from the cluster by name

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			// the name of the deployment is hardcoded. This deployment is owned by the authorino operator.
			// label propagation pattern would be more reliable as the kuadrant operator would be owning these labels
			// kuadrant would add/remove sidecar label to the Authorino object which owns.
			Name: "authorino",
			// it is safe for now to assume that Authorino will exist in the namespace of Kuadrant
			Namespace: kObj.GetNamespace(),
		},
	}

	err := a.GetResource(ctx, types.NamespacedName{Namespace: deployment.Namespace, Name: deployment.Name}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// when deployment is ready, authorino resource will be updated triggering another event for this reconciler
			logger.V(1).Info("authorino deployment not found", "key", client.ObjectKeyFromObject(deployment))
			return nil
		}
		return fmt.Errorf("could not get authorino deployment %w", err)
	}

	// Only enable sidecar when enabled in kuadrant CR AND effective policies in place
	allowMTLS := kObj.IsMTLSAuthorinoEnabled() && len(effectiveAuthPoliciesMap) > 0

	// add "sidecar.istio.io/inject" label to authorino deployment.
	// label value depends on whether MTLS is enabled or not
	updated := utils.MergeMapStringString(
		&deployment.Spec.Template.Labels,
		map[string]string{
			"sidecar.istio.io/inject": strconv.FormatBool(allowMTLS),
			kuadrantManagedLabelKey:   "true",
		},
	)

	if updated {
		if err := a.UpdateResource(ctx, deployment); err != nil {
			return err
		}
	}

	return nil
}
