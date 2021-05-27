package authorino

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders/istioprovider"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	authorino "github.com/kuadrant/authorino/api/v1beta1"
	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AuthorinoProvider struct {
	Log       logr.Logger
	K8sClient client.Client
}

// +kubebuilder:rbac:groups=config.authorino.3scale.net,resources=services,verbs=get;list;watch;create;update;patch;delete

func New(logger logr.Logger, client client.Client) *AuthorinoProvider {
	utilruntime.Must(authorino.AddToScheme(client.Scheme()))

	return &AuthorinoProvider{
		Log:       logger,
		K8sClient: client,
	}
}
func (a *AuthorinoProvider) Create(ctx context.Context, apip v1beta1.APIProduct) (err error) {

	serviceConfigs, err := APIProductToServiceConfigs(apip)
	if err != nil {
		return err
	}

	for _, serviceConfig := range serviceConfigs {
		err = a.K8sClient.Create(ctx, &serviceConfig)
		if err != nil {
			return err
		}
	}

	return nil
}

func APIProductToServiceConfigs(apip v1beta1.APIProduct) ([]authorino.Service, error) {
	serviceConfigs := make([]authorino.Service, 0)
	for _, environment := range apip.Spec.Environments {
		service := authorino.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apip.Name + apip.Namespace + environment.Name,
				Namespace: istioprovider.KuadrantNamespace,
			},
			Spec: authorino.ServiceSpec{
				Hosts:         environment.Hosts,
				Identity:      nil,
				Metadata:      nil,
				Authorization: nil,
			},
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
				apikey := authorino.Identity_APIKey{}
				found := false
				for _, credentialSource := range environment.CredentialSources {
					if credentialSource.Name == securityScheme.Name {
						apikey.LabelSelectors = make(map[string]string)
						for k, v := range credentialSource.APIKeyAuth.LabelSelectors {
							apikey.LabelSelectors[k] = v
						}
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("securityScheme credential source not found")
				}
				identity.Credentials.In = authorino.Credentials_In(securityScheme.APIKeyAuth.Location)
				identity.Credentials.KeySelector = securityScheme.APIKeyAuth.Name
				identity.APIKey = &apikey
			} else if securityScheme.OpenIDConnectAuth != nil {
				//TODO(jmprusi): Implement
			}
			service.Spec.Identity = append(service.Spec.Identity, &identity)
		}
		serviceConfigs = append(serviceConfigs, service)
	}
	return serviceConfigs, nil
}

func (a *AuthorinoProvider) Update(ctx context.Context, apip v1beta1.APIProduct) (err error) {
	return nil
}

func (a *AuthorinoProvider) Delete(ctx context.Context, apip v1beta1.APIProduct) (err error) {
	return nil
}

func (a *AuthorinoProvider) Status(ctx context.Context, apip v1beta1.APIProduct) (ready bool, err error) {
	// Right now, we just try to get all the objects that should have been created, and check their status.
	// If any object is missing/not-created, Status returns false.
	serviceConfigs, err := APIProductToServiceConfigs(apip)
	if err != nil {
		return false, err
	}

	for _, serviceConfig := range serviceConfigs {
		existingServiceConfig := authorino.Service{}
		err = a.K8sClient.Get(ctx, client.ObjectKeyFromObject(&serviceConfig), &existingServiceConfig)
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
