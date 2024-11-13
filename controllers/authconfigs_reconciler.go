package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantauthorino "github.com/kuadrant/kuadrant-operator/pkg/authorino"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/pkg/policymachinery"
	"github.com/kuadrant/kuadrant-operator/pkg/utils"
)

//+kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;patch;delete

type AuthConfigsReconciler struct {
	client *dynamic.DynamicClient
}

// AuthConfigsReconciler subscribes to events with potential to change Authorino AuthConfig custom resources
func (r *AuthConfigsReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantauthorino.AuthConfigGroupKind},
		},
	}
}

func (r *AuthConfigsReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("AuthConfigsReconciler")

	authorino := GetAuthorinoFromTopology(topology)
	if authorino == nil {
		logger.V(1).Info("authorino resource not found in the topology")
		return nil
	}
	authConfigsNamespace := authorino.GetNamespace()

	effectivePolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		logger.Error(ErrMissingStateEffectiveAuthPolicies, "failed to reconcile authconfig objects")
		return nil
	}
	effectivePoliciesMap := effectivePolicies.(EffectiveAuthPolicies)

	logger.V(1).Info("reconciling authconfig objects", "effectivePolicies", len(effectivePoliciesMap))
	defer logger.V(1).Info("finished reconciling authconfig objects")

	desiredAuthConfigs := make(map[k8stypes.NamespacedName]struct{})
	modifiedAuthConfigs := []string{}

	for pathID, effectivePolicy := range effectivePoliciesMap {
		_, _, _, httpRoute, httpRouteRule, err := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
		if err != nil {
			logger.Error(err, "failed to reconcile authconfig objects", "path", effectivePolicy.Path)
			continue
		}
		httpRouteKey := k8stypes.NamespacedName{Name: httpRoute.GetName(), Namespace: httpRoute.GetNamespace()}
		httpRouteRuleKey := httpRouteRule.Name

		authConfigName := AuthConfigNameForPath(pathID)
		desiredAuthConfig := r.buildDesiredAuthConfig(effectivePolicy, authConfigName, authConfigsNamespace)
		desiredAuthConfigs[k8stypes.NamespacedName{Name: desiredAuthConfig.GetName(), Namespace: desiredAuthConfig.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantauthorino.AuthConfigsResource).Namespace(desiredAuthConfig.GetNamespace())

		existingAuthConfigObj, found := lo.Find(topology.Objects().Children(httpRouteRule), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantauthorino.AuthConfigGroupKind && child.GetName() == authConfigName && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredAuthConfig.GetLabels()))
		})

		// create
		if !found {
			modifiedAuthConfigs = append(modifiedAuthConfigs, authConfigName)
			desiredAuthConfigUnstructured, err := controller.Destruct(desiredAuthConfig)
			if err != nil {
				logger.Error(err, "failed to destruct authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", desiredAuthConfig)
				continue
			}

			if _, err = resource.Create(ctx, desiredAuthConfigUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", desiredAuthConfigUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingAuthConfig := existingAuthConfigObj.(*controller.RuntimeObject).Object.(*authorinov1beta3.AuthConfig)

		if equalAuthConfigs(existingAuthConfig, desiredAuthConfig) {
			logger.V(1).Info("authconfig object is up to date, nothing to do")
			continue
		}

		modifiedAuthConfigs = append(modifiedAuthConfigs, authConfigName)

		// delete
		if utils.IsObjectTaggedToDelete(desiredAuthConfig) && !utils.IsObjectTaggedToDelete(existingAuthConfig) {
			if err := resource.Delete(ctx, existingAuthConfig.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete wasmplugin object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", fmt.Sprintf("%s/%s", existingAuthConfig.GetNamespace(), existingAuthConfig.GetName()))
				// TODO: handle error
			}
			continue
		}

		// update
		existingAuthConfig.Spec = desiredAuthConfig.Spec

		existingAuthConfigUnstructured, err := controller.Destruct(existingAuthConfig)
		if err != nil {
			logger.Error(err, "failed to destruct authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", existingAuthConfig)
			continue
		}
		if _, err = resource.Update(ctx, existingAuthConfigUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update authconfig object", "httpRoute", httpRouteKey.String(), "httpRouteRule", httpRouteRuleKey, "authconfig", existingAuthConfigUnstructured.Object)
			// TODO: handle error
		}
	}

	if len(modifiedAuthConfigs) > 0 {
		state.Store(StateModifiedAuthConfigs, modifiedAuthConfigs)
	}

	// cleanup authconfigs that are not in the effective policies
	staleAuthConfigs := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredAuthConfigs[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantauthorino.AuthConfigGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(AuthObjectLabels()) && !desired
	})
	for _, authConfig := range staleAuthConfigs {
		if err := r.client.Resource(kuadrantauthorino.AuthConfigsResource).Namespace(authConfig.GetNamespace()).Delete(ctx, authConfig.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete authconfig object", "authconfig", fmt.Sprintf("%s/%s", authConfig.GetNamespace(), authConfig.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *AuthConfigsReconciler) buildDesiredAuthConfig(effectivePolicy EffectiveAuthPolicy, name, namespace string) *authorinov1beta3.AuthConfig {
	_, _, _, _, httpRouteRule, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)

	authConfig := &authorinov1beta3.AuthConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthConfig",
			APIVersion: authorinov1beta3.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation: httpRouteRule.GetLocator(),
			},
			Labels: AuthObjectLabels(),
		},
		Spec: authorinov1beta3.AuthConfigSpec{
			Hosts: []string{name},
		},
	}

	spec := effectivePolicy.Spec.Spec.Proper()

	// named patterns
	if namedPatterns := spec.NamedPatterns; namedPatterns != nil {
		authConfig.Spec.NamedPatterns = lo.MapValues(spec.NamedPatterns, func(v kuadrantv1.MergeablePatternExpressions, _ string) authorinov1beta3.PatternExpressions {
			return v.PatternExpressions
		})
	}

	// return early if authScheme is nil
	authScheme := spec.AuthScheme
	if authScheme == nil {
		return authConfig
	}

	// authentication
	if authentication := authScheme.Authentication; authentication != nil {
		authConfig.Spec.Authentication = lo.MapValues(authentication, func(v kuadrantv1.MergeableAuthenticationSpec, _ string) authorinov1beta3.AuthenticationSpec {
			return v.AuthenticationSpec
		})
	}

	// metadata
	if metadata := authScheme.Metadata; metadata != nil {
		authConfig.Spec.Metadata = lo.MapValues(metadata, func(v kuadrantv1.MergeableMetadataSpec, _ string) authorinov1beta3.MetadataSpec {
			return v.MetadataSpec
		})
	}

	// authorization
	if authorization := authScheme.Authorization; authorization != nil {
		authConfig.Spec.Authorization = lo.MapValues(authorization, func(v kuadrantv1.MergeableAuthorizationSpec, _ string) authorinov1beta3.AuthorizationSpec {
			return v.AuthorizationSpec
		})
	}

	// response
	if response := authScheme.Response; response != nil {
		var unauthenticated *authorinov1beta3.DenyWithSpec
		if response.Unauthenticated != nil {
			unauthenticated = &response.Unauthenticated.DenyWithSpec
		}

		var unauthorized *authorinov1beta3.DenyWithSpec
		if response.Unauthorized != nil {
			unauthorized = &response.Unauthorized.DenyWithSpec
		}

		authConfig.Spec.Response = &authorinov1beta3.ResponseSpec{
			Unauthenticated: unauthenticated,
			Unauthorized:    unauthorized,
			Success: authorinov1beta3.WrappedSuccessResponseSpec{
				Headers: authorinoSpecsFromConfigs(response.Success.Headers, func(config kuadrantv1.MergeableHeaderSuccessResponseSpec) authorinov1beta3.HeaderSuccessResponseSpec {
					return authorinov1beta3.HeaderSuccessResponseSpec{SuccessResponseSpec: config.SuccessResponseSpec}
				}),
				DynamicMetadata: authorinoSpecsFromConfigs(response.Success.DynamicMetadata, func(config kuadrantv1.MergeableSuccessResponseSpec) authorinov1beta3.SuccessResponseSpec {
					return config.SuccessResponseSpec
				}),
			},
		}
	}

	// callbacks
	if callbacks := authScheme.Callbacks; callbacks != nil {
		authConfig.Spec.Callbacks = lo.MapValues(callbacks, func(v kuadrantv1.MergeableCallbackSpec, _ string) authorinov1beta3.CallbackSpec {
			return v.CallbackSpec
		})
	}

	return authConfig
}

func authorinoSpecsFromConfigs[T, U any](configs map[string]U, extractAuthorinoSpec func(U) T) map[string]T {
	specs := make(map[string]T, len(configs))
	for name, config := range configs {
		authorinoConfig := extractAuthorinoSpec(config)
		specs[name] = authorinoConfig
	}

	if len(specs) == 0 {
		return nil
	}

	return specs
}

func equalAuthConfigs(existing, desired *authorinov1beta3.AuthConfig) bool {
	// httprouterule back ref annotation
	existingAnnotations := existing.GetAnnotations()
	desiredAnnotations := desired.GetAnnotations()
	if existingAnnotations == nil || desiredAnnotations == nil || existingAnnotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation] != desiredAnnotations[kuadrantauthorino.AuthConfigHTTPRouteRuleAnnotation] {
		return false
	}

	// labels
	existingLabels := existing.GetLabels()
	desiredLabels := desired.GetLabels()
	if len(existingLabels) != len(desiredLabels) || !labels.Set(existingLabels).AsSelector().Matches(labels.Set(desiredLabels)) {
		return false
	}

	// spec
	return reflect.DeepEqual(existing.Spec, desired.Spec)
}
