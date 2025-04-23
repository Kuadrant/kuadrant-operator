package controllers

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

type LimitadorIstioIntegrationReconciler struct {
	*reconcilers.BaseReconciler

	Client *dynamic.DynamicClient
}

func NewLimitadorIstioIntegrationReconciler(mgr controllerruntime.Manager, client *dynamic.DynamicClient) *LimitadorIstioIntegrationReconciler {
	return &LimitadorIstioIntegrationReconciler{
		Client: client,
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
	}
}

func (l *LimitadorIstioIntegrationReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: l.Run, Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
			{Kind: ptr.To(kuadrantv1beta1.LimitadorGroupKind)},
			// Effective policies impact on the istio integration
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
		},
	}
}

func (l *LimitadorIstioIntegrationReconciler) Run(baseCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(baseCtx).WithName("LimitadorIstioIntegrationReconciler")
	ctx := logr.NewContext(baseCtx, logger)
	logger.V(1).Info("reconciling limitador integration in istio", "status", "started")
	defer logger.V(1).Info("reconciling limitador integration in istio", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)

	if kObj == nil {
		// Nothing to be done. It is expected the limitador resource managed by kuadrant
		// to be removed as well
		return nil
	}

	effectiveRateLimitPolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return ErrMissingStateEffectiveRateLimitPolicies
	}
	effectiveRateLimitPoliciesMap := effectiveRateLimitPolicies.(EffectiveRateLimitPolicies)

	logger.V(1).Info("effective rate limit policies info", "effectiveRateLimitPolicies", len(effectiveRateLimitPoliciesMap))

	// read limitador objects that are children of kuadrant instead of fetching the list all limitador objects of the cluster
	limitadorObjs := utils.Filter(topology.All().Children(kObj), func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})

	if len(limitadorObjs) == 0 {
		// Nothing to be done.
		// when limitador is ready, a new event will be triggered for this reconciler
		return nil
	}

	// Currently only one instance of limitador is supported as a child of kuadrant
	limitador := limitadorObjs[0]

	// read deployment objects that are children of limitador
	deploymentObjs := lo.FilterMap(topology.All().Children(limitador), func(child machinery.Object, _ int) (*appsv1.Deployment, bool) {
		if child.GroupVersionKind().GroupKind() != kuadrantv1beta1.DeploymentGroupKind {
			return nil, false
		}

		runtimeObj, ok := child.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}

		// cannot do "return runtimeObj.Object.(*appsv1.Deployment)" as strict mode is used and does not match main method signature.
		deployment, ok := runtimeObj.Object.(*appsv1.Deployment)
		return deployment, ok
	})

	if len(deploymentObjs) == 0 {
		// Nothing to be done.
		// when limitador is ready, a new event will be triggered for this reconciler
		return nil
	}

	// Currently only one instance of deployment is supported as a child of limitaodor
	// Needs to be deep copied to avoid race conditions with the object living in the topology
	deployment := deploymentObjs[0].DeepCopy()

	err := l.GetResource(ctx, types.NamespacedName{Namespace: deployment.Namespace, Name: deployment.Name}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// when deployment is ready, limitador resource will be updated triggering another event for this reconciler
			logger.V(1).Info("limitador deployment not found", "key", client.ObjectKeyFromObject(deployment))
			return nil
		}
		return fmt.Errorf("could not get limitador deployment %w", err)
	}

	// Only enable sidecar when enabled in kuadrant CR AND effective policies in place
	allowMTLS := kObj.IsMTLSLimitadorEnabled() && len(effectiveRateLimitPoliciesMap) > 0

	// add "sidecar.istio.io/inject" label to limitador deployment.
	// label value depends on whether MTLS is enabled or not
	updated := utils.MergeMapStringString(
		&deployment.Spec.Template.Labels,
		map[string]string{
			"sidecar.istio.io/inject": strconv.FormatBool(allowMTLS),
			kuadrantManagedLabelKey:   "true",
		},
	)

	if updated {
		if err := l.UpdateResource(ctx, deployment); err != nil {
			return err
		}
	}

	return nil
}
