package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
	secv1beta1types "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// AuthPolicyReconciler reconciles a AuthPolicy object
type AuthPolicyReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

const authPolicyFinalizer = "authpolicy.kuadrant.io/finalizer"

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=authpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=authpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete

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

	if err := r.reconcileAuthPolicy(ctx, &ap); err != nil {
		logger.Error(err, "failed to reconcile AuthPolicy")
		return ctrl.Result{}, err
	}

	logger.Info("completed reconciling AuthPolicy")
	return ctrl.Result{}, nil
}

// IstioAuthPolicy generates Istio's AuthorizationPolicy using Kuadrant's AuthPolicy
func (r *AuthPolicyReconciler) reconcileAuthPolicy(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger := logr.FromContext(ctx)

	if err := ap.Validate(); err != nil {
		return err
	}

	if err := r.reconcileNetworkResourceBackReference(ctx, ap); err != nil {
		return err
	}

	httpRoute, err := r.fetchHTTPRoute(ctx, ap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("referenced HTTPRoute not found")
			return nil
		}
		return err
	}

	if err := TargetableRoute(httpRoute); err != nil {
		return err
	}

	for _, parentRef := range httpRoute.Spec.ParentRefs {
		gwNamespace := httpRoute.Namespace // consider gateway local if namespace is not given
		if parentRef.Namespace != nil {
			gwNamespace = string(*parentRef.Namespace)
		}
		gwName := string(parentRef.Name)

		for _, policyConfig := range ap.Spec.PolicyConfigs {
			// convert []Rule  to []*Rule
			rulePtrSlice := []*secv1beta1types.Rule{}
			for idx := range policyConfig.Rules {
				// TODO(rahulanand16nov): Do the check and append instead of force hostname from httproute
				for _, toRule := range policyConfig.Rules[idx].To {
					if toRule.Operation != nil {
						toRule.Operation.Hosts = HostnamesToStrings(httpRoute.Spec.Hostnames)
					}
				}
				rulePtrSlice = append(rulePtrSlice, &policyConfig.Rules[idx])
			}

			actionInt := secv1beta1types.AuthorizationPolicy_Action_value[string(policyConfig.Action)]

			authPolicy := secv1beta1resources.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					// TODO(rahulanand16nov): take care of multiple custom action policies with different providers
					Name:      getAuthPolicyName(gwName, httpRoute.Name, string(policyConfig.Action)),
					Namespace: gwNamespace,
				},
				Spec: secv1beta1types.AuthorizationPolicy{
					Action: secv1beta1types.AuthorizationPolicy_Action(actionInt),
					Rules:  rulePtrSlice,
					Selector: &v1beta1.WorkloadSelector{
						MatchLabels: map[string]string{}, // TODO(rahulanand16nov): fetch from gateway
					},
				},
			}

			if string(policyConfig.Action) == "CUSTOM" {
				authPolicy.Spec.ActionDetail = &secv1beta1types.AuthorizationPolicy_Provider{
					Provider: &secv1beta1types.AuthorizationPolicy_ExtensionProvider{
						Name: policyConfig.Provider,
					},
				}
			}

			err := r.ReconcileResource(ctx, &secv1beta1resources.AuthorizationPolicy{}, &authPolicy, alwaysUpdateAuthPolicy)
			if err != nil && !apierrors.IsAlreadyExists(err) {
				logger.Error(err, "ReconcileResource failed to create/update AuthorizationPolicy resource")
				return err
			}
		}
	}

	return nil
}

func (r *AuthPolicyReconciler) reconcileNetworkResourceBackReference(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger := logr.FromContext(ctx)
	httpRoute, err := r.fetchHTTPRoute(ctx, ap)
	if err != nil {
		// The object should also exist
		return err
	}

	// Reconcile the back reference:
	httpRouteAnnotations := httpRoute.GetAnnotations()
	if httpRouteAnnotations == nil {
		httpRouteAnnotations = map[string]string{}
	}

	apKey := client.ObjectKeyFromObject(ap)
	val, present := httpRouteAnnotations[common.AuthPolicyBackRefAnnotation]
	if present {
		if val != apKey.String() {
			return fmt.Errorf("the target HTTPRoute {%s} is already referenced by authpolicy %s", client.ObjectKeyFromObject(httpRoute), apKey.String())
		}
	} else {
		httpRouteAnnotations[common.AuthPolicyBackRefAnnotation] = apKey.String()
		httpRoute.SetAnnotations(httpRouteAnnotations)
		err := r.UpdateResource(ctx, httpRoute)
		logger.V(1).Info("reconcileNetworkResourceBackReference: update HTTPRoute", "httpRoute", client.ObjectKeyFromObject(httpRoute), "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *AuthPolicyReconciler) removeIstioAuthPolicy(ctx context.Context, ap *apimv1alpha1.AuthPolicy) error {
	logger := logr.FromContext(ctx)
	logger.Info("Removing Istio's AuthorizationPolicy")

	httpRoute, err := r.fetchHTTPRoute(ctx, ap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("referenced HTTPRoute not found")
			return nil
		}
		return err
	}

	for _, parentRef := range httpRoute.Spec.ParentRefs {
		gwNamespace := httpRoute.Namespace
		if parentRef.Namespace != nil {
			gwNamespace = string(*parentRef.Namespace)
		}
		gwName := string(parentRef.Name)

		for _, policyConfig := range ap.Spec.PolicyConfigs {
			authPolicyKey := client.ObjectKey{
				Namespace: gwNamespace,
				Name:      getAuthPolicyName(gwName, httpRoute.Name, string(policyConfig.Action)),
			}

			istioAuthPolicy := &secv1beta1resources.AuthorizationPolicy{}
			if err := r.GetResource(ctx, authPolicyKey, istioAuthPolicy); err != nil {
				logger.Error(err, "failed to fetch Istio's AuthorizationPolicy")
				return err
			}

			if err := r.DeleteResource(ctx, istioAuthPolicy); err != nil {
				logger.Error(err, "failed to delete Istio's AuthorizationPolicy")
				return err
			}
		}
	}

	logger.Info("removed Istio's AuthorizationPolicy")
	return nil
}

// fetchHTTPRoute fetches the HTTPRoute described in targetRef *within* AuthPolicy's namespace.
func (r *AuthPolicyReconciler) fetchHTTPRoute(ctx context.Context, ap *apimv1alpha1.AuthPolicy) (*gatewayapiv1alpha2.HTTPRoute, error) {
	logger := logr.FromContext(ctx)
	key := client.ObjectKey{
		Name:      string(ap.Spec.TargetRef.Name),
		Namespace: ap.Namespace,
	}

	httpRoute := &gatewayapiv1alpha2.HTTPRoute{}
	err := r.Client().Get(ctx, key, httpRoute)
	logger.V(1).Info("fetchHTTPRoute", "httpRoute", key, "err", err)
	if err != nil {
		return nil, err
	}

	return httpRoute, nil
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
		Complete(r)
}
