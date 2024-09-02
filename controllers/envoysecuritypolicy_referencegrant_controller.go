package controllers

import (
	"context"
	"encoding/json"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	KuadrantReferenceGrantName = "kuadrant-authorization-rg"
)

// EnvoySecurityPolicyReferenceGrantReconciler reconciles ReferenceGrant objects for auth
type EnvoySecurityPolicyReferenceGrantReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch;create;update;patch;delete

func (r *EnvoySecurityPolicyReferenceGrantReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Kuadrant", req.NamespacedName)
	logger.Info("Reconciling SecurityPolicy ReferenceGrant")
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

	rg, err := r.securityPolicyReferenceGrant(ctx, kObj.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.SetOwnerReference(kObj, rg); err != nil {
		logger.Error(err, "failed to set owner reference on envoy SecurityPolicy ReferenceGrant resource")
		return ctrl.Result{}, err
	}

	if err := r.ReconcileResource(ctx, &gatewayapiv1beta1.ReferenceGrant{}, rg, kuadrantenvoygateway.SecurityPolicyReferenceGrantMutator); err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "failed to reconcile envoy SecurityPolicy ReferenceGrant resource")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EnvoySecurityPolicyReferenceGrantReconciler) securityPolicyReferenceGrant(ctx context.Context, kuadrantNamespace string) (*gatewayapiv1beta1.ReferenceGrant, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("securityPolicyReferenceGrant")

	rg := &gatewayapiv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KuadrantReferenceGrantName,
			Namespace: kuadrantNamespace,
		},
		Spec: gatewayapiv1beta1.ReferenceGrantSpec{
			To: []gatewayapiv1beta1.ReferenceGrantTo{
				{
					Group: "",
					Kind:  "Service",
					Name:  ptr.To[gatewayapiv1.ObjectName](kuadrant.AuthorinoServiceName),
				},
			},
		},
	}

	espNamespaces := make(map[string]struct{})
	listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(map[string]string{kuadrant.KuadrantNamespaceAnnotation: kuadrantNamespace})}
	espList := &egv1alpha1.SecurityPolicyList{}
	if err := r.Client().List(ctx, espList, listOptions); err != nil {
		return nil, err
	}

	for _, esp := range espList.Items {
		// only append namespaces that differ from the kuadrant namespace and are not marked for deletion
		if esp.DeletionTimestamp == nil && esp.Namespace != kuadrantNamespace {
			espNamespaces[esp.Namespace] = struct{}{}
		}
	}

	if len(espNamespaces) == 0 {
		logger.V(1).Info("no security policies exist outside of the kuadrant namespace, skipping ReferenceGrant")
		utils.TagObjectToDelete(rg)
		return rg, nil
	}

	refGrantFrom := lo.MapToSlice(espNamespaces, func(namespace string, _ struct{}) gatewayapiv1beta1.ReferenceGrantFrom {
		return gatewayapiv1beta1.ReferenceGrantFrom{
			Group:     egv1alpha1.GroupName,
			Kind:      egv1alpha1.KindSecurityPolicy,
			Namespace: gatewayapiv1.Namespace(namespace),
		}
	})
	rg.Spec.From = refGrantFrom

	return rg, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvoySecurityPolicyReferenceGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantenvoygateway.IsEnvoyGatewaySecurityPolicyInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Envoy SecurityPolicy ReferenceGrant controller disabled. EnvoyGateway API was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Envoy SecurityPolicy ReferenceGrant controller disabled. GatewayAPI was not found")
		return nil
	}

	securityPolicyToKuadrantEventMapper := mappers.NewSecurityPolicyToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("securityPolicyToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Owns(&gatewayapiv1beta1.ReferenceGrant{}).
		Watches(
			&egv1alpha1.SecurityPolicy{},
			handler.EnqueueRequestsFromMapFunc(securityPolicyToKuadrantEventMapper.Map),
		).
		Complete(r)
}
