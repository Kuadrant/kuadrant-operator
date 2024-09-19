package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

// EnvoyGatewayLimitadorClusterReconciler reconciles an EnvoyGateway EnvoyPatchPolicy object
// to setup limitador's cluster on the gateway. It is a requirement for the wasm module to work.
// https://gateway.envoyproxy.io/latest/api/extension_types/#envoypatchpolicy
type EnvoyGatewayLimitadorClusterReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoypatchpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoyextensionpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *EnvoyGatewayLimitadorClusterReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("envoyExtensionPolicy", req.NamespacedName)
	logger.V(1).Info("Reconciling limitador cluster")
	ctx := logr.NewContext(eventCtx, logger)

	extPolicy := &egv1alpha1.EnvoyExtensionPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, extPolicy); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no envoygateway extension policy object found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get envoygateway extension policy object")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(extPolicy.Spec.PolicyTargetReferences, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if extPolicy.DeletionTimestamp != nil {
		// no need to handle deletion
		// ownerrefs will do the job
		return ctrl.Result{}, nil
	}

	//
	// Get kuadrant
	//
	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	err := r.Client().List(ctx, kuadrantList)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(kuadrantList.Items) == 0 {
		logger.Info("kuadrant object not found. Nothing to do")
		return ctrl.Result{}, nil
	}

	kObj := kuadrantList.Items[0]

	//
	// Get limitador
	//
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kObj.Namespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("read limitador", "key", limitadorKey, "err", err)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("limitador object not found. Nothing to do")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
		logger.Info("limitador status reports not ready. Retrying")
		return ctrl.Result{Requeue: true}, nil
	}

	limitadorClusterPatchPolicy, err := r.desiredLimitadorClusterPatchPolicy(extPolicy, limitador)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.ReconcileResource(ctx, &egv1alpha1.EnvoyPatchPolicy{}, limitadorClusterPatchPolicy, reconcilers.CreateOnlyMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Envoygateway limitador cluster reconciled successfully")

	return ctrl.Result{}, nil
}

func (r *EnvoyGatewayLimitadorClusterReconciler) desiredLimitadorClusterPatchPolicy(
	extPolicy *egv1alpha1.EnvoyExtensionPolicy,
	limitador *limitadorv1alpha1.Limitador) (*egv1alpha1.EnvoyPatchPolicy, error) {
	patchPolicy := &egv1alpha1.EnvoyPatchPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       egv1alpha1.KindEnvoyPatchPolicy,
			APIVersion: egv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      LimitadorClusterEnvoyPatchPolicyName(extPolicy.GetName()),
			Namespace: extPolicy.Namespace,
		},
		Spec: egv1alpha1.EnvoyPatchPolicySpec{
			// Same target ref as the associated extension policy
			TargetRef: extPolicy.Spec.PolicyTargetReferences.TargetRefs[0].LocalPolicyTargetReference,
			Type:      egv1alpha1.JSONPatchEnvoyPatchType,
			JSONPatches: []egv1alpha1.EnvoyJSONPatchConfig{
				limitadorClusterPatch(
					limitador.Status.Service.Host,
					int(limitador.Status.Service.Ports.GRPC),
				),
			},
		},
	}

	// controller reference
	// patchPolicy has ownerref to extension policy
	if err := r.SetOwnerReference(extPolicy, patchPolicy); err != nil {
		return nil, err
	}

	return patchPolicy, nil
}

func LimitadorClusterEnvoyPatchPolicyName(targetName string) string {
	return fmt.Sprintf("patch-for-%s", targetName)
}

func limitadorClusterPatch(limitadorSvcHost string, limitadorGRPCPort int) egv1alpha1.EnvoyJSONPatchConfig {
	// The patch defines the rate_limit_cluster, which provides the endpoint location of the external rate limit service.
	// TODO(eguzki): Istio EnvoyFilter uses almost the same structure. DRY
	patchUnstructured := map[string]any{
		"name":                   common.KuadrantRateLimitClusterName,
		"type":                   "STRICT_DNS",
		"connect_timeout":        "1s",
		"lb_policy":              "ROUND_ROBIN",
		"http2_protocol_options": map[string]any{},
		"load_assignment": map[string]any{
			"cluster_name": common.KuadrantRateLimitClusterName,
			"endpoints": []map[string]any{
				{
					"lb_endpoints": []map[string]any{
						{
							"endpoint": map[string]any{
								"address": map[string]any{
									"socket_address": map[string]any{
										"address":    limitadorSvcHost,
										"port_value": limitadorGRPCPort,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	value := &apiextensionsv1.JSON{}
	value.UnmarshalJSON(patchRaw)

	return egv1alpha1.EnvoyJSONPatchConfig{
		Type: egv1alpha1.ClusterEnvoyResourceType,
		Name: common.KuadrantRateLimitClusterName,
		Operation: egv1alpha1.JSONPatchOperation{
			Op:    egv1alpha1.JSONPatchOperationType("add"),
			Path:  "",
			Value: value,
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvoyGatewayLimitadorClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantenvoygateway.IsEnvoyGatewayInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway limitador cluster controller disabled. EnvoyGateway API was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway limitador cluster disabled. GatewayAPI was not found")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&egv1alpha1.EnvoyExtensionPolicy{}).
		Owns(&egv1alpha1.EnvoyPatchPolicy{}).
		Complete(r)
}
