package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

const (
	NetworkPolicyReconcilerBName = "NetworkPolicyReconciler.operators"

	authorinoOperatorDeployment = "authorino-operator"
	dnsOperatorDeployment       = "dns-operator-controller-manager"
	kuadrantOperatorDeployment  = "kuadrant-operator-controller-manager"
	limitadorOperatorDeployment = "limitador-operator-controller-manager"
)

type OperatorNetworkPolicyReconciler struct {
	Client *dynamic.DynamicClient
}

type response struct {
	Policy *networkingv1.NetworkPolicy
	Check  writeChecks
}

//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func NewOperatorNetworkPolicyReconciler(client *dynamic.DynamicClient) *OperatorNetworkPolicyReconciler {
	return &OperatorNetworkPolicyReconciler{Client: client}
}

func (r *OperatorNetworkPolicyReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &v1beta1.NetworkPolicyGroupKind},
			{Kind: &v1beta1.DeploymentGroupKind},
		},
	}
}

func (r *OperatorNetworkPolicyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	defer span.End()

	logger := controller.LoggerFromContext(ctx).WithName(NetworkPolicyReconcilerBName)
	logger.Info("reconciling networkPolicy resources for operators", "status", "started")
	defer logger.Info("reconciling networkPolicy resources for operators", "status", "completed")

	var errs []error

	rootDeployment := getRootDeployment(topology)
	if rootDeployment == nil {
		logger.Info("no kuadrant deployment found, not creating network policies")
		return nil
	}

	deployments := lo.FilterMap(topology.All().Children(&controller.RuntimeObject{Object: rootDeployment}), func(child machinery.Object, _ int) (*appsv1.Deployment, bool) {
		if child.GroupVersionKind().GroupKind() != v1beta1.DeploymentGroupKind {
			return nil, false
		}
		runtimeObj, ok := child.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}
		deployment, ok := runtimeObj.Object.(*appsv1.Deployment)
		return deployment, ok
	})

	deployments = append(deployments, rootDeployment)
	for _, deployment := range deployments {
		logger.Info("Processing deployment", "deployment", deployment.GetName())
		response, err := processDeployment(logger, deployment, topology)
		if err != nil {
			logger.Error(err, "error processing deployment", "deployment", deployment.GetName())
			errs = append(errs, err)
			continue
		}
		err = r.writePolicyToCluster(ctx, logger, span, response.Policy, response.Check)
		if err != nil {
			logger.Error(err, "error writing to cluster", "deployment", deployment.GetName())
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		span.SetStatus(codes.Error, "reconciliation completed with errors")
		for _, err := range errs {
			logger.Error(err, "reconciliation error")
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}
	// Don't return errors as it can cancel the context of workflows running in parallel.
	return nil
}

func (r *OperatorNetworkPolicyReconciler) writePolicyToCluster(ctx context.Context, logger logr.Logger, span trace.Span, networkPolicy *networkingv1.NetworkPolicy, check writeChecks) error {
	if networkPolicy == nil {
		return fmt.Errorf("networkPolicy is nil")
	}

	desiredNetworkPolicyUnstructured, err := controller.Destruct(networkPolicy)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct NetworkPolicy")
		logger.Error(err, "failed to destruct NetworkPolicy object", "networkPolicy", networkPolicy, "check", check)
		return err
	}
	if check.Create {
		logger.Info("creating network policy")
		if _, err = r.Client.Resource(v1beta1.NetworkPolicyResource).Namespace(networkPolicy.GetNamespace()).Create(ctx, desiredNetworkPolicyUnstructured, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create NetworkPolicy")
			logger.Error(err, "failed to create NetworkPolicy object", "networkPolicy", desiredNetworkPolicyUnstructured.Object, "check", check)
			return err
		}
	} else if check.Update {
		if _, err = r.Client.Resource(v1beta1.NetworkPolicyResource).Namespace(networkPolicy.GetNamespace()).Update(ctx, desiredNetworkPolicyUnstructured, metav1.UpdateOptions{}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to update NetworkPolicy")
			logger.Error(err, "failed to update NetworkPolicy object", "networkPolicy", desiredNetworkPolicyUnstructured.Object, "check", check)
			return err
		}
	}
	return nil
}

func processDeployment(logger logr.Logger, deployment *appsv1.Deployment, topology *machinery.Topology) (response, error) {
	switch deployment.GetName() {
	case kuadrantOperatorDeployment:
		return kuadrantOperatorPolicy(logger, deployment, topology)
	case authorinoOperatorDeployment, dnsOperatorDeployment, limitadorOperatorDeployment:
		return commonOperatorPolicy(logger, deployment, topology)
	default:
		err := fmt.Errorf("no default function found")
		logger.Error(err, "no default function found to handle deployment", "deployment", deployment.GetName())
		return response{}, err
	}
}

func commonOperatorPolicy(logger logr.Logger, deployment *appsv1.Deployment, topology *machinery.Topology) (response, error) {
	checks := writeChecks{Create: false, Update: false}
	existingPolicy := getExistingPolicy(deployment, topology)
	if existingPolicy == nil {
		checks.Create = true
	}

	labels := deployment.Spec.Template.GetLabels()

	if labels == nil {
		labels = map[string]string{"kuadrant.io/managed": "true"}
	}

	var errs []error
	metricsPort, err := getManagerPortValue("metrics", deployment)
	if err != nil {
		errs = append(errs, err)
		metricsPort = 8080
	}

	desiredPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.GetName(),
			Namespace: deployment.GetNamespace(),
			Labels:    CommonLabels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			PolicyTypes: []networkingv1.PolicyType{"Ingress"},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				ingressRule([]networkingv1.NetworkPolicyPeer{}, metricsPort),
			},
		},
	}

	policy, update := mergeNetworkPolicy(*desiredPolicy, existingPolicy)
	checks.Update = update

	updateRef, err := setOwnerRef(policy, existingPolicy, deployment)
	if err != nil {
		logger.Error(err, "error setting ownerRef on policy", "policy_name", policy.GetName())
		errs = append(errs, err)
	}

	if updateRef {
		checks.Update = updateRef
	}

	err = nil
	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return response{Policy: policy, Check: checks}, err
}

func kuadrantOperatorPolicy(logger logr.Logger, deployment *appsv1.Deployment, topology *machinery.Topology) (response, error) {
	checks := writeChecks{Create: false, Update: false}
	existingPolicy := getExistingPolicy(deployment, topology)
	if existingPolicy == nil {
		checks.Create = true
	}
	fromNamespaces := gatewayNamespacePeers(topology)

	labels := deployment.Spec.Template.GetLabels()

	if labels == nil {
		labels = map[string]string{"kuadrant.io/managed": "true"}
	}

	var errs []error
	metricsPort, err := getManagerPortValue("metrics", deployment)
	if err != nil {
		errs = append(errs, err)
		metricsPort = 8080
	}

	gRPCport, err := getManagerPortValue("grpc", deployment)
	if err != nil {
		errs = append(errs, err)
		gRPCport = 50051
	}

	wasmPort, err := getManagerPortValue("wasm", deployment)
	if err != nil {
		errs = append(errs, err)
		wasmPort = 8082
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		ingressRule([]networkingv1.NetworkPolicyPeer{}, metricsPort),
	}

	if len(fromNamespaces) > 0 {
		ingress = append(ingress, ingressRule(fromNamespaces, gRPCport))
		ingress = append(ingress, ingressRule(fromNamespaces, wasmPort))
	}

	desiredPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.GetName(),
			Namespace: deployment.GetNamespace(),
			Labels:    CommonLabels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			PolicyTypes: []networkingv1.PolicyType{"Ingress"},
			Ingress:     ingress,
		},
	}

	policy, update := mergeNetworkPolicy(*desiredPolicy, existingPolicy)
	checks.Update = update

	updateRef, err := setOwnerRef(policy, existingPolicy, deployment)
	if err != nil {
		logger.Error(err, "error setting ownerRef on policy", "policy_name", policy.GetName())
		errs = append(errs, err)
	}

	if updateRef {
		checks.Update = updateRef
	}

	err = nil
	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return response{Policy: policy, Check: checks}, err
}

func getManagerPortValue(name string, deployment *appsv1.Deployment) (int, error) {
	// containerName is always manager in how we have operators configured.
	containerName := "manager"
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			for _, port := range container.Ports {
				if port.Name == name {
					return int(port.ContainerPort), nil
				}
			}
		}
	}
	return 0, fmt.Errorf("no port value found")
}

func getRootDeployment(topology *machinery.Topology) *appsv1.Deployment {
	deployments := getDeployments(topology)
	for _, deployment := range deployments {
		if deployment.GetName() == kuadrantOperatorDeployment {
			return deployment
		}
	}
	return nil
}

func getDeployments(topology *machinery.Topology) []*appsv1.Deployment {
	deployments := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GroupVersionKind().GroupKind().Kind == v1beta1.DeploymentGroupKind.Kind
	})

	if deployments == nil {
		return nil
	}

	output := make([]*appsv1.Deployment, len(deployments))

	for idx, policy := range deployments {
		p, ok := policy.(*controller.RuntimeObject).Object.(*appsv1.Deployment)
		if ok && p != nil {
			output[idx] = p
		}
	}

	return output
}

func setOwnerRef(policy, existingPolicy *networkingv1.NetworkPolicy, deployment *appsv1.Deployment) (bool, error) {
	if policy == nil {
		return false, fmt.Errorf("nil policy found")
	}

	update := false
	ownerRef := metav1.OwnerReference{
		APIVersion:         deployment.GroupVersionKind().GroupVersion().String(),
		Kind:               deployment.Kind,
		Name:               deployment.GetName(),
		UID:                deployment.GetUID(),
		BlockOwnerDeletion: new(true),
		Controller:         new(true),
	}

	if len(policy.GetOwnerReferences()) == 0 {
		update = true
	}

	var existingOwnerRefs []metav1.OwnerReference

	if existingPolicy != nil {
		existingOwnerRefs = existingPolicy.GetOwnerReferences()
	}
	if !slices.ContainsFunc(existingOwnerRefs, func(ref metav1.OwnerReference) bool {
		return ref.UID == ownerRef.UID
	}) {
		existingOwnerRefs = append(existingOwnerRefs, ownerRef)
		update = true
	}

	policy.SetOwnerReferences(existingOwnerRefs)

	return update, nil
}

func getExistingPolicy(deployment *appsv1.Deployment, topology *machinery.Topology) *networkingv1.NetworkPolicy {
	policies := lo.FilterMap(topology.All().Children(&controller.RuntimeObject{Object: deployment}), func(child machinery.Object, _ int) (*networkingv1.NetworkPolicy, bool) {
		if child.GroupVersionKind().GroupKind() != v1beta1.NetworkPolicyGroupKind || child.GetName() != deployment.GetName() {
			return nil, false
		}
		runtimeObj, ok := child.(*controller.RuntimeObject)
		if !ok {
			return nil, false
		}
		policy, ok := runtimeObj.Object.(*networkingv1.NetworkPolicy)
		return policy, ok
	})

	if len(policies) == 1 {
		return policies[0]
	}
	return nil
}
