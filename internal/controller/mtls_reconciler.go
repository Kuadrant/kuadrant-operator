package controllers

import (
	"context"
	"fmt"
	"sync"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	securityv1beta1 "istio.io/api/security/v1"
	istiosecurity "istio.io/client-go/pkg/apis/security/v1"
	v12 "k8s.io/api/apps/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
)

type PeerAuthentication istiosecurity.PeerAuthentication

func (p *PeerAuthentication) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

type MTLSReconciler struct {
	*reconcilers.BaseReconciler

	Client *dynamic.DynamicClient
}

func NewMTLSReconciler(mgr controllerruntime.Manager, client *dynamic.DynamicClient) *MTLSReconciler {
	return &MTLSReconciler{
		Client: client,
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
	}
}

func (r *MTLSReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Run, Events: []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
		{Kind: ptr.To(kuadrantv1.RateLimitPolicyGroupKind)},
		{Kind: ptr.To(kuadrantv1.AuthPolicyGroupKind)},
		{Kind: ptr.To(machinery.HTTPRouteGroupKind)},
		{Kind: ptr.To(machinery.GatewayGroupKind)},
		{Kind: ptr.To(istio.PeerAuthenticationGroupKind)},
	},
	}
}

//+kubebuilder:rbac:groups=security.istio.io,resources=peerauthentications,verbs=get;list;watch;create;update;patch;delete

func (r *MTLSReconciler) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {

	logger := controller.LoggerFromContext(eventCtx).WithName("MTLSReconciler")
	logger.V(1).Info("reconciling mtls", "status", "started")
	defer logger.V(1).Info("reconciling mtls", "status", "completed")
	targetables := topology.Targetables()
	gateways := targetables.Items(func(o machinery.Object) bool {
		gateway, ok := o.(*machinery.Gateway)
		return ok && gateway.Spec.GatewayClassName == "istio"
	})

	httpRouteRules := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.HTTPRouteRule)
		return ok
	})
	anyAttachedAuthorRateLimitPolicy := false
	policies := make([]machinery.Policy, 0)
outer:
	for _, gateway := range gateways {
		for _, httpRouteRule := range httpRouteRules {
			paths := targetables.Paths(gateway, httpRouteRule)
			for _, path := range paths {
				policies = kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
					if _, ok := policy.(*kuadrantv1.AuthPolicy); ok {
						return true
					}
					if _, ok := policy.(*kuadrantv1.RateLimitPolicy); ok {
						return true
					}
					return false
				})
				if len(policies) > 0 {
					anyAttachedAuthorRateLimitPolicy = true
					break outer
				}
			}
		}

	}

	// Check that a kuadrant resource exists, and mtls enabled, and that there is at least one RateLimit or Auth Policy attached.
	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil && kObj.Spec.MTLS == nil && (!kObj.Spec.MTLS.Enable || anyAttachedAuthorRateLimitPolicy == false) {
		defer logger.V(1).Info("mtls not enabled or applicable", "status", "completed")
		peerAuthentications := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
			if peerAuthentication, ok := item.(*PeerAuthentication); ok {
				if value, exists := peerAuthentication.Labels[kuadrantManagedLabelKey]; exists && value == "true" {
					return item, true
				}
			}
			return nil, false
		})
		r.deleteAllPeerAuthentications(eventCtx, peerAuthentications, logger)
	} else {
		// find an authorino object, then find and update the associated deployment
		aobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
			if item.GroupVersionKind().Kind == kuadrantv1beta1.AuthorinoGroupKind.Kind {
				return item, true
			}
			return nil, false
		})
		// add label to authorino deployment {"sidecar.istio.io/inject":"true"}}}}}
		aDeployment := &v12.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				// TODO can't be hardcoded, this is just one example
				Name:      "authorino",
				Namespace: aobjs[0].GetNamespace(),
			},
		}
		err := r.GetResource(eventCtx, types.NamespacedName{
			Namespace: aDeployment.Namespace,
			Name:      aDeployment.Name,
		}, aDeployment)
		if err != nil {
			return fmt.Errorf("could not get authorino deployment %w", err)
		}
		aDeploymentMutators := make([]reconcilers.DeploymentMutateFn, 0)
		aDeploymentMutators = append(aDeploymentMutators, reconcilers.DeploymentTemplateLabelIstioInjectMutator)
		err = r.ReconcileResource(eventCtx, &v12.Deployment{}, aDeployment, reconcilers.DeploymentMutator(aDeploymentMutators...))
		if err != nil {
			return fmt.Errorf("could not add label to authorino deployment %w", err)
		}

		// find a limitador object, then find and update the associated deployment
		lobjs := lo.FilterMap(topology.Objects().Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
			if item.GroupVersionKind().Kind == kuadrantv1beta1.LimitadorGroupKind.Kind {
				return item, true
			}
			return nil, false
		})
		// add label to limitador deployment {"sidecar.istio.io/inject":"true"}}}}}
		lDeployment := &v12.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				// TODO can't be hardcoded, this is just one example
				Name:      "limitador-limitador",
				Namespace: lobjs[0].GetNamespace(),
			},
		}
		err = r.GetResource(eventCtx, types.NamespacedName{
			Namespace: lDeployment.Namespace,
			Name:      lDeployment.Name,
		}, lDeployment)
		if err != nil {
			return fmt.Errorf("could not get authorino deployment %w", err)
		}
		lDeploymentMutators := make([]reconcilers.DeploymentMutateFn, 0)
		lDeploymentMutators = append(lDeploymentMutators, reconcilers.DeploymentTemplateLabelIstioInjectMutator)
		err = r.ReconcileResource(eventCtx, &v12.Deployment{}, lDeployment, reconcilers.DeploymentMutator(lDeploymentMutators...))
		if err != nil {
			return fmt.Errorf("could not add label to limitador deployment %w", err)
		}

		peerAuth := &istiosecurity.PeerAuthentication{
			ObjectMeta: controllerruntime.ObjectMeta{
				Name:      "default",
				Namespace: kObj.Namespace,
				Labels:    KuadrantManagedObjectLabels(),
			},
			Spec: securityv1beta1.PeerAuthentication{
				Mtls: &securityv1beta1.PeerAuthentication_MutualTLS{
					Mode: securityv1beta1.PeerAuthentication_MutualTLS_STRICT,
				},
			},
		}

		logger.Info("creating peer authentication resource", "status", "processing")

		err = r.CreateResource(eventCtx, peerAuth)
		if err != nil && !apiErrors.IsAlreadyExists(err) {
			logger.Error(err, "failed to create peer authentication resource", "status", "error")
			return err
		}
	}
	return nil
}

func (r *MTLSReconciler) deleteAllPeerAuthentications(ctx context.Context, peerAuthenticationObjs []machinery.Object, logger logr.Logger) {
	for _, peerAuthentication := range peerAuthenticationObjs {
		logger.V(1).Info(fmt.Sprintf("deleting peer authentication %s %s/%s", peerAuthentication.GroupVersionKind().Kind, peerAuthentication.GetNamespace(), peerAuthentication.GetName()))
		if err := r.Client.Resource(v1alpha3.SchemeGroupVersion.WithResource("peerauthentications")).Namespace(peerAuthentication.GetNamespace()).Delete(ctx, peerAuthentication.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, fmt.Sprintf("failed to delete peer authentication %s %s/%s", peerAuthentication.GroupVersionKind().Kind, peerAuthentication.GetNamespace(), peerAuthentication.GetName()))
			return
		}
		logger.V(1).Info(fmt.Sprintf("deleted peer authentication %s %s/%s", peerAuthentication.GroupVersionKind().Kind, peerAuthentication.GetNamespace(), peerAuthentication.GetName()))
	}
}
