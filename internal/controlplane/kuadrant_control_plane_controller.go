package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const requeueInterval = 5 * time.Minute

// Component deployer RBAC — CRD management
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=create;list;watch
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,resourceNames=dnsrecords.kuadrant.io;dnshealthcheckprobes.kuadrant.io,verbs=get;list;watch;update;patch

// Component deployer RBAC — ClusterRole management
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=dns-operator-manager-role;dns-operator-remote-cluster-role,verbs=get;list;watch;update;patch;bind;escalate

// Component deployer RBAC — ClusterRoleBinding management
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=dns-operator-manager-rolebinding;dns-operator-remote-cluster-rolebinding,verbs=delete;get;list;watch;update;patch

// Component deployer RBAC — namespace-scoped Role and RoleBinding management
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=create;delete;get;list;watch;update;patch

// KuadrantControlPlane RBAC
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrantcontrolplanes,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrantcontrolplanes/status,verbs=get;update;patch

// OLM migration cleanup permissions are granted via a namespace-scoped Role
// (config/rbac/olm_migration_role.yaml), not the ClusterRole. This limits
// Subscription/CSV access to the operator namespace only.
// Remove the Role, RoleBinding, and this comment after 2-3 releases.

type Reconciler struct {
	client.Client
	deployer *Deployer
	recorder events.EventRecorder
	logger   logr.Logger
}

func NewReconciler(c client.Client, deployer *Deployer, recorder events.EventRecorder, logger logr.Logger) *Reconciler {
	return &Reconciler{
		Client:   c,
		deployer: deployer,
		recorder: recorder,
		logger:   logger.WithName("controlplane"),
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.logger.Info("reconciling KuadrantControlPlane", "name", req.Name)

	cp := &kuadrantv1alpha1.KuadrantControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			r.logger.Info("KuadrantControlPlane deleted, re-creating")
			return r.ensureDefaultCR(ctx)
		}
		return ctrl.Result{}, err
	}

	if cp.Name != kuadrantv1alpha1.KuadrantControlPlaneDefaultName {
		r.logger.Info("ignoring KuadrantControlPlane with non-default name", "name", cp.Name)
		return ctrl.Result{}, nil
	}

	var deployErr error
	for _, component := range r.deployer.EnabledComponents() {
		if err := r.deployer.DeployComponent(ctx, component); err != nil {
			r.logger.Error(err, "failed to deploy component", "component", component.Name)
			if r.recorder != nil {
				r.recorder.Eventf(cp, componentReference(component.Name), corev1.EventTypeWarning, "ComponentDeployFailed", "ComponentDeploy", "failed to deploy component %s: %s", component.Name, err)
			}
			deployErr = err
			break
		}
	}

	if err := r.updateStatus(ctx, cp, deployErr); err != nil {
		return ctrl.Result{}, err
	}

	if deployErr != nil {
		return ctrl.Result{}, deployErr
	}

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1alpha1.KuadrantControlPlane{}).
		Watches(&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.mapToControlPlane),
			builder.WithPredicates(r.childDeploymentPredicate())).
		Watches(&apiextv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(r.mapToControlPlane),
			builder.WithPredicates(r.childCRDPredicate())).
		Named("kuadrant-control-plane").
		Complete(r)
}

func (r *Reconciler) mapToControlPlane(_ context.Context, _ client.Object) []ctrl.Request {
	return []ctrl.Request{{NamespacedName: types.NamespacedName{
		Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName,
	}}}
}

func (r *Reconciler) childCRDPredicate() predicate.Predicate {
	crdNames := r.deployer.CRDNames()
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		for _, name := range crdNames {
			if obj.GetName() == name {
				return true
			}
		}
		return false
	})
}

func (r *Reconciler) childDeploymentPredicate() predicate.Predicate {
	deploymentNames := r.deployer.DeploymentNames()
	namespace := r.deployer.Namespace()
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if obj.GetNamespace() != namespace {
			return false
		}
		for _, name := range deploymentNames {
			if obj.GetName() == name {
				return true
			}
		}
		return false
	})
}

func (r *Reconciler) ensureDefaultCR(ctx context.Context) (ctrl.Result, error) {
	if err := ensureDefaultControlPlane(ctx, r.Client, r.recorder, r.logger); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func ensureDefaultControlPlane(ctx context.Context, c client.Client, recorder events.EventRecorder, logger logr.Logger) error {
	cp := &kuadrantv1alpha1.KuadrantControlPlane{}
	err := c.Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("checking for existing KuadrantControlPlane: %w", err)
	}

	cp = &kuadrantv1alpha1.KuadrantControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName,
		},
	}
	if err := c.Create(ctx, cp); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating default KuadrantControlPlane: %w", err)
	}
	logger.Info("created default KuadrantControlPlane")
	if recorder != nil {
		recorder.Eventf(cp, cp, corev1.EventTypeNormal, "KuadrantControlPlaneCreated", "ControlPlaneBootstrap", "created default KuadrantControlPlane")
	}
	return nil
}

func (r *Reconciler) updateStatus(ctx context.Context, cp *kuadrantv1alpha1.KuadrantControlPlane, deployErr error) error {
	previousComponents := cp.Status.Components

	cp.Status.ObservedGeneration = cp.Generation
	cp.Status.Components = r.buildComponentStatuses(ctx)

	r.emitComponentVersionEvents(cp, previousComponents, cp.Status.Components)

	allReady := true
	for _, cs := range cp.Status.Components {
		if !cs.Ready {
			allReady = false
			break
		}
	}

	readyCondition := metav1.Condition{
		Type:               kuadrantv1alpha1.ControlPlaneConditionReady,
		ObservedGeneration: cp.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if deployErr != nil {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = kuadrantv1alpha1.ControlPlaneReasonDeployFailed
		readyCondition.Message = deployErr.Error()
	} else if !allReady {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = kuadrantv1alpha1.ControlPlaneReasonComponentsUnhealthy
	} else {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = kuadrantv1alpha1.ControlPlaneReasonComponentsHealthy
	}

	meta.SetStatusCondition(&cp.Status.Conditions, readyCondition)

	return r.Status().Update(ctx, cp)
}

// emitComponentVersionEvents compares the chart versions recorded in status
// before and after this reconcile, surfacing initial installs and upgrades
// that would otherwise only be visible by diffing status snapshots.
func (r *Reconciler) emitComponentVersionEvents(cp *kuadrantv1alpha1.KuadrantControlPlane, previous, current []kuadrantv1alpha1.ComponentStatus) {
	if r.recorder == nil {
		return
	}

	previousVersions := make(map[string]string, len(previous))
	for _, cs := range previous {
		previousVersions[cs.Name] = cs.ChartVersion
	}

	for _, cs := range current {
		if cs.ChartVersion == "" {
			continue
		}
		oldVersion, existed := previousVersions[cs.Name]
		switch {
		case !existed || oldVersion == "":
			r.recorder.Eventf(cp, componentReference(cs.Name), corev1.EventTypeNormal, "ComponentInstalled", "ComponentDeploy", "component %s installed at version %s", cs.Name, cs.ChartVersion)
		case oldVersion != cs.ChartVersion:
			r.recorder.Eventf(cp, componentReference(cs.Name), corev1.EventTypeNormal, "ComponentVersionChanged", "ComponentDeploy", "component %s updated from %s to %s", cs.Name, oldVersion, cs.ChartVersion)
		}
	}
}

// componentReference builds a synthetic reference used only to distinguish
// per-component events sharing the same regarding object (the KuadrantControlPlane)
// and reason — without it, the event recorder's isomorphic-event aggregation
// would collapse events about different components into a single event series,
// discarding all but the first component's message.
func componentReference(name string) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Kind: "Component",
		Name: name,
	}
}

func (r *Reconciler) buildComponentStatuses(ctx context.Context) []kuadrantv1alpha1.ComponentStatus {
	components := r.deployer.EnabledComponents()
	statuses := make([]kuadrantv1alpha1.ComponentStatus, 0, len(components))
	for _, component := range components {
		cs := kuadrantv1alpha1.ComponentStatus{
			Name:         component.Name,
			Ready:        r.isDeploymentReady(ctx, component),
			ChartVersion: r.getChartVersion(component),
			Images:       r.getImageStatuses(component),
			CRDs:         r.getCRDStatuses(ctx, component),
		}
		statuses = append(statuses, cs)
	}
	return statuses
}

func (r *Reconciler) getChartVersion(component Component) string {
	return r.deployer.ChartVersion(component.Name)
}

func (r *Reconciler) getImageStatuses(component Component) []kuadrantv1alpha1.ImageStatus {
	deployed := r.deployer.DeployedImages(component.Name)
	images := make([]kuadrantv1alpha1.ImageStatus, 0, len(deployed))
	for _, d := range deployed {
		images = append(images, kuadrantv1alpha1.ImageStatus{Name: d.Container, Image: d.Image})
	}
	return images
}

func (r *Reconciler) isDeploymentReady(ctx context.Context, component Component) bool {
	if component.DeploymentName == "" {
		return false
	}
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: r.deployer.Namespace(),
		Name:      component.DeploymentName,
	}, deploy); err != nil {
		if !apierrors.IsNotFound(err) {
			r.logger.V(1).Info("unexpected error checking deployment readiness",
				"component", component.Name, "error", err)
		}
		return false
	}
	return isDeploymentAvailable(deploy)
}

func isDeploymentAvailable(deploy *appsv1.Deployment) bool {
	for _, c := range deploy.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == "True"
		}
	}
	return false
}

func (r *Reconciler) getCRDStatuses(ctx context.Context, component Component) []kuadrantv1alpha1.CRDStatus {
	statuses := make([]kuadrantv1alpha1.CRDStatus, 0, len(component.CRDNames))
	crdGVR := apiextv1.SchemeGroupVersion.WithResource("customresourcedefinitions")
	dynClient := r.deployer.DynamicClient()

	for _, name := range component.CRDNames {
		established := false
		obj, err := dynClient.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			established = isCRDEstablished(obj)
		}
		statuses = append(statuses, kuadrantv1alpha1.CRDStatus{
			Name:        name,
			Established: established,
		})
	}
	return statuses
}
