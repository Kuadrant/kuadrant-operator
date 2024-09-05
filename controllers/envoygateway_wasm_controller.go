package controllers

import (
	"context"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// EnvoyGatewayWasmReconciler reconciles an EnvoyGateway EnvoyExtensionPolicy object for the kuadrant's wasm module
type EnvoyGatewayWasmReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoyextensionpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *EnvoyGatewayWasmReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("kuadrant", req.NamespacedName)
	logger.V(1).Info("Reconciling envoygateway wasm attachment")
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

	rawTopology, err := kuadranttools.TopologyForPolicies(ctx, r.Client(), kuadrantv1beta2.NewRateLimitPolicyType())
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, gw := range rawTopology.Gateways() {
		topology, err := rlptools.ApplyOverrides(rawTopology, gw.GetGateway())
		if err != nil {
			return ctrl.Result{}, err
		}
		envoyPolicy, err := r.desiredEnvoyExtensionPolicy(ctx, gw, kObj, topology)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ReconcileResource(ctx, &egv1alpha1.EnvoyExtensionPolicy{}, envoyPolicy, kuadrantenvoygateway.EnvoyExtensionPolicyMutator)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.V(1).Info("Envoygateway wasm attachment reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *EnvoyGatewayWasmReconciler) desiredEnvoyExtensionPolicy(
	ctx context.Context, gw kuadrantgatewayapi.GatewayNode,
	kObj *kuadrantv1beta1.Kuadrant,
	topology *kuadrantgatewayapi.Topology) (*egv1alpha1.EnvoyExtensionPolicy, error) {

	baseLogger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	envoyPolicy := &egv1alpha1.EnvoyExtensionPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       egv1alpha1.KindEnvoyExtensionPolicy,
			APIVersion: egv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      EnvoyExtensionPolicyName(gw.GetName()),
			Namespace: gw.GetNamespace(),
		},
		Spec: egv1alpha1.EnvoyExtensionPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1.Group(gatewayapiv1.GroupVersion.Group),
							Kind:  gatewayapiv1.Kind("Gateway"),
							Name:  gatewayapiv1.ObjectName(gw.GetName()),
						},
					},
				},
			},
			Wasm: []egv1alpha1.Wasm{
				{
					Name:   ptr.To("kuadrant-wasm-shim"),
					RootID: ptr.To("kuadrant_wasm_shim"),
					Code: egv1alpha1.WasmCodeSource{
						Type: egv1alpha1.ImageWasmCodeSourceType,
						Image: &egv1alpha1.ImageWasmCodeSource{
							URL: WASMFilterImageURL,
						},
					},
					Config: nil,
					// When a fatal error accurs during the initialization or the execution of the
					// Wasm extension, if FailOpen is set to false the system blocks the traffic and returns
					// an HTTP 5xx error.
					FailOpen: ptr.To(false),
				},
			},
		},
	}

	logger := baseLogger.WithValues("envoyextensionpolicy", client.ObjectKeyFromObject(envoyPolicy))

	config, err := wasm.WasmConfigForGateway(ctx, gw.GetGateway(), topology)
	if err != nil {
		return nil, err
	}

	if config == nil || len(config.RateLimitPolicies) == 0 {
		logger.V(1).Info("config is empty. EnvoyExtensionPolicy will be deleted if it exists")
		utils.TagObjectToDelete(envoyPolicy)
		return envoyPolicy, nil
	}

	configJSON, err := config.ToJSON()
	if err != nil {
		return nil, err
	}

	envoyPolicy.Spec.Wasm[0].Config = configJSON

	kuadrant.AnnotateObject(envoyPolicy, kObj.GetNamespace())

	// controller reference
	if err := r.SetOwnerReference(gw.GetGateway(), envoyPolicy); err != nil {
		return nil, err
	}

	return envoyPolicy, nil
}

func EnvoyExtensionPolicyName(targetName string) string {
	return fmt.Sprintf("kuadrant-wasm-for-%s", targetName)
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvoyGatewayWasmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantenvoygateway.IsEnvoyGatewayInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway Wasm controller disabled. EnvoyGateway API was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway Wasm controller disabled. GatewayAPI was not found")
		return nil
	}

	kuadrantListEventMapper := mappers.NewKuadrantListEventMapper(
		mappers.WithLogger(r.Logger().WithName("envoyExtensionPolicyToKuadrantEventMapper")),
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
			&egv1alpha1.EnvoyExtensionPolicy{},
			handler.EnqueueRequestsFromMapFunc(kuadrantListEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
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
