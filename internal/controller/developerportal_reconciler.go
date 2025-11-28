package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/env"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch

const (
	developerPortalControllerName                      = "developer-portal-controller"
	developerPortalControllerServiceAccount            = "developer-portal-controller-manager"
	developerPortalFinalizer                           = "kuadrant.io/developerportal"
	developerPortalControllerClusterRole               = "developer-portal-manager-role"
	developerPortalControllerClusterRoleBinding        = "developer-portal-manager-rolebinding"
	developerPortalControllerLeaderElectionRole        = "developer-portal-leader-election-role"
	developerPortalControllerLeaderElectionRoleBinding = "developer-portal-leader-election-rolebinding"
)

var (
	developerPortalControllerImage = env.GetString("RELATED_IMAGE_DEVELOPERPORTAL", "quay.io/kuadrant/developer-portal-controller:latest")
	kuadrantOperatorNamespace      = env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
)

type DeveloperPortalReconciler struct {
	*reconcilers.BaseReconciler
}

func NewDeveloperPortalReconciler(mgr ctrlruntime.Manager) *DeveloperPortalReconciler {
	return &DeveloperPortalReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetAPIReader(),
		),
	}
}

func (r *DeveloperPortalReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{ReconcileFunc: r.Reconcile, Events: []controller.ResourceEventMatcher{
		{Kind: &kuadrantv1beta1.KuadrantGroupKind},
		// Deployment (managed by reconciler)
		{Kind: &kuadrantv1beta1.DeploymentGroupKind, EventType: ptr.To(controller.DeleteEvent)},
		{Kind: &kuadrantv1beta1.DeploymentGroupKind, EventType: ptr.To(controller.UpdateEvent)},
	},
	}
}

func (r *DeveloperPortalReconciler) Reconcile(baseCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(baseCtx).WithName("DeveloperPortalReconciler")
	ctx := logr.NewContext(baseCtx, logger)
	logger.Info("developer portal reconciler", "status", "started")
	defer logger.Info("developer portal reconciler", "status", "completed")

	kObj := GetKuadrantFromTopologyDuringDeletion(topology)
	if kObj == nil {
		logger.V(1).Info("kuadrant resource not found, skipping")
		return nil
	}

	if !controllerutil.ContainsFinalizer(kObj, developerPortalFinalizer) {
		kObjCopy := kObj.DeepCopy()
		if err := r.AddFinalizer(ctx, kObjCopy, developerPortalFinalizer); err != nil {
			logger.Error(err, "failed to add finalizer")
			return err
		}
		logger.Info("added finalizer to Kuadrant CR")
	}

	enabled := kObj.IsDeveloperPortalEnabled()
	isDeleting := kObj.GetDeletionTimestamp() != nil

	// If Kuadrant CR is being deleted, ensure Deployment cleanup happens
	if isDeleting {
		enabled = false
		logger.Info("Kuadrant CR is being deleted, ensuring developer portal Deployment cleanup")
	}

	// Verify all required RBAC resources exist (pre-installed via kustomize)
	// Skip verification during deletion to allow cleanup even if RBAC resources are missing
	if !isDeleting {
		if !r.verifyRBACResources(topology, kuadrantOperatorNamespace, logger) {
			return fmt.Errorf("developer portal rbac resources verification failed, skipping reconciliation")
		}
	} else {
		logger.V(1).Info("Skipping RBAC verification during deletion")
	}

	deployment := r.buildDeployment(kuadrantOperatorNamespace, !enabled)

	if err := r.reconcileDeployment(ctx, deployment, logger); err != nil {
		return err
	}

	// Wait for Deployment to be deleted before removing finalizer
	if !enabled && controllerutil.ContainsFinalizer(kObj, developerPortalFinalizer) && kObj.GetDeletionTimestamp() != nil {
		if err := r.Client().Get(ctx, client.ObjectKey{
			Name:      developerPortalControllerName,
			Namespace: kuadrantOperatorNamespace,
		}, &appsv1.Deployment{}); err == nil {
			logger.V(1).Info("waiting for Deployment to be deleted")
			return nil
		}

		logger.Info("developer portal Deployment deleted, removing finalizer from Kuadrant CR")
		kObjCopy := kObj.DeepCopy()
		if err := r.RemoveFinalizer(ctx, kObjCopy, developerPortalFinalizer); err != nil {
			logger.Error(err, "failed to remove finalizer")
			return err
		}
	}

	return nil
}

// verifyRBACResources checks that all required RBAC resources exist in the topology.
// Returns true if all resources exist, false otherwise.
func (r *DeveloperPortalReconciler) verifyRBACResources(topology *machinery.Topology, namespace string, logger logr.Logger) bool {
	// Check ClusterRole exists
	clusterRoles := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == ClusterRoleGroupKind && o.GetName() == developerPortalControllerClusterRole
	})
	if len(clusterRoles) == 0 {
		logger.Info("developer-portal-controller ClusterRole not found in topology, skipping reconciliation", "clusterRole", developerPortalControllerClusterRole)
		return false
	}

	// Check leader election Role exists in the operator namespace
	leaderElectionRoles := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == RoleGroupKind &&
			o.GetName() == developerPortalControllerLeaderElectionRole &&
			o.GetNamespace() == namespace
	})
	if len(leaderElectionRoles) == 0 {
		logger.Info("developer-portal-controller leader election Role not found in topology, skipping reconciliation",
			"role", developerPortalControllerLeaderElectionRole, "namespace", namespace)
		return false
	}

	// Check ServiceAccount exists in the operator namespace
	serviceAccounts := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == ServiceAccountGroupKind &&
			o.GetName() == developerPortalControllerServiceAccount &&
			o.GetNamespace() == namespace
	})
	if len(serviceAccounts) == 0 {
		logger.Info("developer-portal-controller ServiceAccount not found in topology, skipping reconciliation",
			"serviceAccount", developerPortalControllerServiceAccount, "namespace", namespace)
		return false
	}

	// Check ClusterRoleBinding exists
	clusterRoleBindings := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == ClusterRoleBindingGroupKind &&
			o.GetName() == developerPortalControllerClusterRoleBinding
	})
	if len(clusterRoleBindings) == 0 {
		logger.Info("developer-portal-controller ClusterRoleBinding not found in topology, skipping reconciliation",
			"clusterRoleBinding", developerPortalControllerClusterRoleBinding)
		return false
	}

	// Check RoleBinding exists in the operator namespace
	roleBindings := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == RoleBindingGroupKind &&
			o.GetName() == developerPortalControllerLeaderElectionRoleBinding &&
			o.GetNamespace() == namespace
	})
	if len(roleBindings) == 0 {
		logger.Info("developer-portal-controller RoleBinding not found in topology, skipping reconciliation",
			"roleBinding", developerPortalControllerLeaderElectionRoleBinding, "namespace", namespace)
		return false
	}

	return true
}

func (r *DeveloperPortalReconciler) buildDeployment(namespace string, tagForDeletion bool) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      developerPortalControllerName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                         developerPortalControllerName,
				"control-plane":               "controller-manager",
				kuadrant.DeveloperPortalLabel: "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":           developerPortalControllerName,
					"control-plane": "controller-manager",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":           developerPortalControllerName,
						"control-plane": "controller-manager",
					},
					Annotations: map[string]string{
						"kubectl.kubernetes.io/default-container": "manager",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: developerPortalControllerServiceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "manager",
							Image:   developerPortalControllerImage,
							Command: []string{"/manager"},
							Args: []string{
								"--leader-elect",
								"--health-probe-bind-address=:8081",
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
					TerminationGracePeriodSeconds: ptr.To(int64(10)),
				},
			},
		},
	}

	if tagForDeletion {
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}
		deployment.Annotations[utils.DeleteTagAnnotation] = "true"
	}

	return deployment
}

func (r *DeveloperPortalReconciler) reconcileDeployment(ctx context.Context, deployment *appsv1.Deployment, logger logr.Logger) error {
	_, err := r.ReconcileResource(ctx, &appsv1.Deployment{}, deployment, reconcilers.DeploymentMutator(reconcilers.DeploymentImageMutator))
	if err != nil {
		logger.Error(err, "reconciling deployment", "key", client.ObjectKeyFromObject(deployment))
		return err
	}
	return nil
}
