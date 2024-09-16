package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadranttools"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// AuthPolicyEnvoySecurityPolicyReconciler reconciles SecurityPolicy objects for auth
type AuthPolicyEnvoySecurityPolicyReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=securitypolicies,verbs=get;list;watch;create;update;patch;delete

func (r *AuthPolicyEnvoySecurityPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Kuadrant", req.NamespacedName)
	logger.Info("Reconciling auth SecurityPolicy")
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

	topology, err := kuadranttools.TopologyForPolicies(ctx, r.Client(), kuadrantv1beta2.NewAuthPolicyType())
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, policy := range topology.Policies() {
		err := r.reconcileSecurityPolicy(ctx, policy, kObj.Namespace)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *AuthPolicyEnvoySecurityPolicyReconciler) reconcileSecurityPolicy(ctx context.Context, policy kuadrantgatewayapi.PolicyNode, kuadrantNamespace string) error {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("reconcileSecurityPolicy")

	esp := envoySecurityPolicy(policy, kuadrantNamespace)
	if err := r.SetOwnerReference(policy.Policy, esp); err != nil {
		return err
	}

	if err := r.ReconcileResource(ctx, &egv1alpha1.SecurityPolicy{}, esp, kuadrantenvoygateway.EnvoySecurityPolicyMutator); err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "failed to reconcile envoy SecurityPolicy resource")
		return err
	}

	return nil
}

func envoySecurityPolicy(policy kuadrantgatewayapi.PolicyNode, kuadrantNamespace string) *egv1alpha1.SecurityPolicy {
	esp := &egv1alpha1.SecurityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EnvoySecurityPolicyName(policy.GetName()),
			Namespace: policy.GetNamespace(),
			Labels: map[string]string{
				kuadrant.KuadrantNamespaceAnnotation: kuadrantNamespace,
			},
		},
		Spec: egv1alpha1.SecurityPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{},
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

	// if targetref has been deleted, or
	// if gateway target and not programmed, or
	// route target which is not accepted by any parent;
	// tag for deletion
	targetRef := policy.TargetRef()
	if (targetRef == nil || targetRef.GetGatewayNode() != nil && meta.IsStatusConditionFalse(targetRef.GetGatewayNode().Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed))) ||
		(targetRef.GetRouteNode() != nil && !lo.ContainsBy(targetRef.GetRouteNode().Status.Parents, func(p gatewayapiv1.RouteParentStatus) bool {
			return meta.IsStatusConditionTrue(p.Conditions, string(gatewayapiv1.RouteConditionAccepted))
		})) {
		utils.TagObjectToDelete(esp)
		return esp
	}

	targetNetworkObjectGvk := targetRef.GetObject().GetObjectKind().GroupVersionKind()
	esp.Spec.PolicyTargetReferences.TargetRefs = append(esp.Spec.PolicyTargetReferences.TargetRefs,
		gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1.Group(targetNetworkObjectGvk.Group),
				Kind:  gatewayapiv1.Kind(targetNetworkObjectGvk.Kind),
				Name:  gatewayapiv1.ObjectName(targetRef.GetObject().GetName()),
			},
		})

	return esp
}

func EnvoySecurityPolicyName(targetName string) string {
	return fmt.Sprintf("for-%s", targetName)
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

	securityPolicyToKuadrantEventMapper := mappers.NewSecurityPolicyToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("securityPolicyToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)
	policyToKuadrantEventMapper := mappers.NewPolicyToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("policyToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)
	gatewayToKuadrantEventMapper := mappers.NewGatewayToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("gatewayToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)
	httpRouteToKuadrantEventMapper := mappers.NewHTTPRouteToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Watches(
			&egv1alpha1.SecurityPolicy{},
			handler.EnqueueRequestsFromMapFunc(securityPolicyToKuadrantEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.AuthPolicy{},
			handler.EnqueueRequestsFromMapFunc(policyToKuadrantEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayToKuadrantEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToKuadrantEventMapper.Map),
		).
		Complete(r)
}
