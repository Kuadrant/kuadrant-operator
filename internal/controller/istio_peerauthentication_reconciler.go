package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	istiosecurityapiv1 "istio.io/api/security/v1"
	istiotypev1beta1 "istio.io/api/type/v1beta1"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/istio"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

//+kubebuilder:rbac:groups=security.istio.io,resources=peerauthentications,verbs=get;list;watch;create;update;patch;delete

type PeerAuthenticationReconciler struct {
	*reconcilers.BaseReconciler

	Client *dynamic.DynamicClient
}

func NewPeerAuthenticationReconciler(mgr controllerruntime.Manager, client *dynamic.DynamicClient) *PeerAuthenticationReconciler {
	return &PeerAuthenticationReconciler{
		Client: client,
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
	}
}

func (p *PeerAuthenticationReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: p.Run, Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(kuadrantv1beta1.KuadrantGroupKind)},
			{Kind: ptr.To(istio.PeerAuthenticationGroupKind)},
		},
	}
}

func (p *PeerAuthenticationReconciler) Run(baseCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(baseCtx).WithName("PeerAuthenticationReconciler")
	ctx := logr.NewContext(baseCtx, logger)
	logger.V(1).Info("reconciling peerauthentication", "status", "started")
	defer logger.V(1).Info("reconciling peerauthentication", "status", "completed")

	kObj := GetKuadrantFromTopology(topology)

	if kObj == nil {
		// Nothing to be done. It is expected the limitador and authorino resources
		// managed by kuadrant to be removed as well
		return nil
	}

	peerAuth := &istiosecurityv1.PeerAuthentication{
		TypeMeta: metav1.TypeMeta{
			Kind:       istio.PeerAuthenticationGroupKind.Kind,
			APIVersion: istiosecurityv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: controllerruntime.ObjectMeta{
			Name:      "default",
			Namespace: kObj.Namespace,
			Labels:    KuadrantManagedObjectLabels(),
		},
		Spec: istiosecurityapiv1.PeerAuthentication{
			Mtls: &istiosecurityapiv1.PeerAuthentication_MutualTLS{
				Mode: istiosecurityapiv1.PeerAuthentication_MutualTLS_STRICT,
			},
			Selector: &istiotypev1beta1.WorkloadSelector{
				MatchLabels: KuadrantManagedObjectLabels(),
			},
		},
	}

	if !kObj.IsMTLSEnabled() {
		utils.TagObjectToDelete(peerAuth)
	}

	if err := controllerutil.SetControllerReference(kObj, peerAuth, p.Scheme()); err != nil {
		logger.Error(err, "setting owner reference to peer authentication resource", "status", "error")
		return err
	}

	err := p.ReconcileResource(ctx, &istiosecurityv1.PeerAuthentication{}, peerAuth, reconcilers.CreateOnlyMutator)
	if err != nil {
		logger.Error(err, "failed to create peer authentication resource", "status", "error")
		return err
	}

	return nil
}
