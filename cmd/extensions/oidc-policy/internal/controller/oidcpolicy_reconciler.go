package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/samber/lo"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type OIDCPolicyReconciler struct {
	types.ExtensionBase
	kCtx types.KuadrantCtx
}

type ingressGatewayInfo struct {
	Hostname  string                    `json:"hostname"`
	Name      string                    `json:"name"`
	Namespace string                    `json:"namespace"`
	Protocol  gatewayapiv1.ProtocolType `json:"protocol"`
	url       *url.URL
}

func (g *ingressGatewayInfo) GetURL() *url.URL {
	if g.url == nil {
		g.url = &url.URL{
			Scheme: strings.ToLower(string(g.Protocol)),
			Host:   g.Hostname,
		}
	}
	return g.url
}

func NewOIDCPolicyReconciler() *OIDCPolicyReconciler {
	return &OIDCPolicyReconciler{}
}

func (r *OIDCPolicyReconciler) WithKuadrantCtx(kCtx types.KuadrantCtx) *OIDCPolicyReconciler {
	r.kCtx = kCtx
	return r
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

// TODO: Current OIDC Workflow works only for Browser apps and Native apps that manage the Auth via browser
// TODO: It only implements Authentication using the Authorization Code Flow (Recommended). Missing Implicit and Hybrid Flow
// TODO: Expand TokenSource to work with credentials other than cookie

func (r *OIDCPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}
	r.WithKuadrantCtx(kCtx)

	r.Logger.Info("Reconciling OIDCPolicy")

	oidcPolicy := &kuadrantv1alpha1.OIDCPolicy{}
	if err := r.Client.Get(ctx, request.NamespacedName, oidcPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Error(err, "OIDCPolicy not found")
			return reconcile.Result{}, nil
		}
		r.Logger.Error(err, "Failed to get OIDCPolicy")
		return reconcile.Result{}, err
	}

	if oidcPolicy.GetDeletionTimestamp() != nil {
		r.Logger.Info("OIDCPolicy marked to be deleted")
		return ctrl.Result{}, nil
	}

	ingressGatewayData, err := extcontroller.Resolve[ingressGatewayInfo](
		ctx,
		kCtx,
		oidcPolicy,
		`{"protocol": self.findGateways()[0].spec.listeners[0].protocol,
		"hostname": self.findGateways()[0].spec.listeners[0].hostname,
		"name": self.findGateways()[0].metadata.name,
		"namespace": self.findGateways()[0].metadata.namespace}`,
		true)

	if err != nil {
		r.Logger.Error(err, "Failed to resolve ingress gateway info")
		return reconcile.Result{}, err
	}

	r.Logger.V(1).Info("Resolving ingress gateway info", "ingressGatewayData", ingressGatewayData)

	createdPolicies, specErr := r.reconcileSpec(ctx, oidcPolicy, &ingressGatewayData)
	statusResult, statusErr := r.reconcileStatus(ctx, oidcPolicy, createdPolicies, specErr)

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if statusResult.RequeueAfter > 0 {
		r.Logger.Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	r.Logger.Info("successfully reconciled")
	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileSpec(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) ([]*kuadrantv1.AuthPolicy, error) {
	// Reconcile AuthPolicy for the oidc policy http route
	mainAuthPol, err := r.reconcileMainAuthPolicy(ctx, pol, igw)
	if err != nil {
		r.Logger.Error(err, "Failed to reconcile main AuthPolicy")
		return nil, err
	}

	// Reconcile HTTPRoute for the callback for exchanging code/token
	if err = r.reconcileCallbackHTTPRoute(ctx, pol, igw); err != nil {
		r.Logger.Error(err, "Failed to reconcile callback HTTPRoute")
		return nil, err
	}
	// Reconcile AuthPolicy for the Token exchange flow with metadata http call
	callbackPol, err := r.reconcileCallbackAuthPolicy(ctx, pol, igw)
	if err != nil {
		r.Logger.Error(err, "Failed to reconcile callback AuthPolicy")
		return nil, err
	}

	return []*kuadrantv1.AuthPolicy{mainAuthPol, callbackPol}, nil
}

func (r *OIDCPolicyReconciler) reconcileStatus(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, authPolicies []*kuadrantv1.AuthPolicy, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(pol, authPolicies, specErr)

	equalStatus := pol.Status.Equals(newStatus, r.Logger)
	r.Logger.Info("Status", "status is different", !equalStatus)
	r.Logger.Info("Status", "generation is different", pol.Generation != pol.Status.ObservedGeneration)
	if equalStatus && pol.Generation == pol.Status.ObservedGeneration {
		// Steady state
		r.Logger.Info("Status was not updated")
		return reconcile.Result{}, specErr
	}

	r.Logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", pol.Status.ObservedGeneration, newStatus.ObservedGeneration))

	pol.Status = *newStatus
	updateErr := r.Client.Status().Update(ctx, pol)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			r.Logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, specErr
}

func (r *OIDCPolicyReconciler) calculateStatus(pol *kuadrantv1alpha1.OIDCPolicy, authPolicies []*kuadrantv1.AuthPolicy, specErr error) *kuadrantv1alpha1.OIDCPolicyStatus {
	newStatus := &kuadrantv1alpha1.OIDCPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))

	if specErr != nil {
		for _, authPolicy := range authPolicies {
			cond, found := lo.Find(authPolicy.Status.GetConditions(), func(c metav1.Condition) bool {
				return c.Type == string(types.PolicyConditionEnforced)
			})
			if !found || cond.Status == metav1.ConditionFalse {
				meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, fmt.Errorf("%s AuthPolicy has not been enforced", authPolicy.Name), false))
				return newStatus
			}
		}
	}

	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, specErr, true))
	return newStatus
}

func (r *OIDCPolicyReconciler) reconcileMainAuthPolicy(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (*kuadrantv1.AuthPolicy, error) {
	desiredAuthPol, err := buildMainAuthPolicy(pol, igw)
	if err != nil {
		r.Logger.Error(err, "Failed to build main AuthPolicy")
		return nil, err
	}
	err = controllerutil.SetControllerReference(pol, desiredAuthPol, r.Scheme)
	if err != nil {
		r.Logger.Error(err, "Error setting OwnerReference on AuthPolicy")
		return nil, err
	}
	authPolicyMutators := make([]authPolicyMutateFn, 0)
	authPolicyMutators = append(authPolicyMutators, authPolicySpecMutator)
	policyMutator := authPolicyMutator(authPolicyMutators...)

	reconciledPol, err := r.reconcileAuthPolicy(ctx, desiredAuthPol, policyMutator)
	if err != nil {
		r.Logger.Error(err, "Error reconciling OIDC Main AuthPolicy")
		return nil, err
	}

	r.Logger.Info("Successfully reconciled OIDC Main AuthPolicy")
	return reconciledPol, nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackAuthPolicy(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (*kuadrantv1.AuthPolicy, error) {
	desiredAuthPol, err := buildCallbackAuthPolicy(pol, igw)
	if err != nil {
		return nil, err
	}
	err = controllerutil.SetControllerReference(pol, desiredAuthPol, r.Scheme)
	if err != nil {
		r.Logger.Error(err, "Error setting OwnerReference on callback AuthPolicy")
		return nil, err
	}

	authPolicyMutators := make([]authPolicyMutateFn, 0)
	authPolicyMutators = append(authPolicyMutators, authPolicySpecMutator)
	policyMutator := authPolicyMutator(authPolicyMutators...)

	reconciledPol, err := r.reconcileAuthPolicy(ctx, desiredAuthPol, policyMutator)
	if err != nil {
		r.Logger.Error(err, "Error reconciling OIDC Callback AuthPolicy")
		return nil, err
	}

	r.Logger.Info("Successfully reconciled OIDC Callback AuthPolicy")
	return reconciledPol, nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackHTTPRoute(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) error {
	desiredHTTPRoute := buildCallbackHTTPRoute(pol, igw)

	err := controllerutil.SetControllerReference(pol, desiredHTTPRoute, r.Scheme)
	if err != nil {
		r.Logger.Error(err, "Error setting OwnerReference on callback HTTPRoute")
		return err
	}

	httpRouteMutators := make([]httpRouteMutateFn, 0)
	httpRouteMutators = append(httpRouteMutators, httpObjectMetaRouteMutator, httpSpecRouteMutator)
	routeMutator := httpRouteMutator(httpRouteMutators...)

	if err = r.reconcileHTTPRoute(ctx, desiredHTTPRoute, routeMutator); err != nil {
		r.Logger.Error(err, "Error reconciling OIDC Callback HTTPRoute")
		return err
	}

	r.Logger.Info("Successfully reconciled OIDC Callback HTTPRoute")
	return nil
}

func (r *OIDCPolicyReconciler) reconcileAuthPolicy(ctx context.Context, desired *kuadrantv1.AuthPolicy, mutatefn types.MutateFn) (*kuadrantv1.AuthPolicy, error) {
	obj, err := r.kCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desired, mutatefn)
	return obj.(*kuadrantv1.AuthPolicy), err
}

func (r *OIDCPolicyReconciler) reconcileHTTPRoute(ctx context.Context, desired *gatewayapiv1.HTTPRoute, mutatefn types.MutateFn) error {
	_, err := r.kCtx.ReconcileObject(ctx, &gatewayapiv1.HTTPRoute{}, desired, mutatefn)
	return err
}

func buildMainAuthPolicy(pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (*kuadrantv1.AuthPolicy, error) {
	authorizeURL, err := pol.GetAuthorizeURL(igw.GetURL())
	if err != nil {
		return nil, err
	}
	serializedAuthorizeURL, err := json.Marshal(authorizeURL)
	if err != nil {
		return nil, err
	}

	setCookie := fmt.Sprintf(`
"target=" + request.path + "; domain=%s; HttpOnly; %s SameSite=Lax; Path=/; Max-Age=3600"`, igw.Hostname, getSecureFlag(igw.Protocol))

	var authorization = map[string]kuadrantv1.MergeableAuthorizationSpec{}
	var authPatterns []authorinov1beta3.PatternExpressionOrRef
	claims := pol.GetClaims()

	if len(claims) > 0 {
		for k, v := range claims {
			authPatterns = append(authPatterns, authorinov1beta3.PatternExpressionOrRef{
				CelPredicate: authorinov1beta3.CelPredicate{
					Predicate: fmt.Sprintf(`"%s" in auth.identity.%s`, v, k),
				},
			})
		}
		authorization = map[string]kuadrantv1.MergeableAuthorizationSpec{
			"oidc": {
				AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
					CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
						Conditions: []authorinov1beta3.PatternExpressionOrRef{
							{
								CelPredicate: authorinov1beta3.CelPredicate{
									Predicate: fmt.Sprintf(`auth.identity.iss == "%s"`, pol.Spec.Provider.IssuerURL),
								},
							},
						},
					},
					AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
						PatternMatching: &authorinov1beta3.PatternMatchingAuthorizationSpec{
							Patterns: authPatterns,
						},
					},
				},
			},
		}
	}

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
									Credentials: credentialsSource(pol.GetTokenSource()),
								},
							},
						},
						Authorization: authorization,
						Response: &kuadrantv1.MergeableResponseSpec{
							Unauthenticated: &kuadrantv1.MergeableDenyWithSpec{
								DenyWithSpec: authorinov1beta3.DenyWithSpec{
									Code: 302,
									Headers: map[string]authorinov1beta3.ValueOrSelector{
										"location": {
											Value: runtime.RawExtension{
												Raw: serializedAuthorizeURL,
											},
										},
										"set-cookie": {
											Expression: authorinov1beta3.CelExpression(setCookie),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func buildCallbackHTTPRoute(pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) *gatewayapiv1.HTTPRoute {
	pathMatch := gatewayapiv1.PathMatchPathPrefix
	path := kuadrantv1alpha1.DefaultCallbackPath
	gwName := gatewayapiv1.ObjectName(igw.Name)
	gwNamespace := gatewayapiv1.Namespace(igw.Namespace)

	return &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       machinery.HTTPRouteGroupKind.Kind,
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pol.Name + "-callback",
			Namespace: pol.Namespace,
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:      gwName,
						Namespace: &gwNamespace,
					},
				},
			},
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: ptr.To(gatewayapiv1.HTTPPathMatch{
								Type:  &pathMatch,
								Value: &path,
							}),
						},
					},
				},
			},
		},
	}
}

func buildCallbackAuthPolicy(pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (*kuadrantv1.AuthPolicy, error) {
	igwURL := igw.GetURL()
	tokenExchangeURL, err := pol.GetIssuerTokenExchangeURL()
	if err != nil {
		return nil, err
	}
	authorizeURL, err := pol.GetAuthorizeURL(igwURL)
	if err != nil {
		return nil, err
	}
	redirectURI, err := pol.GetRedirectURL(igwURL)
	if err != nil {
		return nil, err
	}

	callBodyCelExpression := fmt.Sprintf(`
"code=" + request.query.split("&").map(entry, entry.split("=")).filter(pair, pair[0] == "code").map(pair, pair[1])[0] + "&redirect_uri=%s&client_id=%s&grant_type=authorization_code"
`, redirectURI, pol.Spec.Provider.ClientID)

	callbackRoute := gatewayapiv1alpha2.LocalPolicyTargetReference{
		Group: gatewayapiv1alpha2.GroupName,
		Kind:  gatewayapiv1alpha2.Kind("HTTPRoute"),
		Name:  gatewayapiv1alpha2.ObjectName(pol.Name + "-callback"),
	}

	callbackMethod := authorinov1beta3.HttpMethod("POST")

	callbackBody := authorinov1beta3.ValueOrSelector{
		Expression: authorinov1beta3.CelExpression(callBodyCelExpression),
	}

	opaAuthorizationRule := fmt.Sprintf(`cookies := { name: value | raw_cookies := input.request.headers.cookie; cookie_parts := split(raw_cookies, ";"); part := cookie_parts[_]; kv := split(trim(part, " "), "="); count(kv) == 2; name := trim(kv[0], " "); value := trim(kv[1], " ")}
location := concat("", ["%s", cookies.target]) { input.auth.metadata.token.id_token; cookies.target }
location := "%s/baker" { input.auth.metadata.token.id_token; not cookies.target }
location := "%s" { not input.auth.metadata.token.id_token }
allow = true`, igwURL, igwURL, authorizeURL)

	return &kuadrantv1.AuthPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthPolicy",
			APIVersion: kuadrantv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pol.Name + "-callback",
			Namespace: pol.Namespace,
		},
		Spec: kuadrantv1.AuthPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: callbackRoute,
			},
			Overrides: &kuadrantv1.MergeableAuthPolicySpec{
				Strategy: "merge",
				AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
					AuthScheme: &kuadrantv1.AuthSchemeSpec{
						Metadata: map[string]kuadrantv1.MergeableMetadataSpec{
							"token": {
								MetadataSpec: authorinov1beta3.MetadataSpec{
									CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
										Conditions: []authorinov1beta3.PatternExpressionOrRef{
											{
												CelPredicate: authorinov1beta3.CelPredicate{
													Predicate: `request.query.split("&").map(entry, entry.split("=")).filter(pair, pair[0] == "code").map(pair, pair[1]).size() > 0`,
												},
											},
										},
									},
									MetadataMethodSpec: authorinov1beta3.MetadataMethodSpec{
										Http: &authorinov1beta3.HttpEndpointSpec{
											Url:    tokenExchangeURL,
											Method: &callbackMethod,
											Body:   &callbackBody,
										},
									},
								},
							},
						},
						Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
							"location": {
								AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
									AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
										Opa: &authorinov1beta3.OpaAuthorizationSpec{
											Rego:      opaAuthorizationRule,
											AllValues: true,
										},
									},
									CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
										Priority: 1,
									},
								},
							},
							"deny": {
								AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
									AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
										Opa: &authorinov1beta3.OpaAuthorizationSpec{
											Rego: "allow = false",
										},
									},
									CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
										Priority: 2,
									},
								},
							},
						},
						Response: &kuadrantv1.MergeableResponseSpec{
							Unauthorized: &kuadrantv1.MergeableDenyWithSpec{
								DenyWithSpec: authorinov1beta3.DenyWithSpec{
									Code:    302,
									Headers: credentialsHeader(pol.GetTokenSource(), igw),
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

type authPolicyMutateFn func(existing, desired *kuadrantv1.AuthPolicy) bool

func authPolicyMutator(opts ...authPolicyMutateFn) types.MutateFn {
	return func(desiredObj, existingObj client.Object) (bool, error) {
		existing, ok := existingObj.(*kuadrantv1.AuthPolicy)
		if !ok {
			return false, fmt.Errorf("%T is not a *kuadrantv1.AuthPolicy", existingObj)
		}
		desired, ok := desiredObj.(*kuadrantv1.AuthPolicy)
		if !ok {
			return false, fmt.Errorf("%T is not a *kuadrantv1.AuthPolicy", desiredObj)
		}

		update := false

		// Loop through each option
		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}

func authPolicySpecMutator(existing, desired *kuadrantv1.AuthPolicy) bool {
	update := false

	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		update = true
	}

	return update
}

type httpRouteMutateFn func(existing, desired *gatewayapiv1.HTTPRoute) bool

func httpRouteMutator(opts ...httpRouteMutateFn) types.MutateFn {
	return func(desiredObj, existingObj client.Object) (bool, error) {
		existing, ok := existingObj.(*gatewayapiv1.HTTPRoute)
		if !ok {
			return false, fmt.Errorf("%T is not a *gatewayapiv1.HTTPRoute", existingObj)
		}
		desired, ok := desiredObj.(*gatewayapiv1.HTTPRoute)
		if !ok {
			return false, fmt.Errorf("%T is not a *gatewayapiv1.HTTPRoute", desiredObj)
		}
		update := false
		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}
		return update, nil
	}
}

func httpObjectMetaRouteMutator(existing, desired *gatewayapiv1.HTTPRoute) bool {
	update := false

	if !reflect.DeepEqual(existing.ObjectMeta, desired.ObjectMeta) {
		existing.ObjectMeta = desired.ObjectMeta
		update = true
	}

	return update
}

func httpSpecRouteMutator(existing, desired *gatewayapiv1.HTTPRoute) bool {
	update := false

	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		update = true
	}

	return update
}

func credentialsSource(tokenSource *authorinov1beta3.Credentials) authorinov1beta3.Credentials {
	switch tokenSource.GetType() {
	case authorinov1beta3.AuthorizationHeaderCredentials:
		return authorinov1beta3.Credentials{AuthorizationHeader: &authorinov1beta3.Prefixed{Prefix: tokenSource.AuthorizationHeader.Prefix}}
	case authorinov1beta3.CustomHeaderCredentials:
		return authorinov1beta3.Credentials{CustomHeader: &authorinov1beta3.CustomHeader{Named: authorinov1beta3.Named{Name: tokenSource.CustomHeader.Name}}}
	case authorinov1beta3.QueryStringCredentials:
		return authorinov1beta3.Credentials{QueryString: &authorinov1beta3.Named{Name: tokenSource.QueryString.Name}}
	case authorinov1beta3.CookieCredentials:
		return authorinov1beta3.Credentials{Cookie: &authorinov1beta3.Named{Name: tokenSource.Cookie.Name}}
	default:
		return authorinov1beta3.Credentials{Cookie: &authorinov1beta3.Named{Name: kuadrantv1alpha1.DefaultTokenSourceName}}
	}
}

func credentialsHeader(tokenSource *authorinov1beta3.Credentials, igw *ingressGatewayInfo) map[string]authorinov1beta3.ValueOrSelector {
	headers := make(map[string]authorinov1beta3.ValueOrSelector)
	headers["location"] = authorinov1beta3.ValueOrSelector{Expression: "auth.authorization.location.location"}
	switch tokenSource.GetType() {
	case authorinov1beta3.AuthorizationHeaderCredentials:
		headers["Authorization"] = authorinov1beta3.ValueOrSelector{Expression: authorinov1beta3.CelExpression(fmt.Sprintf(`"%s " + auth.metadata.token.id_token`, tokenSource.AuthorizationHeader.Prefix))}
	case authorinov1beta3.CustomHeaderCredentials:
		headers[tokenSource.CustomHeader.Name] = authorinov1beta3.ValueOrSelector{Expression: "auth.metadata.token.id_token"}
	case authorinov1beta3.CookieCredentials:
		headers["set-cookie"] = cookieHeader(tokenSource.Cookie.Name, igw)
	default:
		headers["set-cookie"] = cookieHeader(kuadrantv1alpha1.DefaultTokenSourceName, igw)
	}
	return headers
}

func cookieHeader(cookieName string, igw *ingressGatewayInfo) authorinov1beta3.ValueOrSelector {
	return authorinov1beta3.ValueOrSelector{Expression: authorinov1beta3.CelExpression(fmt.Sprintf(`
"%s=" + auth.metadata.token.id_token + "; domain=%s; HttpOnly; %s SameSite=Lax; Path=/; Max-Age=3600"
`, cookieName, igw.Hostname, getSecureFlag(igw.Protocol)))}
}

func getSecureFlag(protocol gatewayapiv1.ProtocolType) string {
	flag := ""
	if protocol == gatewayapiv1.HTTPSProtocolType {
		flag = "Secure;"
	}
	return flag
}
