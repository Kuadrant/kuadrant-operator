package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/env"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

const (
	developerPortalControllerName           = "developer-portal-controller"
	developerPortalControllerServiceAccount = "developer-portal-controller"
	developerPortalControllerClusterRole    = "kuadrant-operator-developer-portal-controller-manager-role"
)

var (
	developerPortalControllerImage = env.GetString("RELATED_IMAGE_DEVELOPERPORTAL", "quay.io/kuadrant/developer-portal-controller:latest")
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
		{Kind: ptr.To(ClusterRoleGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		{Kind: ptr.To(ClusterRoleGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(ClusterRoleBindingGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		{Kind: ptr.To(ClusterRoleBindingGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		{Kind: ptr.To(ServiceAccountGroupKind), EventType: ptr.To(controller.DeleteEvent)},
		{Kind: ptr.To(ServiceAccountGroupKind), EventType: ptr.To(controller.UpdateEvent)},
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

	kObj := GetKuadrantFromTopology(topology)
	if kObj == nil {
		logger.V(1).Info("kuadrant resource not found, skipping")
		return nil
	}

	enabled := kObj.IsDeveloperPortalEnabled()

	if !enabled {
		logger.Info("developer portal disabled, tagging resources for deletion", "status", "processing")
	} else {
		logger.Info("developer portal enabled, reconciling resources", "status", "processing")
	}

	clusterRole := r.buildClusterRole(!enabled)
	serviceAccount := r.buildServiceAccount(kObj, !enabled)
	clusterRoleBinding := r.buildClusterRoleBinding(kObj, !enabled)
	deployment := r.buildDeployment(kObj, !enabled)

	if err := r.reconcileClusterRole(ctx, clusterRole, logger); err != nil {
		return err
	}

	if err := r.reconcileServiceAccount(ctx, serviceAccount, logger); err != nil {
		return err
	}

	if err := r.reconcileClusterRoleBinding(ctx, clusterRoleBinding, logger); err != nil {
		return err
	}

	if err := r.reconcileDeployment(ctx, deployment, logger); err != nil {
		return err
	}

	return nil
}

func (r *DeveloperPortalReconciler) buildServiceAccount(kObj *kuadrantv1beta1.Kuadrant, tagForDeletion bool) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      developerPortalControllerServiceAccount,
			Namespace: kObj.Namespace,
			Labels: map[string]string{
				"app":                         developerPortalControllerName,
				kuadrant.DeveloperPortalLabel: "true",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         kObj.GroupVersionKind().GroupVersion().String(),
					Kind:               kObj.GroupVersionKind().Kind,
					Name:               kObj.Name,
					UID:                kObj.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
	}

	if tagForDeletion {
		if sa.Annotations == nil {
			sa.Annotations = make(map[string]string)
		}
		sa.Annotations[utils.DeleteTagAnnotation] = "true"
	}

	return sa
}

func (r *DeveloperPortalReconciler) buildClusterRole(tagForDeletion bool) *rbacv1.ClusterRole {
	cr := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: developerPortalControllerClusterRole,
			Labels: map[string]string{
				"app":                         developerPortalControllerName,
				kuadrant.DeveloperPortalLabel: "true",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"devportal.kuadrant.io"},
				Resources: []string{
					"apiproducts",
					"apiproducts/status",
					"apiproducts/finalizers",
					"apikeyrequests",
					"apikeyrequests/status",
					"apikeyrequests/finalizers",
				},
				Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"create", "get", "list", "update"},
			},
			{
				APIGroups: []string{"gateway.networking.k8s.io"},
				Resources: []string{"httproutes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"extensions.kuadrant.io"},
				Resources: []string{"planpolicies"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	if tagForDeletion {
		if cr.Annotations == nil {
			cr.Annotations = make(map[string]string)
		}
		cr.Annotations[utils.DeleteTagAnnotation] = "true"
	}

	return cr
}

func (r *DeveloperPortalReconciler) buildClusterRoleBinding(kObj *kuadrantv1beta1.Kuadrant, tagForDeletion bool) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: developerPortalControllerName + "-rolebinding",
			Labels: map[string]string{
				"app":                         developerPortalControllerName,
				kuadrant.DeveloperPortalLabel: "true",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     developerPortalControllerClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      developerPortalControllerServiceAccount,
				Namespace: kObj.Namespace,
			},
		},
	}

	if tagForDeletion {
		if crb.Annotations == nil {
			crb.Annotations = make(map[string]string)
		}
		crb.Annotations[utils.DeleteTagAnnotation] = "true"
	}

	return crb
}

func (r *DeveloperPortalReconciler) buildDeployment(kObj *kuadrantv1beta1.Kuadrant, tagForDeletion bool) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      developerPortalControllerName,
			Namespace: kObj.Namespace,
			Labels: map[string]string{
				"app":                         developerPortalControllerName,
				"control-plane":               "controller-manager",
				kuadrant.DeveloperPortalLabel: "true",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         kObj.GroupVersionKind().GroupVersion().String(),
					Kind:               kObj.GroupVersionKind().Kind,
					Name:               kObj.Name,
					UID:                kObj.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
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

func (r *DeveloperPortalReconciler) reconcileClusterRole(ctx context.Context, cr *rbacv1.ClusterRole, logger logr.Logger) error {
	_, err := r.ReconcileResource(ctx, &rbacv1.ClusterRole{}, cr, reconcilers.ClusterRoleMutator(reconcilers.ClusterRoleRulesMutator))
	if err != nil {
		logger.Error(err, "reconciling cluster role", "name", cr.Name)
		return err
	}
	return nil
}

func (r *DeveloperPortalReconciler) reconcileServiceAccount(ctx context.Context, sa *corev1.ServiceAccount, logger logr.Logger) error {
	_, err := r.ReconcileResource(ctx, &corev1.ServiceAccount{}, sa, reconcilers.ServiceAccountMutator())
	if err != nil {
		logger.Error(err, "reconciling service account", "key", client.ObjectKeyFromObject(sa))
		return err
	}
	return nil
}

func (r *DeveloperPortalReconciler) reconcileClusterRoleBinding(ctx context.Context, crb *rbacv1.ClusterRoleBinding, logger logr.Logger) error {
	_, err := r.ReconcileResource(ctx, &rbacv1.ClusterRoleBinding{}, crb, reconcilers.ClusterRoleBindingMutator(reconcilers.ClusterRoleBindingSubjectsMutator))
	if err != nil {
		logger.Error(err, "reconciling cluster role binding", "name", crb.Name)
		return err
	}
	return nil
}

func (r *DeveloperPortalReconciler) reconcileDeployment(ctx context.Context, deployment *appsv1.Deployment, logger logr.Logger) error {
	_, err := r.ReconcileResource(ctx, &appsv1.Deployment{}, deployment, reconcilers.DeploymentMutator(reconcilers.DeploymentImageMutator))
	if err != nil {
		logger.Error(err, "reconciling deployment", "key", client.ObjectKeyFromObject(deployment))
		return err
	}
	return nil
}
