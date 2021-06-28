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
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-logr/logr"
	"istio.io/api/networking/v1alpha3"
	v1beta12 "istio.io/api/security/v1beta1"
	v1beta13 "istio.io/api/type/v1beta1"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioSecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

//TODO: move the const to a proper place, or get it from config
const (
	KuadrantNamespace             = "kuadrant-system"
	KuadrantAuthorizationProvider = "kuadrant-authorization"
)

type IstioProvider struct {
	*reconcilers.BaseReconciler
	logger logr.Logger
}

// +kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch

func New(baseReconciler *reconcilers.BaseReconciler) *IstioProvider {
	// Register the Istio Scheme into the client, so we can interact with istio objects.
	utilruntime.Must(istio.AddToScheme(baseReconciler.Scheme()))
	utilruntime.Must(istioSecurity.AddToScheme(baseReconciler.Scheme()))

	// TODO: Create the gateway for Kuadrant
	// TODO: Add the proper config to the mesh for the extAuthz.

	return &IstioProvider{
		BaseReconciler: baseReconciler,
		logger:         ctrl.Log.WithName("kuadrant").WithName("ingressprovider").WithName("istio"),
	}
}

func (is *IstioProvider) Logger() logr.Logger {
	return is.logger
}

func (is *IstioProvider) Reconcile(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	log := is.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	log.V(1).Info("Reconcile")

	virtualService, err := is.toVirtualServices(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = is.ReconcileIstioVirtualService(ctx, virtualService, alwaysUpdateVirtualService)
	if err != nil {
		return ctrl.Result{}, err
	}

	existingVS := &istio.VirtualService{}
	err = is.GetResource(ctx, client.ObjectKeyFromObject(virtualService), existingVS)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	authPolicy := getAuthorizationPolicy(existingVS)
	err = is.ReconcileIstioAuthorizationPolicy(ctx, authPolicy, alwaysUpdateAuthPolicy)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (is *IstioProvider) ReconcileIstioVirtualService(ctx context.Context, desired *istio.VirtualService, mutatefn reconcilers.MutateFn) error {
	return is.ReconcileResource(ctx, &istio.VirtualService{}, desired, mutatefn)
}

func (is *IstioProvider) ReconcileIstioAuthorizationPolicy(ctx context.Context, desired *istioSecurity.AuthorizationPolicy, mutatefn reconcilers.MutateFn) error {
	return is.ReconcileResource(ctx, &istioSecurity.AuthorizationPolicy{}, desired, mutatefn)
}

func getAuthorizationPolicy(virtualService *istio.VirtualService) *istioSecurity.AuthorizationPolicy {
	var policyHosts []string

	// Now we need to generate the list of hosts that will match the authorization policy,
	// to be sure, we will match the "$host" and "$host:*".
	for _, host := range virtualService.Spec.Hosts {
		//TODO(jmprusi): avoid duplicates
		hostSplitted := strings.Split(host, ":")
		policyHosts = append(policyHosts, hostSplitted[0]+":*")
		policyHosts = append(policyHosts, hostSplitted[0])
	}
	authPolicy := &istioSecurity.AuthorizationPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthorizationPolicy",
			APIVersion: "security.istio.io/v1beta1",
		},
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
	owner := getOwnerReference(virtualService)
	authPolicy.SetOwnerReferences(append(authPolicy.GetOwnerReferences(), owner))

	return authPolicy
}

func getOwnerReference(virtualService *istio.VirtualService) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: virtualService.APIVersion,
		Kind:       virtualService.Kind,
		Name:       virtualService.Name,
		UID:        virtualService.UID,
	}
}

// TODO(jmprusi): Pending refactor...
func (is *IstioProvider) toVirtualServices(ctx context.Context, apip *networkingv1beta1.APIProduct) (*istio.VirtualService, error) {
	httpRoutes := make([]*v1alpha3.HTTPRoute, 0)
	virtualService := istio.VirtualService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VirtualService",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apip.Name + apip.Namespace,
			Namespace: KuadrantNamespace,
		},
		Spec: v1alpha3.VirtualService{
			Gateways: []string{"kuadrant-gateway"},
			Hosts:    apip.Spec.Routing.Hosts,
		},
	}
	for _, apiSel := range apip.Spec.APIs {
		// Try to get the API object from k8s.
		api := &networkingv1beta1.API{}
		err := is.Client().Get(ctx, types.NamespacedName{
			Namespace: apiSel.Namespace,
			Name:      apiSel.Name,
		}, api)
		if err != nil {
			return nil, err
		}

		destination := &networkingv1beta1.Destination{}
		// TODO(jmprusi): improve by making a map and pushing this logic to the API def
		for i := range api.Spec.TAGs {
			if api.Spec.TAGs[i].Name == apiSel.Tag {
				destination = &api.Spec.TAGs[i].Destination
				break
			}
		}
		if destination == nil {
			return nil, fmt.Errorf("tag not found in target API")
		}

		service := v1.Service{}
		err = is.Client().Get(ctx, types.NamespacedName{Name: destination.Name,
			Namespace: destination.Namespace},
			&service)
		if err != nil {
			return nil, err
		}

		// TODO(jmprusi): Get the actual internal cluster hostname instead of hardcoding it.
		istioDestination := v1alpha3.HTTPRouteDestination{
			Destination: &v1alpha3.Destination{
				Host: service.Name + "." + service.Namespace + ".svc.cluster.local",
				Port: &v1alpha3.PortSelector{
					Number: uint32(*destination.Port),
				},
			},
		}

		// TODO(jmprusi): All the OAS logic should be encapsulated into the API type.
		wantedTag := networkingv1beta1.Tag{}
		found := false
		for _, tagInfo := range api.Spec.TAGs {
			if tagInfo.Name == apiSel.Tag {
				wantedTag = tagInfo
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("tag %s not found", apiSel.Tag)
		}

		loader := openapi3.Loader{Context: ctx}
		doc, err := loader.LoadFromData([]byte(wantedTag.APIDefinition.OAS))
		if err != nil {
			return nil, err
		}

		// TODO(jmprusi): Getting one of the hosts from the OpenAPISpec... extract this logic and improve.
		serverURL := doc.Servers[0].URL
		u, err := url.Parse(serverURL)
		if err != nil {
			return nil, err
		}
		apiHost := u.Host

		for path, pathItem := range doc.Paths {
			for opVerb, operation := range pathItem.Operations() {
				//TODO(jmprusi): Right now we are ignoring the security field of the operation, we should review this.
				matchPath := path
				httpRoute := v1alpha3.HTTPRoute{
					Name: operation.OperationID,
					// Here we are rewriting the auhtority of the request to one of the hosts in the API definition.
					// TODO(jmprusi): Is this something expected? should we allow for a host override?
					Rewrite: &v1alpha3.HTTPRewrite{
						Authority: apiHost,
					},
				}
				// Handle Prefix Override.
				if apiSel.Mapping.Prefix != "" {
					// If there's an Override, lets append it to the actual Operation Path.
					matchPath = apiSel.Mapping.Prefix + path
					// We need to rewrite the path, to match what the service expects, basically,
					// removing the prefixOverride
					httpRoute.Rewrite.Uri = path
				}

				httpRoute.Match = []*v1alpha3.HTTPMatchRequest{
					{
						Uri: &v1alpha3.StringMatch{
							MatchType: &v1alpha3.StringMatch_Prefix{Prefix: matchPath},
						},
						Method: &v1alpha3.StringMatch{
							MatchType: &v1alpha3.StringMatch_Exact{Exact: opVerb},
						},
					},
				}
				httpRoute.Route = []*v1alpha3.HTTPRouteDestination{&istioDestination}
				httpRoutes = append(httpRoutes, &httpRoute)

			}
		}
	}
	virtualService.Spec.Http = httpRoutes
	return &virtualService, nil
}

func (is *IstioProvider) Status(ctx context.Context, apip *networkingv1beta1.APIProduct) (bool, error) {
	log := is.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	log.V(1).Info("Status")
	return true, nil
}

func (is *IstioProvider) Delete(ctx context.Context, apip *networkingv1beta1.APIProduct) error {
	log := is.Logger().WithValues("apiproduct", client.ObjectKeyFromObject(apip))
	log.V(1).Info("Delete")

	virtualService := &istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apip.Name + apip.Namespace,
			Namespace: KuadrantNamespace,
		},
	}
	is.DeleteResource(ctx, virtualService)

	return nil
}

func alwaysUpdateVirtualService(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istio.VirtualService)
	if !ok {
		return false, fmt.Errorf("%T is not a *istio.VirtualService", existingObj)
	}
	desired, ok := desiredObj.(*istio.VirtualService)
	if !ok {
		return false, fmt.Errorf("%T is not a *istio.VirtualService", desiredObj)
	}

	existing.Spec = desired.Spec
	return true, nil
}

func alwaysUpdateAuthPolicy(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istioSecurity.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioSecurity.AuthorizationPolicy", existingObj)
	}
	desired, ok := desiredObj.(*istioSecurity.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioSecurity.AuthorizationPolicy", desiredObj)
	}

	existing.Spec = desired.Spec
	return true, nil
}
