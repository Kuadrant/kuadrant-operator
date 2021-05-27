/*
 Copyright 2021 Red Hat, Inc.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package istioprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"istio.io/api/networking/v1alpha3"
	v1beta12 "istio.io/api/security/v1beta1"
	v1beta13 "istio.io/api/type/v1beta1"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioSecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//TODO: move the const to a proper place, or get it from config
const (
	KuadrantNamespace             = "kuadrant-system"
	KuadrantAuthorizationProvider = "kuadrant-authorization"
)

type IstioProvider struct {
	Log       logr.Logger
	K8sClient client.Client
}

// +kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch

func New(logger logr.Logger, client client.Client) *IstioProvider {
	// Register the Istio Scheme into the client, so we can interact with istio objects.
	utilruntime.Must(istio.AddToScheme(client.Scheme()))
	utilruntime.Must(istioSecurity.AddToScheme(client.Scheme()))

	// TODO: Create the gateway for Kuadrant
	// TODO: Add the proper config to the mesh for the extAuthz.

	return &IstioProvider{
		Log:       logger,
		K8sClient: client,
	}
}

func (is *IstioProvider) Create(ctx context.Context, apip v1beta1.APIProduct) error {
	log := is.Log.WithValues("apiproduct", client.ObjectKeyFromObject(&apip))
	log.V(1).Info("Istio Provider Create")

	virtualServices, err := is.toVirtualServices(ctx, apip)
	if err != nil {
		return err
	}

	for _, virtualService := range virtualServices {
		err = is.K8sClient.Create(ctx, virtualService)
		if err != nil {
			return fmt.Errorf("failing to create Istio virtualservice for %s: %w", virtualService.GetName(), err)
		}
		err = is.K8sClient.Get(ctx, client.ObjectKeyFromObject(virtualService), virtualService)
		if err != nil {
			return err
		}
		authPolicy := getAuthorizationPolicy(*virtualService)
		err = is.K8sClient.Create(ctx, &authPolicy)
		if err != nil {
			return fmt.Errorf("failing to create Istio AuthorizationPolicy for %s: %w",
				virtualService.GetName(), err)
		}
	}

	return nil
}

func getAuthorizationPolicy(virtualService istio.VirtualService) istioSecurity.AuthorizationPolicy {
	var policyHosts []string
	for _, host := range virtualService.Spec.Hosts {
		if !strings.Contains(host, ":") {
			host = host + ":*"
		}
		policyHosts = append(policyHosts, host)
	}
	authPolicy := istioSecurity.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      virtualService.GetName(),
			Namespace: KuadrantNamespace,
		},
		Spec: v1beta12.AuthorizationPolicy{
			Selector: &v1beta13.WorkloadSelector{
				MatchLabels: map[string]string{
					"app": "istio-ingressgateway",
				},
			},
			Rules: []*v1beta12.Rule{
				{
					To: []*v1beta12.Rule_To{{
						Operation: &v1beta12.Operation{
							Hosts: policyHosts,
						},
					}},
				},
			},
			Action: v1beta12.AuthorizationPolicy_CUSTOM,
			ActionDetail: &v1beta12.AuthorizationPolicy_Provider{
				Provider: &v1beta12.AuthorizationPolicy_ExtensionProvider{
					Name: KuadrantAuthorizationProvider,
				},
			},
		},
	}
	reference := getOwnerReference(virtualService)
	ownerReferences := authPolicy.GetOwnerReferences()
	ownerReferences = append(ownerReferences, reference)
	authPolicy.SetOwnerReferences(ownerReferences)

	return authPolicy
}

func getOwnerReference(virtualService istio.VirtualService) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: virtualService.APIVersion,
		Kind:       virtualService.Kind,
		Name:       virtualService.Name,
		UID:        virtualService.UID,
	}
}

// TODO(jmprusi): Pending refactor...
func (is *IstioProvider) toVirtualServices(ctx context.Context, apip v1beta1.APIProduct) ([]*istio.VirtualService, error) {
	virtualServices := make([]*istio.VirtualService, 0)
	for _, environment := range apip.Spec.Environments {
		httpRoutes := make([]*v1alpha3.HTTPRoute, 0)
		virtualService := istio.VirtualService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VirtualService",
				APIVersion: "networking.istio.io/v1alpha3",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      apip.Name + apip.Namespace + environment.Name,
				Namespace: KuadrantNamespace,
			},
			Spec: v1alpha3.VirtualService{
				Gateways: []string{"kuadrant-gateway"},
				Hosts:    environment.Hosts,
			},
		}
		for _, backendService := range environment.BackendServers {
			// Lets find the referenced API in the APIs List.
			found := -1
			for i, api := range apip.Spec.APIs {
				if api.Name == backendService.API {
					found = i
					break
				}
			}
			if found == -1 {
				return nil, fmt.Errorf("referenced API in backendService not found/part of this product. %s",
					backendService.API)
			}
			// Try to get the API object from k8s.
			api := v1beta1.API{}
			err := is.K8sClient.Get(ctx, types.NamespacedName{
				Namespace: apip.Namespace,
				Name:      apip.Spec.APIs[found].Name,
			}, &api)
			if err != nil {
				return nil, err
			}
			service := v1.Service{}
			err = is.K8sClient.Get(ctx, types.NamespacedName{Name: backendService.Destination.ServiceSelector.Name,
				Namespace: backendService.Destination.ServiceSelector.Namespace},
				&service)
			if err != nil {
				return nil, err
			}

			// TODO(jmprusi): Get the actual internal cluster hostname instead of hardcoding it.
			destination := v1alpha3.HTTPRouteDestination{
				Destination: &v1alpha3.Destination{
					Host: service.Name + "." + service.Namespace + ".svc.cluster.local",
					Port: &v1alpha3.PortSelector{
						Number: uint32(*backendService.Destination.ServiceSelector.Port),
					},
				},
			}
			// if we have been able to get the API object, lets compute the HTTRoutes
			for _, operation := range api.Spec.Operations {
				//TODO(jmprusi): Right now we are ignoring the security field of the operation, we should review this.
				matchPath := ""
				httpRoute := v1alpha3.HTTPRoute{
					Name: operation.Name,
					// Here we are rewriting the auhtority of the request to one of the hosts in the API definition.
					// TODO(jmprusi): Is this something expected? should we allow for a host override?
					Rewrite: &v1alpha3.HTTPRewrite{
						Authority: api.Spec.Hosts[0],
					},
				}
				// Handle Prefix Override.
				if apip.Spec.APIs[found].PrefixOverride != "" {
					// If there's an Override, lets append it to the actual Operation Path.
					matchPath = apip.Spec.APIs[found].PrefixOverride + operation.Path
					// We need to rewrite the path, to match what the service expects, basically,
					// removing the prefixOverride
					httpRoute.Rewrite.Uri = operation.Path
				}

				httpRoute.Match = []*v1alpha3.HTTPMatchRequest{
					{
						Uri: &v1alpha3.StringMatch{
							MatchType: &v1alpha3.StringMatch_Prefix{Prefix: matchPath},
						},
						Method: &v1alpha3.StringMatch{
							MatchType: &v1alpha3.StringMatch_Exact{Exact: operation.Method},
						},
					},
				}
				httpRoute.Route = []*v1alpha3.HTTPRouteDestination{&destination}
				httpRoutes = append(httpRoutes, &httpRoute)
			}
		}
		virtualService.Spec.Http = httpRoutes
		virtualServices = append(virtualServices, &virtualService)
	}
	return virtualServices, nil
}

func (is *IstioProvider) Update(ctx context.Context, apip v1beta1.APIProduct) error {
	log := is.Log.WithValues("apiproduct", client.ObjectKeyFromObject(&apip))
	log.V(1).Info("Istio Provider Update")

	virtualServices, err := is.toVirtualServices(ctx, apip)
	if err != nil {
		return err
	}

	for _, virtualService := range virtualServices {
		currentVS := istio.VirtualService{}
		err = is.K8sClient.Get(ctx, client.ObjectKeyFromObject(virtualService), &currentVS)
		if err != nil {
			return err
		}

		virtualService.ResourceVersion = currentVS.ResourceVersion
		err = is.K8sClient.Update(ctx, virtualService)
		if err != nil {
			return fmt.Errorf("failing to update Istio virtualservice for %s: %w", virtualService.GetName(), err)
		}

		currentAuthPolicy := istioSecurity.AuthorizationPolicy{}
		authPolicy := getAuthorizationPolicy(*virtualService)
		// TODO(jmprusi): Handle the case where the AuthPolicy doesn't exist.
		err = is.K8sClient.Get(ctx, client.ObjectKeyFromObject(&authPolicy), &currentAuthPolicy)
		if err != nil {
			return fmt.Errorf("failing to update Istio AuthorizationPolicy for %s: %w",
				authPolicy.GetName(), err)
		}
		authPolicy.ResourceVersion = currentAuthPolicy.ResourceVersion
		err = is.K8sClient.Update(ctx, &authPolicy)
		if err != nil {
			return fmt.Errorf("failing to update Istio AuthorizationPolicy for %s: %w",
				authPolicy.GetName(), err)
		}
	}
	return nil
}

func (is *IstioProvider) Status(ctx context.Context, apip v1beta1.APIProduct) (bool, error) {

	return true, nil
}

func (is *IstioProvider) Delete(ctx context.Context, apip v1beta1.APIProduct) error {
	log := is.Log.WithValues("apiproduct", client.ObjectKeyFromObject(&apip))
	log.V(1).Info("Istio Provider Delete")

	for _, environment := range apip.Spec.Environments {
		virtualService := istio.VirtualService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apip.Name + apip.Namespace + environment.Name,
				Namespace: KuadrantNamespace,
			},
		}
		log.Info("Istio Provider Delete", "virtualservice", virtualService.GetName())
		is.K8sClient.Delete(ctx, &virtualService)
	}

	return nil
}
