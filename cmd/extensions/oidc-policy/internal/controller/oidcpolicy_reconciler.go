package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	types2 "k8s.io/apimachinery/pkg/types"

	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"

	"github.com/go-logr/logr"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type OIDCPolicyReconciler struct {
	*reconcilers.BaseReconciler
	logger logr.Logger
}

func NewOIDCPolicyReconciler() *OIDCPolicyReconciler {
	return &OIDCPolicyReconciler{}
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

func (r *OIDCPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, _ types.KuadrantCtx) (reconcile.Result, error) {
	r.logger = utils.LoggerFromContext(ctx).WithName("OIDCPolicyReconciler")
	r.logger.Info("Reconciling OIDCPolicy")

	oidcPolicy := &kuadrantv1alpha1.OIDCPolicy{}
	err := r.Client().Get(ctx, request.NamespacedName, oidcPolicy)
	if errors.IsNotFound(err) {
		r.logger.Error(err, "Failed to get OIDCPolicy")
		return reconcile.Result{}, err
	}

	_, specErr := r.reconcileSpec(ctx, oidcPolicy)

	if specErr != nil {
		return reconcile.Result{}, specErr
	}

	r.logger.Info("successfully reconciled")
	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileSpec(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy) (reconcile.Result, error) { //nolint:unparam
	// Reconcile AuthPolicy for the oidc policy http route
	if err := r.reconcileMainAuthPolicy(ctx, pol); err != nil {
		r.logger.Error(err, "Failed to reconcile main auth policy")
		return reconcile.Result{}, err
	}

	// TODO: Reconcile HTTPRoute for the callback for exchanging code/token
	if err := r.reconcileCallbackHTTPRoute(ctx, pol); err != nil {
		r.logger.Error(err, "Failed to reconcile callback HTTP route")
		return reconcile.Result{}, err
	}
	// TODO: Reconcile AuthPolicy for the Token exchange flow with metadata http call
	if err := r.reconcileCallbackAuthPolicy(ctx, pol); err != nil {
		r.logger.Error(err, "Failed to reconcile callback auth policy")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileMainAuthPolicy(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy) error {
	desiredAuthPol := buildMainAuthPolicy(pol)

	err := controllerutil.SetControllerReference(pol, desiredAuthPol, r.Scheme())
	if err != nil {
		r.logger.Error(err, "Error setting OwnerReference on AuthPolicy",
			"Kind", pol.GetObjectKind().GroupVersionKind().String(),
			"Namespace", pol.GetNamespace(),
			"Name", pol.GetName(),
		)
	}
	authPolicy := &kuadrantv1.AuthPolicy{}
	if err = r.Client().Get(ctx, types2.NamespacedName{Namespace: pol.Namespace, Name: pol.Name}, authPolicy); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// TODO: Check if object is tagged for deletion maybe (?)
		// Create
		if err = r.Client().Create(ctx, desiredAuthPol); err != nil {
			r.logger.Error(err, "Failed to create auth policy")
			return err
		}
	}
	// TODO: If tagged for deletion, delete.

	// TODO: item found successfully, update the AuthPolicy
	return nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackAuthPolicy(_ context.Context, _ *kuadrantv1alpha1.OIDCPolicy) error {
	// TODO: reconcileCallbackAuthPolicy
	return nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackHTTPRoute(_ context.Context, _ *kuadrantv1alpha1.OIDCPolicy) error {
	// TODO: reconcileCallbackHTTPRoute
	return nil
}

func buildMainAuthPolicy(pol *kuadrantv1alpha1.OIDCPolicy) *kuadrantv1.AuthPolicy {
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
						Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
							"oidc": {
								AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
									CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
										Conditions: []authorinov1beta3.PatternExpressionOrRef{
											{
												CelPredicate: authorinov1beta3.CelPredicate{
													Predicate: "auth.identity.iss == " + pol.Spec.Provider.IssuerURL,
												},
											},
										},
									},
									AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
										PatternMatching: &authorinov1beta3.PatternMatchingAuthorizationSpec{
											Patterns: []authorinov1beta3.PatternExpressionOrRef{
												{
													CelPredicate: authorinov1beta3.CelPredicate{
														Predicate: "\"companySomething?\" in auth.identity.groups_direct",
													},
												},
											},
										},
									},
								},
							},
						},
						Response: &kuadrantv1.MergeableResponseSpec{
							Unauthenticated: &kuadrantv1.MergeableDenyWithSpec{
								DenyWithSpec: authorinov1beta3.DenyWithSpec{
									Code: 302,
									Headers: map[string]authorinov1beta3.ValueOrSelector{
										"location": {
											Value: runtime.RawExtension{
												Raw: []byte(pol.Spec.Provider.IssuerURL + "/oauth/authorize?client_id=" + pol.Spec.Provider.ClientID + "&redirect_uri=https://NEED_THIS_FROM_KUADRANT_CTX_OR_CRD/auth/callback&response_type=code&scope=openid"),
											},
										},
										"set-cookie": {
											Value: runtime.RawExtension{
												Raw: []byte(`"target=" + request.path + "; domain=NEED_THIS_FROM_KUADRANT_CTX_OR_CRD; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=3600"`),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
