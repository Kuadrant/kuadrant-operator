package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrant "github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/pkg/policymachinery"
)

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoypatchpolicies,verbs=get;list;watch;create;update;patch;delete

// EnvoyGatewayAuthClusterReconciler reconciles Envoy Gateway EnvoyPatchPolicy custom resources for auth
type EnvoyGatewayAuthClusterReconciler struct {
	client *dynamic.DynamicClient
}

// EnvoyGatewayAuthClusterReconciler subscribes to events with potential impact on the Envoy Gateway EnvoyPatchPolicy custom resources for auth
func (r *EnvoyGatewayAuthClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		},
	}
}

func (r *EnvoyGatewayAuthClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayAuthClusterReconciler")

	logger.V(1).Info("building envoy gateway auth clusters")
	defer logger.V(1).Info("finished building envoy gateway auth clusters")

	kuadrant, err := GetKuadrantFromTopology(topology)
	if err != nil {
		if errors.Is(err, ErrMissingKuadrant) {
			logger.V(1).Info(err.Error())
			return nil
		}
		return err
	}

	authorinoObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.AuthorinoGroupKind
	})
	if !found {
		logger.V(1).Info(ErrMissingAuthorino.Error())
		return nil
	}
	authorino := authorinoObj.(*controller.RuntimeObject).Object.(*authorinooperatorv1beta1.Authorino)

	effectivePolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		logger.Error(ErrMissingStateEffectiveAuthPolicies, "failed to get effective auth policies from state")
		return nil
	}

	gateways := lo.UniqBy(lo.FilterMap(lo.Values(effectivePolicies.(EffectiveAuthPolicies)), func(effectivePolicy EffectiveAuthPolicy, _ int) (*machinery.Gateway, bool) {
		gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
		return gateway, gatewayClass.Spec.ControllerName == envoyGatewayGatewayControllerName
	}), func(gateway *machinery.Gateway) string {
		return gateway.GetLocator()
	})

	desiredEnvoyPatchPolicies := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	// reconcile envoy gateway cluster for gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyPatchPolicy, err := r.buildDesiredEnvoyPatchPolicy(authorino, gateway)
		if err != nil {
			logger.Error(err, "failed to build desired envoy patch policy")
			continue
		}
		desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: desiredEnvoyPatchPolicy.GetName(), Namespace: desiredEnvoyPatchPolicy.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(desiredEnvoyPatchPolicy.GetNamespace())

		existingEnvoyPatchPolicyObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind && child.GetName() == desiredEnvoyPatchPolicy.GetName() && child.GetNamespace() == desiredEnvoyPatchPolicy.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyPatchPolicy.GetLabels()))
		})

		// create
		if !found {
			modifiedGateways = append(modifiedGateways, gateway.GetLocator()) // we only signal the gateway as modified when an envoypatchpolicy is created, because updates won't change the status
			desiredEnvoyPatchPolicyUnstructured, err := controller.Destruct(desiredEnvoyPatchPolicy)
			if err != nil {
				logger.Error(err, "failed to destruct envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", desiredEnvoyPatchPolicy)
				continue
			}
			if _, err = resource.Create(ctx, desiredEnvoyPatchPolicyUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", desiredEnvoyPatchPolicyUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingEnvoyPatchPolicy := existingEnvoyPatchPolicyObj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyPatchPolicy)

		if kuadrantenvoygateway.EqualEnvoyPatchPolicies(existingEnvoyPatchPolicy, desiredEnvoyPatchPolicy) {
			logger.V(1).Info("envoypatchpolicy object is up to date, nothing to do")
			continue
		}

		// update
		existingEnvoyPatchPolicy.Spec = envoygatewayv1alpha1.EnvoyPatchPolicySpec{
			TargetRef:   desiredEnvoyPatchPolicy.Spec.TargetRef,
			Type:        desiredEnvoyPatchPolicy.Spec.Type,
			JSONPatches: desiredEnvoyPatchPolicy.Spec.JSONPatches,
			Priority:    desiredEnvoyPatchPolicy.Spec.Priority,
		}

		existingEnvoyPatchPolicyUnstructured, err := controller.Destruct(existingEnvoyPatchPolicy)
		if err != nil {
			logger.Error(err, "failed to destruct envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", existingEnvoyPatchPolicy)
			continue
		}
		if _, err = resource.Update(ctx, existingEnvoyPatchPolicyUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", existingEnvoyPatchPolicyUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateEnvoyGatewayAuthClustersModified, modifiedGateways)

	// cleanup envoy gateway clusters for gateways that are not in the effective policies
	staleEnvoyPatchPolicies := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(AuthObjectLabels()) && !desired
	})

	for _, envoyPatchPolicy := range staleEnvoyPatchPolicies {
		if err := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(envoyPatchPolicy.GetNamespace()).Delete(ctx, envoyPatchPolicy.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoypatchpolicy object", "envoypatchpolicy", fmt.Sprintf("%s/%s", envoyPatchPolicy.GetNamespace(), envoyPatchPolicy.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *EnvoyGatewayAuthClusterReconciler) buildDesiredEnvoyPatchPolicy(authorino *authorinooperatorv1beta1.Authorino, gateway *machinery.Gateway) (*envoygatewayv1alpha1.EnvoyPatchPolicy, error) {
	envoyPatchPolicy := &envoygatewayv1alpha1.EnvoyPatchPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantenvoygateway.EnvoyPatchPolicyGroupKind.Kind,
			APIVersion: envoygatewayv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AuthClusterName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    AuthObjectLabels(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gateway.GroupVersionKind().GroupVersion().String(),
					Kind:               gateway.GroupVersionKind().Kind,
					Name:               gateway.Name,
					UID:                gateway.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: envoygatewayv1alpha1.EnvoyPatchPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.GatewayGroupKind.Kind),
				Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
			},
			Type: envoygatewayv1alpha1.JSONPatchEnvoyPatchType,
		},
	}

	authorinoServiceInfo := authorinoServiceInfoFromAuthorino(authorino)
	jsonPatches, err := kuadrantenvoygateway.BuildEnvoyPatchPolicyClusterPatch(kuadrant.KuadrantAuthClusterName, authorinoServiceInfo.Host, int(authorinoServiceInfo.Port), authClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyPatchPolicy.Spec.JSONPatches = jsonPatches

	return envoyPatchPolicy, nil
}
