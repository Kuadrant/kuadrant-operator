package apim

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	securityv1beta1 "istio.io/api/security/v1beta1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/log"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	KuadrantAuthProviderAnnotation = "kuadrant.io/auth-provider"
)

//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete

// VirtualServiceReconciler reconciles Istio's AuthorizationPolicy object
type VirtualServiceReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

func (r *VirtualServiceReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("VirtualService", req.NamespacedName)
	ctx := logr.NewContext(eventCtx, logger)

	virtualService := istionetworkingv1alpha3.VirtualService{}
	if err := r.Client().Get(ctx, req.NamespacedName, &virtualService); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get VirtualService")
		return ctrl.Result{}, err
	}

	// check if this virtualservice is to be protected or not.
	_, present := virtualService.GetAnnotations()[KuadrantAuthProviderAnnotation]
	if !present {
		for _, gateway := range virtualService.Spec.Gateways {
			gwKey := common.NamespacedNameToObjectKey(gateway, virtualService.Namespace)
			authObjKey := types.NamespacedName{
				Name:      getAuthPolicyName(gwKey.Name, virtualService.Name),
				Namespace: gwKey.Namespace,
			}

			authPolicy := istiosecurityv1beta1.AuthorizationPolicy{}
			if err := r.Client().Get(context.Background(), authObjKey, &authPolicy); err != nil {
				// no annotation but authpolicy exist means annotation was removed.
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "failed to check AuthorizationPolicy existence")
				}
				return ctrl.Result{}, nil // this virtualservice is not protected.
			}

			// Orphan AuthorizationPolicy exists
			if err := r.Client().Delete(context.Background(), &authPolicy); err != nil {
				logger.Error(err, "failed to delete orphan authorizationpolicy")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// reconcile authpolicy for the protected virtualservice
	if err := r.reconcileAuthPolicy(ctx, logger, &virtualService); err != nil {
		logger.Error(err, "failed to reconcile AuthorizationPolicy")
		return ctrl.Result{}, err
	}
	logger.Info("successfully reconciled AuthorizationPolicy")

	return ctrl.Result{}, nil
}

func (r *VirtualServiceReconciler) reconcileAuthPolicy(ctx context.Context, logger logr.Logger, vs *istionetworkingv1alpha3.VirtualService) error {
	logger.Info("Reconciling AuthorizationPolicy")

	// annotation presence is already checked.
	providerName := vs.GetAnnotations()[KuadrantAuthProviderAnnotation]

	// fill out the rules
	authToRules := []*securityv1beta1.Rule_To{}
	for _, httpRoute := range vs.Spec.Http {
		for idx, matchRequest := range httpRoute.Match {
			toRule := &securityv1beta1.Rule_To{
				Operation: &securityv1beta1.Operation{},
			}

			toRule.Operation.Hosts = vs.Spec.Hosts
			if normalizedURI := normalizeStringMatch(matchRequest.Uri); normalizedURI != "" {
				toRule.Operation.Paths = append(toRule.Operation.Paths, normalizedURI)
			}

			if normalizedMethod := normalizeStringMatch(matchRequest.Method); normalizedMethod != "" {
				// Looks like it's case-sensitive:
				// https://istio.io/latest/docs/reference/config/security/normalization/#1-method-not-in-upper-case
				method := strings.ToUpper(normalizedMethod)
				toRule.Operation.Methods = append(toRule.Operation.Methods, method)
			}

			// If there is only regex stringmatches then we'll have bunch of repeated To rules with
			// only same host filled into each. Following make sure only one field like that is present.
			operation := toRule.Operation
			if len(operation.Paths) == 0 && len(operation.Methods) == 0 && idx > 0 {
				continue
			}
			authToRules = append(authToRules, toRule)
		}
	}

	authPolicySpec := securityv1beta1.AuthorizationPolicy{
		Rules: []*securityv1beta1.Rule{{
			To: authToRules,
		}},
		Action: securityv1beta1.AuthorizationPolicy_CUSTOM,
		ActionDetail: &securityv1beta1.AuthorizationPolicy_Provider{
			Provider: &securityv1beta1.AuthorizationPolicy_ExtensionProvider{
				Name: providerName,
			},
		},
	}

	for _, gateway := range vs.Spec.Gateways {
		gwKey := common.NamespacedNameToObjectKey(gateway, vs.Namespace)

		authPolicy := istiosecurityv1beta1.AuthorizationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getAuthPolicyName(gwKey.Name, vs.Name),
				Namespace: vs.Namespace,
			},
			Spec: authPolicySpec,
		}

		if err := controllerutil.SetOwnerReference(vs, &authPolicy, r.Client().Scheme()); err != nil {
			logger.Error(err, "failed to add owner ref to AuthorizationPolicy resource")
			return err
		}
		err := r.ReconcileResource(ctx, &istiosecurityv1beta1.AuthorizationPolicy{}, &authPolicy, alwaysUpdateAuthPolicy)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "ReconcileResource failed to create/update AuthorizationPolicy resource")
			return err
		}
	}

	logger.Info("successfully created/updated AuthorizationPolicy resource(s)")
	return nil
}

func normalizeStringMatch(sm *networkingv1alpha3.StringMatch) string {
	if prefix := sm.GetPrefix(); prefix != "" {
		return prefix + "*"
	}
	if exact := sm.GetExact(); exact != "" {
		return exact
	}
	// Regex string match is not supported because authpolicy doesn't as well.
	return ""
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&istionetworkingv1alpha3.VirtualService{}, builder.WithPredicates(virtualServicePredicate())).
		Owns(&istiosecurityv1beta1.AuthorizationPolicy{}).
		WithLogger(log.Log). // use base logger, the manager will add prefixes for watched sources
		Complete(r)
}

func virtualServicePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, present := e.Object.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return present
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if _, present := e.ObjectOld.GetAnnotations()[KuadrantAuthProviderAnnotation]; present {
				return true
			}
			_, present := e.ObjectNew.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return present
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			_, present := e.Object.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return present
		},
	}
}
