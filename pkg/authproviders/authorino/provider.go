package authorino

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	authorino "github.com/kuadrant/authorino/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

type Provider struct {
	*reconcilers.BaseReconciler
}

// +kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;patch;delete

func New(baseReconciler *reconcilers.BaseReconciler) *Provider {
	utilruntime.Must(authorino.AddToScheme(baseReconciler.Scheme()))

	return &Provider{BaseReconciler: baseReconciler}
}

func (a *Provider) Reconcile(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	logger := logr.FromContext(ctx).WithName("authprovider").WithName("authorino")
	logger.V(1).Info("Reconcile")

	authConfig := buildAuthConfig(apip)

	err := a.ReconcileAuthorinoAuthConfig(ctx, authConfig, authorinoAuthConfigBasicMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (a *Provider) ReconcileAuthorinoAuthConfig(ctx context.Context, desired *authorino.AuthConfig, mutatefn reconcilers.MutateFn) error {
	return a.ReconcileResource(ctx, &authorino.AuthConfig{}, desired, mutatefn)
}

func buildAuthConfig(apip *networkingv1beta1.APIProduct) *authorino.AuthConfig {
	authConfig := &authorino.AuthConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apip.Name + apip.Namespace,
			Namespace: common.KuadrantNamespace,
		},
		Spec: authorino.AuthConfigSpec{
			Hosts:         apip.Spec.Hosts,
			Identity:      nil,
			Metadata:      nil,
			Authorization: nil,
			Response:      nil,
		},
	}

	if !apip.HasSecurity() {
		common.TagObjectToDelete(authConfig)
		return authConfig
	}

	for _, securityScheme := range apip.Spec.SecurityScheme {
		identity := authorino.Identity{
			Name:           securityScheme.Name,
			Credentials:    authorino.Credentials{},
			OAuth2:         nil,
			Oidc:           nil,
			APIKey:         nil,
			KubernetesAuth: nil,
		}
		if securityScheme.APIKeyAuth != nil {
			apikey := authorino.Identity_APIKey{
				LabelSelectors: securityScheme.APIKeyAuth.CredentialSource.LabelSelectors,
			}
			identity.Credentials.In = authorino.Credentials_In(securityScheme.APIKeyAuth.Location)
			identity.Credentials.KeySelector = securityScheme.APIKeyAuth.Name
			identity.APIKey = &apikey
		} else if securityScheme.OpenIDConnectAuth != nil {
			identity.Oidc = &authorino.Identity_OidcConfig{
				Endpoint: securityScheme.OpenIDConnectAuth.URL,
			}
		}
		authConfig.Spec.Identity = append(authConfig.Spec.Identity, &identity)
	}

	// Response
	if apip.AuthRateLimit() != nil && apip.HasAPIKeyAuth() {
		response := &authorino.Response{
			Name:       "rate-limit-apikey",
			Wrapper:    authorino.Response_Wrapper("envoyDynamicMetadata"),
			WrapperKey: "ext_auth_data",
			JSON: &authorino.Response_DynamicJSON{
				Properties: []authorino.JsonProperty{
					{
						Name: "user-id",
						ValueFrom: authorino.ValueFromAuthJSON{
							AuthJSON: `auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id`,
						},
					},
				},
			},
		}

		authConfig.Spec.Response = append(authConfig.Spec.Response, response)
	}

	if apip.AuthRateLimit() != nil && apip.HasOIDCAuth() {
		response := &authorino.Response{
			Name:       "rate-limit-oidc",
			Wrapper:    authorino.Response_Wrapper("envoyDynamicMetadata"),
			WrapperKey: "ext_auth_data",
			JSON: &authorino.Response_DynamicJSON{
				Properties: []authorino.JsonProperty{
					{
						Name: "user-id",
						ValueFrom: authorino.ValueFromAuthJSON{
							AuthJSON: `auth.identity.sub`,
						},
					},
				},
			},
		}

		authConfig.Spec.Response = append(authConfig.Spec.Response, response)
	}

	return authConfig
}

func (a *Provider) Delete(ctx context.Context, apip *networkingv1beta1.APIProduct) (err error) {
	log := a.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	log.V(1).Info("Delete")
	return nil
}

func (a *Provider) Status(ctx context.Context, apip *networkingv1beta1.APIProduct) (bool, error) {
	log := a.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	log.V(1).Info("Status")

	// Right now, we just try to get all the objects that should have been created, and check their status.
	// If any object is missing/not-created, Status returns false.
	authConfig := buildAuthConfig(apip)

	if !common.IsObjectTaggedToDelete(authConfig) {
		existingAuthConfig := &authorino.AuthConfig{}
		err := a.Client().Get(ctx, client.ObjectKeyFromObject(authConfig), existingAuthConfig)
		if err != nil && errors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		// TODO(jmprusi): No status in authorino serviceConfig objects, yet.
		//if !existingServiceConfig.Status.Ready {
		//	return false, nil
		//}
	}

	return true, nil
}

func authorinoAuthConfigBasicMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*authorino.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not a *authorino.AuthConfig", existingObj)
	}
	desired, ok := desiredObj.(*authorino.AuthConfig)
	if !ok {
		return false, fmt.Errorf("%T is not a *authorino.AuthConfig", desiredObj)
	}

	updated := false
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		updated = true
	}

	return updated, nil
}
