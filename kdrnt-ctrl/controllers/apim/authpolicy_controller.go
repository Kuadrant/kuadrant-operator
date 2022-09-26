package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	secv1beta1types "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

// AuthPolicyReconciler reconciles a AuthPolicy object
type AuthPolicyReconciler struct {
	reconcilers.TargetRefReconciler
}

const authPolicyFinalizer = "authpolicy.kuadrant.io/finalizer"

var AuthProvider = common.FetchEnv("AUTH_PROVIDER", "kuadrant-authorization")

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=authpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=authpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=authpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;patch;delete

func (r *AuthPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("AuthPolicy", req.NamespacedName)
	logger.Info("Reconciling AuthPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	var ap apimv1alpha1.AuthPolicy
	if err := r.Client().Get(ctx, req.NamespacedName, &ap); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no AuthPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get AuthPolicy")
		return ctrl.Result{}, err
	}

	if ap.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(&ap, authPolicyFinalizer) {
		logger.V(1).Info("Handling removal of authpolicy object")
		if err := r.removeIstioAuthPolicy(ctx, &ap); err != nil {
			logger.Error(err, "failed to remove Istio's AuthorizationPolicy")
			return ctrl.Result{}, err
		}

		if err := r.removeAuthSchemes(ctx, &ap); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.deleteNetworkResourceBackReference(ctx, &ap); err != nil {
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(&ap, authPolicyFinalizer)
		if err := r.UpdateResource(ctx, &ap); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if ap.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&ap, authPolicyFinalizer) {
		controllerutil.AddFinalizer(&ap, authPolicyFinalizer)
		if err := r.UpdateResource(ctx, &ap); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	specResult, specErr := r.reconcileSpec(ctx, &ap)
	if specErr == nil && specResult.Requeue {
		logger.V(1).Info("Reconciling spec not finished. Requeueing.")
		return specResult, nil
	}

	statusResult, statusErr := r.reconcileStatus(ctx, &ap, specErr)

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

	logger.Info("successfully reconciling AuthPolicy")
	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) reconcileSpec(ctx context.Context, ap *apimv1alpha1.AuthPolicy) (ctrl.Result, error) {
	if err := ap.Validate(); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.enforceHierarchicalConstraints(ctx, ap); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNetworkResourceBackReference(ctx, ap); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAuthRules(ctx, ap); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAuthSchemes(ctx, ap); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) enforceHierarchicalConstraints(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	targetHostnames, err := r.TargetHostnames(ctx, ap.Spec.TargetRef, ap.Namespace)
	if err != nil {
		return err
	}

	ruleHosts := make([]string, 0)
	for _, rule := range ap.Spec.AuthRules {
		ruleHosts = append(ruleHosts, rule.Hosts...)
	}

	if valid, invalidHost := common.ValidSubdomains(targetHostnames, ruleHosts); !valid {
		return fmt.Errorf("rule host (%s) does not follow any hierarchical constraints", invalidHost)
	}

	if valid, invalidHost := common.ValidSubdomains(targetHostnames, ap.Spec.AuthScheme.Hosts); !valid {
		return fmt.Errorf("host defined in authscheme (%s) does not follow any hierarchical constraints", invalidHost)
	}

	return nil
}

func (r *AuthPolicyReconciler) reconcileAuthSchemes(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger, _ := logr.FromContext(ctx)

	apKey := client.ObjectKeyFromObject(ap)
	authConfig := &authorinov1beta1.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(apKey),
			Namespace: common.KuadrantNamespace,
		},
		Spec: *ap.Spec.AuthScheme,
	}
	err := r.ReconcileResource(ctx, &authorinov1beta1.AuthConfig{}, authConfig, alwaysUpdateAuthConfig)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logger.Error(err, "ReconcileResource failed to create/update AuthConfig resource")
		return err
	}

	return nil
}

// reconcileAuthRules translates and reconciles `AuthRules` into an Istio AuthorizationPoilcy containing them.
func (r *AuthPolicyReconciler) reconcileAuthRules(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger, _ := logr.FromContext(ctx)

	toRules := []*secv1beta1types.Rule_To{}
	for _, rule := range ap.Spec.AuthRules {
		toRules = append(toRules, &secv1beta1types.Rule_To{
			Operation: &secv1beta1types.Operation{
				Hosts:   rule.Hosts,
				Methods: rule.Methods,
				Paths:   rule.Paths,
			},
		})
	}

	if len(toRules) == 0 {
		targetObj, err := r.FetchValidTargetRef(ctx, ap.Spec.TargetRef, ap.Namespace)
		if err != nil {
			return err
		}
		switch route := targetObj.(type) {
		case *gatewayapiv1alpha2.HTTPRoute:
			// rules not set and targeting a HTTPRoute
			// Compile rules from the route
			httpRouterules := common.RulesFromHTTPRoute(route)
			for idx := range httpRouterules {
				var tmp []string
				toRules = append(toRules, &secv1beta1types.Rule_To{
					Operation: &secv1beta1types.Operation{
						// copy slice
						Hosts:   append(tmp, httpRouterules[idx].Hosts...),
						Methods: append(tmp, httpRouterules[idx].Methods...),
						Paths:   append(tmp, httpRouterules[idx].Paths...),
					},
				})
			}
		}
	}

	gwKeys, err := r.TargetedGatewayKeys(ctx, ap.Spec.TargetRef, ap.Namespace)
	if err != nil {
		return nil
	}

	targetObjectKind := "Gateway"
	if common.IsTargetRefHTTPRoute(ap.Spec.TargetRef) {
		targetObjectKind = "HTTPRoute"
	}

	for _, gwKey := range gwKeys {
		authPolicy := secv1beta1resources.AuthorizationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getAuthPolicyName(gwKey.Name, string(ap.Spec.TargetRef.Name), targetObjectKind),
				Namespace: gwKey.Namespace,
			},
			Spec: secv1beta1types.AuthorizationPolicy{
				Action: secv1beta1types.AuthorizationPolicy_CUSTOM,
				Rules: []*secv1beta1types.Rule{
					{
						To: toRules,
					},
				},
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{}, // TODO(rahulanand16nov): fetch from gateway
				},
				ActionDetail: &secv1beta1types.AuthorizationPolicy_Provider{
					Provider: &secv1beta1types.AuthorizationPolicy_ExtensionProvider{
						Name: AuthProvider,
					},
				},
			},
		}

		err := r.ReconcileResource(ctx, &secv1beta1resources.AuthorizationPolicy{}, &authPolicy, alwaysUpdateAuthPolicy)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "ReconcileResource failed to create/update AuthorizationPolicy resource")
			return err
		}
	}

	return nil
}

func (r *AuthPolicyReconciler) reconcileNetworkResourceBackReference(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	return r.ReconcileTargetBackReference(ctx, client.ObjectKeyFromObject(ap), ap.Spec.TargetRef,
		ap.Namespace, common.AuthPolicyBackRefAnnotation)
}

func (r *AuthPolicyReconciler) removeAuthSchemes(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("Removing Authorino's AuthConfigs")

	apKey := client.ObjectKeyFromObject(ap)
	authConfig := &authorinov1beta1.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authConfigName(apKey),
			Namespace: common.KuadrantNamespace,
		},
	}

	err := r.DeleteResource(ctx, authConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		logger.Error(err, "failed to delete Authorino's AuthConfig")
		return err
	}
	return nil
}

func (r *AuthPolicyReconciler) removeIstioAuthPolicy(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger, _ := logr.FromContext(ctx)
	logger.Info("Removing Istio's AuthorizationPolicy")

	gwKeys, err := r.TargetedGatewayKeys(ctx, ap.Spec.TargetRef, ap.Namespace)
	if err != nil {
		return nil
	}

	targetObjectKind := "Gateway"
	if common.IsTargetRefHTTPRoute(ap.Spec.TargetRef) {
		targetObjectKind = "HTTPRoute"
	}

	for _, gwKey := range gwKeys {
		istioAuthPolicy := &secv1beta1resources.AuthorizationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getAuthPolicyName(gwKey.Name, string(ap.Spec.TargetRef.Name), targetObjectKind),
				Namespace: gwKey.Namespace,
			},
		}

		if err := r.DeleteResource(ctx, istioAuthPolicy); err != nil {
			logger.Error(err, "failed to delete Istio's AuthorizationPolicy")
			return err
		}
	}

	logger.Info("removed Istio's AuthorizationPolicy")
	return nil
}

func (r *AuthPolicyReconciler) deleteNetworkResourceBackReference(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	return r.DeleteTargetBackReference(ctx, client.ObjectKeyFromObject(ap), ap.Spec.TargetRef,
		ap.Namespace, common.AuthPolicyBackRefAnnotation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	HTTPRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteHandler"),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.AuthPolicy{}).
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.HTTPRoute{}},
			handler.EnqueueRequestsFromMapFunc(HTTPRouteEventMapper.MapToAuthPolicy),
		).
		Watches(&source.Kind{Type: &gatewayapiv1alpha2.Gateway{}},
			handler.EnqueueRequestsFromMapFunc(HTTPRouteEventMapper.MapToAuthPolicy)).
		Complete(r)
}
