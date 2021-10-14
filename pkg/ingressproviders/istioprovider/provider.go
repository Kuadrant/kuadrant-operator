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
	"istio.io/api/networking/v1alpha3"
	v1beta12 "istio.io/api/security/v1beta1"
	v1beta13 "istio.io/api/type/v1beta1"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioSecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

var (
	LimitadorServiceClusterHost = fmt.Sprintf("limitador.%s.svc.cluster.local", common.KuadrantNamespace)
	LimitadorServiceGrpcPort    = 8081
)

type IstioProvider struct {
	*reconcilers.BaseReconciler
	logger logr.Logger
}

// +kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

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

	virtualService, err := is.virtualServiceFromAPIProduct(ctx, apip)
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

	authPolicy, err := is.getAuthorizationPolicy(existingVS)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = is.ReconcileIstioAuthorizationPolicy(ctx, authPolicy, alwaysUpdateAuthPolicy)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = is.reconcileRateLimit(ctx, apip)
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

func (is *IstioProvider) getAuthorizationPolicy(virtualService *istio.VirtualService) (*istioSecurity.AuthorizationPolicy, error) {
	policyHosts := []string{}

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
			Namespace: common.KuadrantNamespace,
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
					Name: common.KuadrantAuthorizationProvider,
				},
			},
		},
	}

	// Add "controlled" owner reference. Prevents other controller to adopt this resource
	err := controllerutil.SetControllerReference(virtualService, authPolicy, is.Scheme())

	return authPolicy, err
}

func (is *IstioProvider) virtualServiceFromAPIProduct(ctx context.Context, apip *networkingv1beta1.APIProduct) (*istio.VirtualService, error) {
	httpRoutes := []*v1alpha3.HTTPRoute{}
	for idx := range apip.Spec.APIs {
		apiHTTPRoutes, err := is.apiHTTPRoutes(ctx, &apip.Spec.APIs[idx])
		if err != nil {
			return nil, err
		}
		httpRoutes = append(httpRoutes, apiHTTPRoutes...)
	}

	factory := VirtualServiceFactory{
		ObjectName: apip.Name + apip.Namespace,
		Namespace:  common.KuadrantNamespace,
		Hosts:      apip.Spec.Hosts,
		HTTPRoutes: httpRoutes,
	}

	return factory.VirtualService(), nil
}

func (is *IstioProvider) apiHTTPRoutes(ctx context.Context, apiRef *networkingv1beta1.APIReference) ([]*v1alpha3.HTTPRoute, error) {
	api := &networkingv1beta1.API{}
	err := is.Client().Get(ctx, apiRef.APINamespacedName(), api)
	if err != nil {
		return nil, err
	}

	if api.Spec.Mappings.OAS != nil {
		return HTTPRoutesFromOAS(*api.Spec.Mappings.OAS, apiRef.Prefix, api.Spec.Destination)
	}

	return HTTPRoutesFromPath(api.Spec.Mappings.HTTPPathMatch, apiRef.Prefix, api.Spec.Destination)
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
			Namespace: common.KuadrantNamespace,
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
