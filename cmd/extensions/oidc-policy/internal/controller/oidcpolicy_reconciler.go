package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/kuadrant/limitador-operator/pkg/helpers"
	"k8s.io/apimachinery/pkg/api/meta"

	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

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
	return &OIDCPolicyReconciler{
		BaseReconciler: reconcilers.NewLazyBaseReconciler(),
	}
}

func (r *OIDCPolicyReconciler) WithLogger(logger logr.Logger) *OIDCPolicyReconciler {
	r.logger = logger
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

// TODO: Current OIDC Workflow works only for Browser apps, needs implementation for Native apps

func (r *OIDCPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kCtx types.KuadrantCtx) (reconcile.Result, error) {
	r.WithLogger(utils.LoggerFromContext(ctx).WithName("OIDCPolicyReconciler"))
	r.logger.Info("Reconciling OIDCPolicy")

	oidcPolicy := &kuadrantv1alpha1.OIDCPolicy{}
	if err := r.Client().Get(ctx, request.NamespacedName, oidcPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.logger.Error(err, "OIDCPolicy not found")
			return reconcile.Result{}, nil
		}
		r.logger.Error(err, "Failed to get OIDCPolicy")
		return reconcile.Result{}, err
	}

	if oidcPolicy.GetDeletionTimestamp() != nil {
		r.logger.Info("OIDCPolicy marked to be deleted")
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
		r.logger.Error(err, "Failed to resolve ingress gateway info")
		return reconcile.Result{}, err
	}

	specResult, specErr := r.reconcileSpec(ctx, oidcPolicy, &ingressGatewayData)

	statusResult, statusErr := r.reconcileStatus(ctx, oidcPolicy, specErr)

	if specErr != nil {
		return reconcile.Result{}, specErr
	}

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if specResult.Requeue {
		r.logger.Info("Reconciling OIDCPolicy spec not finished. Requeueing.")
		return specResult, nil
	}

	if statusResult.Requeue {
		r.logger.Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	r.logger.Info("successfully reconciled")
	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileSpec(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (reconcile.Result, error) { //nolint:unparam
	// Reconcile AuthPolicy for the oidc policy http route
	if err := r.reconcileMainAuthPolicy(ctx, pol, igw); err != nil {
		r.logger.Error(err, "Failed to reconcile main AuthPolicy")
		return reconcile.Result{}, err
	}

	// Reconcile HTTPRoute for the callback for exchanging code/token
	if err := r.reconcileCallbackHTTPRoute(ctx, pol, igw); err != nil {
		r.logger.Error(err, "Failed to reconcile callback HTTPRoute")
		return reconcile.Result{}, err
	}
	// Reconcile AuthPolicy for the Token exchange flow with metadata http call
	if err := r.reconcileCallbackAuthPolicy(ctx, pol, igw); err != nil {
		r.logger.Error(err, "Failed to reconcile callback AuthPolicy")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *OIDCPolicyReconciler) reconcileStatus(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(pol, specErr)

	equalStatus := pol.Status.Equals(newStatus, r.logger)
	r.logger.Info("Status", "status is different", !equalStatus)
	r.logger.Info("Status", "generation is different", pol.Generation != pol.Status.ObservedGeneration)
	if equalStatus && pol.Generation == pol.Status.ObservedGeneration {
		// Steady state
		r.logger.Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	r.logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", pol.Status.ObservedGeneration, newStatus.ObservedGeneration))

	pol.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, pol)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			r.logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *OIDCPolicyReconciler) calculateStatus(pol *kuadrantv1alpha1.OIDCPolicy, specErr error) *kuadrantv1alpha1.OIDCPolicyStatus {
	newStatus := &kuadrantv1alpha1.OIDCPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: helpers.DeepCopyConditions(pol.Status.Conditions),
	}

	availableCond := r.readyCondition(specErr)

	meta.SetStatusCondition(&newStatus.Conditions, *availableCond)

	return newStatus
}

func (r *OIDCPolicyReconciler) readyCondition(specErr error) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    kuadrantv1alpha1.StatusConditionReady,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "OIDCPolicy is ready",
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ReconciliationError"
		cond.Message = specErr.Error()
		return cond
	}

	return cond
}

func (r *OIDCPolicyReconciler) reconcileMainAuthPolicy(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) error {
	desiredAuthPol, err := buildMainAuthPolicy(pol, igw)
	if err != nil {
		r.logger.Error(err, "Failed to build main AuthPolicy")
		return err
	}
	err = controllerutil.SetControllerReference(pol, desiredAuthPol, r.Scheme())
	if err != nil {
		r.logger.Error(err, "Error setting OwnerReference on AuthPolicy")
		return err
	}
	authPolicyMutators := make([]authPolicyMutateFn, 0)
	authPolicyMutators = append(authPolicyMutators, authPolicyTargetRefMutator, mainAuthPolicyIssuerURLMutator)
	autPolicyMutator := authPolicyMutator(authPolicyMutators...)

	if err = r.reconcileAuthPolicy(ctx, desiredAuthPol, autPolicyMutator); err != nil {
		r.logger.Error(err, "Error reconciling OIDC Main AuthPolicy")
		return err
	}

	r.logger.Info("Successfully reconciled OIDC Main AuthPolicy")
	return nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackAuthPolicy(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) error {
	desiredAuthPol := buildCallbackAuthPolicy(pol, igw)

	err := controllerutil.SetControllerReference(pol, desiredAuthPol, r.Scheme())
	if err != nil {
		r.logger.Error(err, "Error setting OwnerReference on callback AuthPolicy")
		return err
	}

	authPolicyMutators := make([]authPolicyMutateFn, 0)
	authPolicyMutators = append(authPolicyMutators, authPolicyTargetRefMutator)
	autPolicyMutator := authPolicyMutator(authPolicyMutators...)

	if err = r.reconcileAuthPolicy(ctx, desiredAuthPol, autPolicyMutator); err != nil {
		r.logger.Error(err, "Error reconciling OIDC Callback AuthPolicy")
		return err
	}

	r.logger.Info("Successfully reconciled OIDC Callback AuthPolicy")
	return nil
}

func (r *OIDCPolicyReconciler) reconcileCallbackHTTPRoute(ctx context.Context, pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) error {
	desiredHTTPRoute := buildCallbackHTTPRoute(pol, igw)

	err := controllerutil.SetControllerReference(pol, desiredHTTPRoute, r.Scheme())
	if err != nil {
		r.logger.Error(err, "Error setting OwnerReference on callback HTTPRoute")
		return err
	}

	// For the time being, creat only
	if err = r.reconcileHTTPRoute(ctx, desiredHTTPRoute, reconcilers.CreateOnlyMutator); err != nil {
		r.logger.Error(err, "Error reconciling OIDC Callback HTTPRoute")
		return err
	}

	r.logger.Info("Successfully reconciled OIDC Callback HTTPRoute")
	return nil
}

func (r *OIDCPolicyReconciler) reconcileAuthPolicy(ctx context.Context, desired *kuadrantv1.AuthPolicy, mutatefn reconcilers.MutateFn) error {
	return r.ReconcileResource(ctx, &kuadrantv1.AuthPolicy{}, desired, mutatefn)
}

func (r *OIDCPolicyReconciler) reconcileHTTPRoute(ctx context.Context, desired *gatewayapiv1.HTTPRoute, mutatefn reconcilers.MutateFn) error {
	return r.ReconcileResource(ctx, &gatewayapiv1.HTTPRoute{}, desired, mutatefn)
}

func buildMainAuthPolicy(pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (*kuadrantv1.AuthPolicy, error) {
	authorizeURL := pol.GetAuthorizeURL(igw.GetURL())
	serializedAuthorizeURL, err := json.Marshal(authorizeURL)
	if err != nil {
		return nil, err
	}

	setCookie := fmt.Sprintf(`
"target=" + request.path + "; domain=%s; HttpOnly; %s SameSite=Lax; Path=/; Max-Age=3600"`, igw.Hostname, getSecureFlag(igw.Protocol))

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
									Credentials: authorinov1beta3.Credentials{
										Cookie: &authorinov1beta3.Named{Name: "jwt"},
									},
								},
							},
						},
						/*Authorization: map[string]kuadrantv1.MergeableAuthorizationSpec{
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
														Predicate: "\"evil-genius-cupcakes\" in auth.identity.groups_direct", // TODO: Define authorization claims in CRD
													},
												},
											},
										},
									},
								},
							},
						},*/
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
	path := kuadrantv1alpha1.CallbackPath
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

func buildCallbackAuthPolicy(pol *kuadrantv1alpha1.OIDCPolicy, igw *ingressGatewayInfo) *kuadrantv1.AuthPolicy {
	igwURL := igw.GetURL()
	igwHostname := igw.Hostname
	tokenExchangeURL := pol.GetIssuerTokenExchangeURL()
	redirectURI := pol.GetRedirectURL(igwURL)
	authorizeURL := pol.GetAuthorizeURL(igwURL)

	setCookie := fmt.Sprintf(`
"jwt=" + auth.metadata.token.id_token + "; domain=%s; HttpOnly; %s SameSite=Lax; Path=/; Max-Age=3600"
`, igwHostname, getSecureFlag(igw.Protocol))
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
									Code: 302,
									Headers: map[string]authorinov1beta3.ValueOrSelector{
										"location": {
											Expression: "auth.authorization.location.location",
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
	}
}

type authPolicyMutateFn func(desired, existing *kuadrantv1.AuthPolicy) bool

func authPolicyMutator(opts ...authPolicyMutateFn) reconcilers.MutateFn {
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

func authPolicyTargetRefMutator(desired, existing *kuadrantv1.AuthPolicy) bool {
	update := false

	if !reflect.DeepEqual(existing.Spec.TargetRef, desired.Spec.TargetRef) {
		existing.Spec.TargetRef = desired.Spec.TargetRef
		update = true
	}

	return update
}

func mainAuthPolicyIssuerURLMutator(desired, existing *kuadrantv1.AuthPolicy) bool {
	update := false

	if !reflect.DeepEqual(existing.Spec.Overrides.AuthScheme.Authentication["oidc"].Jwt.IssuerUrl, desired.Spec.Overrides.AuthScheme.Authentication["oidc"].Jwt.IssuerUrl) {
		existing.Spec.Overrides.AuthScheme.Authentication["oidc"].Jwt.IssuerUrl = desired.Spec.Overrides.AuthScheme.Authentication["oidc"].Jwt.IssuerUrl
		existing.Spec.Overrides.AuthScheme.Authorization["oidc"].Conditions[0].Predicate = desired.Spec.Overrides.AuthPolicySpecProper.AuthScheme.Authorization["oidc"].AuthorizationSpec.CommonEvaluatorSpec.Conditions[0].Predicate
		update = true
	}

	return update
}

func getSecureFlag(protocol gatewayapiv1.ProtocolType) string {
	flag := ""
	if protocol == gatewayapiv1.HTTPSProtocolType {
		flag = "Secure;"
	}
	return flag
}
