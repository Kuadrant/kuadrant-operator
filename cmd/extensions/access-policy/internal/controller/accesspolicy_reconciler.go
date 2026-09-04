package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/access-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/access-policy/internal/translator"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

const gatewayKind = "Gateway"

type AccessPolicyReconciler struct {
	types.ExtensionBase
}

func NewAccessPolicyReconciler() *AccessPolicyReconciler {
	return &AccessPolicyReconciler{}
}

func (r *AccessPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}

	var gateway gatewayapiv1.Gateway
	if err := r.Client.Get(ctx, request.NamespacedName, &gateway); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	var policyList v1alpha1.AccessPolicyList
	if err := r.Client.List(ctx, &policyList, client.InNamespace(gateway.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	var targetedPolicies []v1alpha1.AccessPolicy
	for _, p := range policyList.Items {
		for _, targetRef := range p.Spec.TargetRefs {
			if string(targetRef.Kind) == gatewayKind && string(targetRef.Name) == gateway.Name {
				targetedPolicies = append(targetedPolicies, p)
				break
			}
		}
	}

	authPolicyName := fmt.Sprintf("%s-auth", gateway.Name)
	authPolicy := &kuadrantv1.AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authPolicyName,
			Namespace: gateway.Namespace,
		},
	}

	if len(targetedPolicies) == 0 {
		err := r.Client.Delete(ctx, authPolicy)
		if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Sort policies by CreationTimestamp
	//nolint:staticcheck // QF1008: could remove embedded field "Time" from selector
	slices.SortFunc(targetedPolicies, func(a, b v1alpha1.AccessPolicy) int {
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})

	authentications := make(map[string]kuadrantv1.MergeableAuthenticationSpec)
	authorizations := make(map[string]kuadrantv1.MergeableAuthorizationSpec)

	priority := 0
	hasServiceAccount := false
	hasSPIFFE := false

	validPolicies := make([]*v1alpha1.AccessPolicy, 0)

	for i := range targetedPolicies {
		p := &targetedPolicies[i]

		var currentTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
		for _, targetRef := range p.Spec.TargetRefs {
			if string(targetRef.Kind) == gatewayKind && string(targetRef.Name) == gateway.Name {
				currentTargetRef = targetRef
				break
			}
		}

		if p.Spec.Action == v1alpha1.ActionTypeExternalAuth {
			r.updateStatus(ctx, p, currentTargetRef, v1alpha1.PolicyConditionAccepted, metav1.ConditionFalse, gatewayapiv1alpha2.PolicyConditionReason("Invalid"), "ExternalAuth action is out of scope and not supported")
			continue
		}

		allValid := true
		for _, rule := range p.Spec.Rules {
			var principal string
			if rule.Source.Type == v1alpha1.AuthorizationSourceTypeServiceAccount && rule.Source.ServiceAccount != nil {
				hasServiceAccount = true
				ns := rule.Source.ServiceAccount.Namespace
				if ns == "" {
					ns = p.Namespace
				}
				principal = fmt.Sprintf("system:serviceaccount:%s:%s", ns, rule.Source.ServiceAccount.Name)
			} else if rule.Source.Type == v1alpha1.AuthorizationSourceTypeSPIFFE && rule.Source.SPIFFE != nil {
				hasSPIFFE = true
				principal = string(*rule.Source.SPIFFE)
			}

			var authExprs []string
			if rule.Authorization != nil {
				if string(rule.Authorization.Type) == "CEL" && rule.Authorization.CEL != nil {
					authExpr := rule.Authorization.CEL.Expression
					authExpr = translator.TranslateCEL(authExpr)
					if err := translator.ValidateCEL(authExpr); err != nil {
						r.updateStatus(ctx, p, currentTargetRef, v1alpha1.PolicyConditionAccepted, metav1.ConditionFalse, v1alpha1.PolicyReasonInvalidCEL, "Invalid CEL: "+err.Error())
						allValid = false
						break
					}
					authExprs = append(authExprs, authExpr)
				} else if string(rule.Authorization.Type) == "Inline" {
					// MCP matching
					if rule.Authorization.MCP.MCPBaseProtocolMethodsOption == v1alpha1.MCPBaseProtocolMethodsOptionMatch {
						authExprs = append(authExprs, "request.headers['x-mcp-method'] in ['initialize', 'tools/list', 'completion', 'logging', 'notifications', 'ping'] || request.method in ['GET', 'DELETE']")
					}
					var mcpMethodExprs []string
					if len(rule.Authorization.MCP.Methods) > 0 {
						for _, m := range rule.Authorization.MCP.Methods {
							if len(m.Params) > 0 {
								for _, param := range m.Params {
									mcpMethodExprs = append(mcpMethodExprs, fmt.Sprintf("(request.headers['x-mcp-method'] == '%s' && request.headers['x-mcp-toolname'] == '%s')", m.Name, param))
								}
							} else {
								mcpMethodExprs = append(mcpMethodExprs, fmt.Sprintf("request.headers['x-mcp-method'] == '%s'", m.Name))
							}
						}
					}
					if len(mcpMethodExprs) > 0 {
						authExprs = append(authExprs, "("+strings.Join(mcpMethodExprs, " || ")+")")
					}

					// HTTP Methods
					if len(rule.Authorization.Methods) > 0 {
						var methods []string
						for _, m := range rule.Authorization.Methods {
							methods = append(methods, fmt.Sprintf("'%s'", m))
						}
						authExprs = append(authExprs, fmt.Sprintf("request.method in [%s]", strings.Join(methods, ", ")))
					}

					// HTTP Paths
					if len(rule.Authorization.Paths) > 0 {
						var pathExprs []string
						for _, pMatch := range rule.Authorization.Paths {
							val := ""
							if pMatch.Value != nil {
								val = *pMatch.Value
							}
							matchType := v1alpha1.PathMatchPathPrefix
							if pMatch.Type != nil {
								matchType = *pMatch.Type
							}
							switch matchType {
							case v1alpha1.PathMatchExact:
								pathExprs = append(pathExprs, fmt.Sprintf("request.path == '%s'", val))
							case v1alpha1.PathMatchRegularExpression:
								pathExprs = append(pathExprs, fmt.Sprintf("request.path.matches('%s')", val))
							case v1alpha1.PathMatchPathPrefix:
								fallthrough
							default:
								pathExprs = append(pathExprs, fmt.Sprintf("request.path.startsWith('%s')", val))
							}
						}
						if len(pathExprs) > 0 {
							authExprs = append(authExprs, "("+strings.Join(pathExprs, " || ")+")")
						}
					}

					// HTTP Headers (AND semantics across header matchers)
					if len(rule.Authorization.Headers) > 0 {
						for _, hMatch := range rule.Authorization.Headers {
							hName := strings.ToLower(string(hMatch.Name))
							matchType := v1alpha1.HeaderMatchExact
							if hMatch.Type != nil {
								matchType = *hMatch.Type
							}
							switch matchType {
							case v1alpha1.HeaderMatchRegularExpression:
								authExprs = append(authExprs, fmt.Sprintf("(has(request.headers) && '%s' in request.headers && request.headers['%s'].matches('%s'))", hName, hName, hMatch.Value))
							case v1alpha1.HeaderMatchExact:
								fallthrough
							default:
								authExprs = append(authExprs, fmt.Sprintf("(has(request.headers) && '%s' in request.headers && request.headers['%s'] == '%s')", hName, hName, hMatch.Value))
							}
						}
					}

					// HTTP Hosts
					if len(rule.Authorization.Hosts) > 0 {
						var hostExprs []string
						for _, h := range rule.Authorization.Hosts {
							hostStr := string(h)
							if strings.HasPrefix(hostStr, "*.") {
								domain := strings.TrimPrefix(hostStr, "*.")
								hostExprs = append(hostExprs, fmt.Sprintf("request.host.endsWith('.%s')", domain))
							} else {
								hostExprs = append(hostExprs, fmt.Sprintf("(request.host == '%s' || ('host' in request.headers && request.headers['host'] == '%s'))", hostStr, hostStr))
							}
						}
						if len(hostExprs) > 0 {
							authExprs = append(authExprs, "("+strings.Join(hostExprs, " || ")+")")
						}
					}

					// Ports
					if len(rule.Authorization.Ports) > 0 {
						var portExprs []string
						for _, p := range rule.Authorization.Ports {
							portExprs = append(portExprs, fmt.Sprintf("request.port == %d", p))
						}
						if len(portExprs) > 0 {
							authExprs = append(authExprs, "("+strings.Join(portExprs, " || ")+")")
						}
					}
				}
			}

			if !allValid {
				continue
			}

			whenPredicates := []authorinov1beta3.PatternExpressionOrRef{
				{CelPredicate: authorinov1beta3.CelPredicate{Predicate: "size(auth.authorization) == 0"}},
			}
			if principal != "" {
				whenPredicates = append(whenPredicates, authorinov1beta3.PatternExpressionOrRef{
					CelPredicate: authorinov1beta3.CelPredicate{Predicate: fmt.Sprintf("auth.identity.principal == '%s'", principal)},
				})
			}
			if len(authExprs) > 0 {
				combinedAuthExpr := strings.Join(authExprs, " || ")
				whenPredicates = append(whenPredicates, authorinov1beta3.PatternExpressionOrRef{
					CelPredicate: authorinov1beta3.CelPredicate{Predicate: combinedAuthExpr},
				})
			}

			regoRule := "allow = true"
			if p.Spec.Action == v1alpha1.ActionTypeAllow || p.Spec.Action == "" {
				regoRule = "allow = true"
			} else if string(p.Spec.Action) == "Deny" {
				regoRule = "allow = false"
			}

			ruleKey := fmt.Sprintf("%s-%s", p.Name, rule.Name)
			authorizations[ruleKey] = kuadrantv1.MergeableAuthorizationSpec{
				AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
					CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
						Priority:   priority,
						Conditions: whenPredicates,
					},
					AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
						Opa: &authorinov1beta3.OpaAuthorizationSpec{
							Rego: regoRule,
						},
					},
				},
			}
			priority++
		}

		if allValid {
			validPolicies = append(validPolicies, p)
		}
	}

	// Fail-close rule
	authorizations["fail-close"] = kuadrantv1.MergeableAuthorizationSpec{
		AuthorizationSpec: authorinov1beta3.AuthorizationSpec{
			CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
				Priority: priority,
				Conditions: []authorinov1beta3.PatternExpressionOrRef{
					{CelPredicate: authorinov1beta3.CelPredicate{Predicate: "size(auth.authorization) == 0"}},
				},
			},
			AuthorizationMethodSpec: authorinov1beta3.AuthorizationMethodSpec{
				Opa: &authorinov1beta3.OpaAuthorizationSpec{
					Rego: "allow = false",
				},
			},
		},
	}

	if hasServiceAccount {
		authentications["service-account"] = kuadrantv1.MergeableAuthenticationSpec{
			AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
				AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
					KubernetesTokenReview: &authorinov1beta3.KubernetesTokenReviewSpec{
						Audiences: []string{"https://kubernetes.default.svc.cluster.local"},
					},
				},
				CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
					Conditions: []authorinov1beta3.PatternExpressionOrRef{
						{CelPredicate: authorinov1beta3.CelPredicate{Predicate: "'authorization' in request.headers && request.headers['authorization'].startsWith('Bearer ')"}},
					},
				},
				Overrides: authorinov1beta3.ExtendedProperties{
					"principal": authorinov1beta3.ValueOrSelector{Expression: "auth.identity.user.username"},
				},
			},
		}
	}

	if hasSPIFFE {
		authentications["spiffe"] = kuadrantv1.MergeableAuthenticationSpec{
			AuthenticationSpec: authorinov1beta3.AuthenticationSpec{
				AuthenticationMethodSpec: authorinov1beta3.AuthenticationMethodSpec{
					Plain: &authorinov1beta3.PlainIdentitySpec{
						Expression: "source.principal",
					},
				},
				CommonEvaluatorSpec: authorinov1beta3.CommonEvaluatorSpec{
					Conditions: []authorinov1beta3.PatternExpressionOrRef{
						{CelPredicate: authorinov1beta3.CelPredicate{Predicate: "source.principal.startsWith('spiffe://')"}},
					},
				},
				Overrides: authorinov1beta3.ExtendedProperties{
					"principal": authorinov1beta3.ValueOrSelector{Expression: "source.principal"},
				},
			},
		}
	}

	desiredAuthPolicy := authPolicy.DeepCopy()
	if desiredAuthPolicy.Labels == nil {
		desiredAuthPolicy.Labels = map[string]string{}
	}
	desiredAuthPolicy.Labels["app.kubernetes.io/managed-by"] = "accesspolicy-extension"

	desiredAuthPolicy.Spec.TargetRef = gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
			Group: "gateway.networking.k8s.io",
			Kind:  gatewayKind,
			Name:  gatewayapiv1alpha2.ObjectName(gateway.Name),
		},
	}
	desiredAuthPolicy.Spec.AuthScheme = &kuadrantv1.AuthSchemeSpec{
		Authentication: authentications,
		Authorization:  authorizations,
	}

	if err := controllerutil.SetControllerReference(&gateway, desiredAuthPolicy, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	_, err := kuadrantCtx.ReconcileObject(ctx, &kuadrantv1.AuthPolicy{}, desiredAuthPolicy, authPolicyMutator)
	if err != nil {
		// Update all valid policies with ProgramError
		for _, p := range validPolicies {
			var currentTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
			for _, targetRef := range p.Spec.TargetRefs {
				if string(targetRef.Kind) == gatewayKind && string(targetRef.Name) == gateway.Name {
					currentTargetRef = targetRef
					break
				}
			}
			r.updateStatus(ctx, p, currentTargetRef, v1alpha1.PolicyConditionAccepted, metav1.ConditionFalse, gatewayapiv1alpha2.PolicyConditionReason("ProgramError"), "ProgramError: "+err.Error())
		}
		return reconcile.Result{}, err
	}

	r.Logger.Info("Reconciled AuthPolicy")

	// Update successful status for all valid policies
	for _, p := range validPolicies {
		var currentTargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
		for _, targetRef := range p.Spec.TargetRefs {
			if string(targetRef.Kind) == gatewayKind && string(targetRef.Name) == gateway.Name {
				currentTargetRef = targetRef
				break
			}
		}
		r.updateStatus(ctx, p, currentTargetRef, v1alpha1.PolicyConditionAccepted, metav1.ConditionTrue, v1alpha1.PolicyReasonAccepted, "Policy accepted and valid")
	}

	return reconcile.Result{}, nil
}

func (r *AccessPolicyReconciler) updateStatus(ctx context.Context, policy *v1alpha1.AccessPolicy, targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName, conditionType gatewayapiv1alpha2.PolicyConditionType, status metav1.ConditionStatus, reason gatewayapiv1alpha2.PolicyConditionReason, message string) {
	var ancestor *gatewayapiv1alpha2.PolicyAncestorStatus

	gwGroup := gatewayapiv1.Group("gateway.networking.k8s.io")
	gwKind := gatewayapiv1.Kind("Gateway")
	if targetRef.Group != "" {
		gwGroup = targetRef.Group
	}
	if targetRef.Kind != "" {
		gwKind = targetRef.Kind
	}
	gwNamespace := gatewayapiv1.Namespace(policy.Namespace)

	ancestorRef := gatewayapiv1.ParentReference{
		Group:     &gwGroup,
		Kind:      &gwKind,
		Namespace: &gwNamespace,
		Name:      targetRef.Name,
	}

	for i := range policy.Status.Ancestors {
		if policy.Status.Ancestors[i].AncestorRef.Group != nil && *policy.Status.Ancestors[i].AncestorRef.Group == gwGroup &&
			policy.Status.Ancestors[i].AncestorRef.Kind != nil && *policy.Status.Ancestors[i].AncestorRef.Kind == gwKind &&
			policy.Status.Ancestors[i].AncestorRef.Name == targetRef.Name {
			ancestor = &policy.Status.Ancestors[i]
			break
		}
	}

	if ancestor == nil {
		policy.Status.Ancestors = append(policy.Status.Ancestors, gatewayapiv1alpha2.PolicyAncestorStatus{
			AncestorRef:    ancestorRef,
			ControllerName: "extensions.kuadrant.io/accesspolicy-controller",
		})
		ancestor = &policy.Status.Ancestors[len(policy.Status.Ancestors)-1]
	}

	meta.SetStatusCondition(&ancestor.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: policy.Generation,
	})

	_ = r.Client.Status().Update(ctx, policy)
}

func authPolicyMutator(existingObj, desiredObj client.Object) (bool, error) {
	var update bool
	existing, ok := existingObj.(*kuadrantv1.AuthPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *kuadrantv1.AuthPolicy", existingObj)
	}
	desired, ok := desiredObj.(*kuadrantv1.AuthPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *kuadrantv1.AuthPolicy", desiredObj)
	}
	if !reflect.DeepEqual(desired.Spec.TargetRef, existing.Spec.TargetRef) {
		existing.Spec.TargetRef = desired.Spec.TargetRef
		update = true
	}
	if !reflect.DeepEqual(desired.Spec.AuthScheme, existing.Spec.AuthScheme) {
		existing.Spec.AuthScheme = desired.Spec.AuthScheme
		update = true
	}
	return update, nil
}
