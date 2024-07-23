package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadranttools"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// AuthPolicyEnvoySecurityPolicyReconciler reconciles SecurityPolicy objects for auth
type AuthPolicyEnvoySecurityPolicyReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=securitypolicies,verbs=get;list;watch;create;update;patch;delete

func (r *AuthPolicyEnvoySecurityPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
	logger.Info("Reconciling auth SecurityPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client().Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no gateway found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(gw, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if !kuadrant.IsKuadrantManaged(gw) {
		return ctrl.Result{}, nil
	}

	topology, err := kuadranttools.TopologyFromGateway(ctx, r.Client(), gw, &kuadrantv1beta2.AuthPolicy{})
	if err != nil {
		return ctrl.Result{}, err
	}

	kuadrantNamespace, err := kuadrant.GetKuadrantNamespace(gw)
	if err != nil {
		logger.Error(err, "failed to get kuadrant namespace")
		return ctrl.Result{}, err
	}

	// reconcile security policies for gateways
	for _, gwNode := range topology.Gateways() {
		node := gwNode
		err := r.reconcileSecurityPolicy(ctx, &node, kuadrantNamespace)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	// reconcile security policies for routes
	for _, routeNode := range topology.Routes() {
		node := routeNode
		err := r.reconcileSecurityPolicy(ctx, &node, kuadrantNamespace)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *AuthPolicyEnvoySecurityPolicyReconciler) reconcileSecurityPolicy(ctx context.Context, targetable kuadrantgatewayapi.PolicyTargetNode, kuadrantNamespace string) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileSecurityPolicy")

	esp := envoySecurityPolicy(targetable.GetObject(), kuadrantNamespace)
	if len(targetable.AttachedPolicies()) == 0 {
		utils.TagObjectToDelete(esp)
	}

	if err := r.SetOwnerReference(targetable.GetObject(), esp); err != nil {
		return err
	}

	if err := r.ReconcileResource(ctx, &egv1alpha1.SecurityPolicy{}, esp, kuadrantenvoygateway.EnvoySecurityPolicyMutator); err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "failed to reconcile envoy SecurityPolicy resource")
		return err
	}

	return nil
}

func envoySecurityPolicy(targetNetworkObject client.Object, kuadrantNamespace string) *egv1alpha1.SecurityPolicy {
	targetNetworkObjectGvk := targetNetworkObject.GetObjectKind().GroupVersionKind()
	esp := &egv1alpha1.SecurityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EnvoySecurityPolicyName(targetNetworkObject.GetName()),
			Namespace: targetNetworkObject.GetNamespace(),
			Labels: map[string]string{
				kuadrant.KuadrantNamespaceAnnotation: kuadrantNamespace,
			},
		},
		Spec: egv1alpha1.SecurityPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.Group(targetNetworkObjectGvk.Group),
							Kind:  gatewayapiv1.Kind(targetNetworkObjectGvk.Kind),
							Name:  gatewayapiv1.ObjectName(targetNetworkObject.GetName()),
						},
					},
				},
			},
			ExtAuth: &egv1alpha1.ExtAuth{
				GRPC: &egv1alpha1.GRPCExtAuthService{
					BackendRefs: []egv1alpha1.BackendRef{
						{
							BackendObjectReference: gatewayapiv1.BackendObjectReference{
								Name:      kuadrant.AuthorinoServiceName,
								Kind:      ptr.To[gatewayapiv1.Kind]("Service"),
								Namespace: ptr.To(gatewayapiv1.Namespace(kuadrantNamespace)),
								Port:      ptr.To(gatewayapiv1.PortNumber(50051)),
							},
						},
					},
				},
			},
		},
	}
	kuadrant.AnnotateObject(esp, kuadrantNamespace)
	return esp
}

func EnvoySecurityPolicyName(targetName string) string {
	return fmt.Sprintf("on-%s", targetName)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthPolicyEnvoySecurityPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantenvoygateway.IsEnvoyGatewaySecurityPolicyInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway SecurityPolicy controller disabled. EnvoyGateway API was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway SecurityPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)
	apToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("authpolicyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapiv1.Gateway{}).
		Owns(&egv1alpha1.SecurityPolicy{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToParentGatewaysEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.AuthPolicy{},
			handler.EnqueueRequestsFromMapFunc(apToParentGatewaysEventMapper.Map),
		).
		Complete(r)
}
