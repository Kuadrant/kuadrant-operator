package controller

import (
	"context"

	kuadpolmachinery "github.com/kuadrant/policy-machinery/controller"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/go-logr/logr"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type OIDCPolicyReconciler struct {
}

// kuadrant permissions
//+kubebuilder:rbac:groups=kuadrant.io,resources=oidcpolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=oidcpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=oidcpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/finalizers,verbs=update

// gateway-api permissions
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;create;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

func (r *OIDCPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, _ *types.KuadrantCtx) (reconcile.Result, error) {
	logger := utils.LoggerFromContext(ctx).WithName("OIDCPolicyReconciler")
	logger.Info("Reconciling OIDCPolicy")

	dynClient, err := utils.DynamicClientFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	gvr := schema.GroupVersion{Group: "kuadrant.io", Version: "v1alpha1"}.WithResource("oidcpolicies")
	oidcPolicyResource := dynClient.Resource(gvr).Namespace(request.Namespace)

	unstructuredOidcPol, err := oidcPolicyResource.Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(nil, "Could not find OIDCPolicy")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get OIDCPolicy")
		return reconcile.Result{}, err
	}

	result, err := kuadpolmachinery.Restructure[v1alpha1.OIDCPolicy](unstructuredOidcPol)
	if err != nil {
		logger.Error(err, "Failed to restructure OIDCPolicy")
		return reconcile.Result{}, err
	}
	oidcPolicy := result.(v1alpha1.OIDCPolicy)

	_, specErr := r.reconcileSpec(ctx, dynClient, &logger, &oidcPolicy)

	if specErr != nil {
		return reconcile.Result{}, specErr
	}

	logger.Info("successfully reconciled")
	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileSpec(ctx context.Context, dynClient *dynamic.DynamicClient, logger *logr.Logger, pol *v1alpha1.OIDCPolicy) (reconcile.Result, error) { //nolint:unparam
	// Reconcile AuthPolicy for the oidc policy http route
	desiredAuthPol := buildAuthPolicy(pol)
	desiredAuthPolUnstructured, err := kuadpolmachinery.Destruct(desiredAuthPol)
	if err != nil {
		logger.Error(err, "Failed to destruct AuthPolicy")
		return reconcile.Result{}, err
	}
	// TODO Set Owner reference of AuthPolicy

	gvr := schema.GroupVersion{Group: "kuadrant.io", Version: "v1"}.WithResource("authpolicies")
	authPolicyResource := dynClient.Resource(gvr).Namespace(pol.Namespace)

	_, err = authPolicyResource.Get(ctx, pol.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(nil, "Failed to get auth policy")
			return reconcile.Result{}, nil
		}

		// Not found, let's create
		if _, err = authPolicyResource.Create(ctx, desiredAuthPolUnstructured, metav1.CreateOptions{}); err != nil {
			logger.Error(err, "Failed to create auth policy")
			return reconcile.Result{}, err
		}
	}

	// TODO: item found successfully, update the AuthPolicy

	// TODO: Reconcile HTTPRoute for the callback for exchanging code/token
	// TODO: Reconcile AuthPolicy for the Token exchange flow with metadata http call

	return reconcile.Result{}, nil
}

func buildAuthPolicy(pol *v1alpha1.OIDCPolicy) *kuadrantv1.AuthPolicy {
	return &kuadrantv1.AuthPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthPolicy",
			APIVersion: kuadrantv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pol.Name,
			Namespace: pol.Namespace,
		},
		Spec: kuadrantv1.AuthPolicySpec{
			TargetRef: pol.Spec.TargetRef,
			Overrides: &kuadrantv1.MergeableAuthPolicySpec{
				Strategy: "merge",
				AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
					AuthScheme: &kuadrantv1.AuthSchemeSpec{
						Authentication: map[string]kuadrantv1.MergeableAuthenticationSpec{
							"oidc": {
								AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
									AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
										Jwt: &authorinov1beta3.JwtAuthenticationSpec{
											IssuerUrl: pol.Spec.Provider.IssuerURL,
										},
									},
								},
							},
						},
						//Response: &kuadrantv1.MergeableResponseSpec{
						//	Unauthenticated: &kuadrantv1.MergeableDenyWithSpec{
						//		DenyWithSpec: authorinov1beta3.DenyWithSpec{
						//			Code: 302,
						//			Headers: map[string]authorinov1beta3.ValueOrSelector{
						//				"location": {
						//					Value: runtime.RawExtension{
						//						Raw: []byte(pol.Spec.Provider.IssuerURL + "/oauth/authorize?client_id=" + pol.Spec.Provider.ClientID + "&redirect_uri=https://NEED_THIS_FROM_KUADRANT_CTX_OR_CRD/auth/callback&response_type=code&scope=openid"),
						//					},
						//				},
						//			},
						//		},
						//		Source: "dassource",
						//	},
						//},
					},
				},
			},
		},
	}
}
