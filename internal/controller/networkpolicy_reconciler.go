package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/go-logr/logr"
	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

const (
	NetworkPolicyReconcilerName = "NetworkPolicyReconciler"
	AuthorinoNetworkPolicy      = "kuadrant-authorino"
	LimitadorNetworkPolicy      = "kuadrant-limitador"
)

// writeChecks used to decide if a resource should be write to a cluster via create or update
type writeChecks = struct {
	Create bool
	Update bool
}

type NetworkPolicyReconciler struct {
	Client *dynamic.DynamicClient
}

//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func NewNetworkPolicyReconciler(client *dynamic.DynamicClient) *NetworkPolicyReconciler {
	return &NetworkPolicyReconciler{Client: client}
}

func (r *NetworkPolicyReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &v1beta1.KuadrantGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &v1beta1.KuadrantGroupKind, EventType: ptr.To(controller.DeleteEvent)},
			{Kind: &v1beta1.AuthorinoGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &v1beta1.AuthorinoGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &v1beta1.LimitadorGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &v1beta1.LimitadorGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &v1beta1.NetworkPolicyGroupKind},
			{Kind: &machinery.GatewayGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &machinery.GatewayGroupKind, EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *NetworkPolicyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	defer span.End()

	logger := controller.LoggerFromContext(ctx).WithName(NetworkPolicyReconcilerName)
	logger.Info("reconciling networkPolicy resource", "status", "started")
	defer logger.Info("reconciling networkPolicy resource", "status", "completed")

	policies := getNetworkPolicies(topology)

	kObj := GetKuadrantFromTopology(topology, state)
	if kObj == nil {
		span.AddEvent("no kuadrant object found")
		span.SetStatus(codes.Ok, "no kuadrant resource")
		return nil
	}

	var errs []error
	// -------------------------------------------------------------------------------------------------------------
	span.AddEvent("setting authorino network policy")
	update := false

	authorinoObj := GetAuthorinoFromTopology(topology, state)

	minAuthorinoNetworkPolicy := generateAuthorinoNetworkPolicy(kObj, authorinoObj, topology)

	var existingAuthorinoNetworkPolicy *networkingv1.NetworkPolicy
	for _, policy := range policies {
		if policy.GetName() == AuthorinoNetworkPolicy {
			existingAuthorinoNetworkPolicy = policy
			break
		}
	}

	desiredAuthorinoNetworkPolicy, update := mergeNetworkPolicy(*minAuthorinoNetworkPolicy, existingAuthorinoNetworkPolicy)

	if authorinoObj != nil {
		ownerRef := metav1.OwnerReference{
			APIVersion:         authorinoObj.GroupVersionKind().GroupVersion().String(),
			Kind:               authorinoObj.Kind,
			Name:               authorinoObj.GetName(),
			UID:                authorinoObj.GetUID(),
			BlockOwnerDeletion: new(true),
			Controller:         new(true),
		}

		var existingOwnerRefs []metav1.OwnerReference

		if existingAuthorinoNetworkPolicy != nil {
			existingOwnerRefs = existingAuthorinoNetworkPolicy.GetOwnerReferences()
		}
		if !slices.ContainsFunc(existingOwnerRefs, func(ref metav1.OwnerReference) bool {
			return ref.UID == ownerRef.UID
		}) {
			existingOwnerRefs = append(existingOwnerRefs, ownerRef)
		}

		desiredAuthorinoNetworkPolicy.SetOwnerReferences(existingOwnerRefs)
	}

	err := r.writePolicyToCluster(ctx, logger, span, desiredAuthorinoNetworkPolicy, writeChecks{
		Create: existingAuthorinoNetworkPolicy == nil,
		Update: update,
	})
	if err != nil {
		logger.Error(err, "failed to write authorino network policy to cluster")
		errs = append(errs, err)
	}

	// -------------------------------------------------------------------------------------------------------------

	span.AddEvent("setting limitador network policy")

	lObj := GetLimitadorFromTopology(topology, state)
	minLimitadorNetworkPolicy := generateLimitadorNetworkPolicy(kObj, lObj, topology)

	var existingLimitadorNetworkPolicy *networkingv1.NetworkPolicy
	for _, policy := range policies {
		if policy.GetName() == LimitadorNetworkPolicy {
			existingLimitadorNetworkPolicy = policy
			break
		}
	}

	desiredLimitadorNetworkPolicy, update := mergeNetworkPolicy(*minLimitadorNetworkPolicy, existingLimitadorNetworkPolicy)

	if lObj != nil {
		ownerRef := metav1.OwnerReference{
			APIVersion:         lObj.GroupVersionKind().GroupVersion().String(),
			Kind:               lObj.Kind,
			Name:               lObj.GetName(),
			UID:                lObj.GetUID(),
			BlockOwnerDeletion: new(true),
			Controller:         new(true),
		}

		var existingOwnerRefs []metav1.OwnerReference

		if existingLimitadorNetworkPolicy != nil {
			existingOwnerRefs = existingLimitadorNetworkPolicy.GetOwnerReferences()
		}
		if !slices.ContainsFunc(existingOwnerRefs, func(ref metav1.OwnerReference) bool {
			return ref.UID == ownerRef.UID
		}) {
			existingOwnerRefs = append(existingOwnerRefs, ownerRef)
		}

		desiredLimitadorNetworkPolicy.SetOwnerReferences(existingOwnerRefs)
	}

	err = r.writePolicyToCluster(ctx, logger, span, desiredLimitadorNetworkPolicy, writeChecks{
		Create: existingLimitadorNetworkPolicy == nil,
		Update: update,
	})
	if err != nil {
		logger.Error(err, "failed to write limitador network policy to cluster")
		errs = append(errs, err)
	}
	// -------------------------------------------------------------------------------------------------------------

	if len(errs) > 0 {
		span.SetStatus(codes.Error, "reconciliation completed with errors")
	} else {
		span.SetStatus(codes.Ok, "")
	}
	return errors.Join(errs...)
}

func (r *NetworkPolicyReconciler) writePolicyToCluster(ctx context.Context, logger logr.Logger, span trace.Span, networkPolicy *networkingv1.NetworkPolicy, check writeChecks) error {
	if networkPolicy == nil {
		return fmt.Errorf("networkPolicy is nil")
	}

	desiredNetworkPolicyUnstructured, err := controller.Destruct(networkPolicy)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to destruct NetworkPolicy")
		logger.Error(err, "failed to destruct NetworkPolicy object", "networkPolicy", networkPolicy)
		return err
	}
	if check.Create {
		logger.Info("creating network policy")
		if _, err = r.Client.Resource(v1beta1.NetworkPolicyResource).Namespace(networkPolicy.GetNamespace()).Create(ctx, desiredNetworkPolicyUnstructured, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create NetworkPolicy")
			logger.Error(err, "failed to create NetworkPolicy object", "networkPolicy", desiredNetworkPolicyUnstructured.Object)
			return err
		}
	} else if check.Update {
		if _, err = r.Client.Resource(v1beta1.NetworkPolicyResource).Namespace(networkPolicy.GetNamespace()).Update(ctx, desiredNetworkPolicyUnstructured, metav1.UpdateOptions{}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to update NetworkPolicy")
			logger.Error(err, "failed to update NetworkPolicy object", "networkPolicy", desiredNetworkPolicyUnstructured.Object)
			return err
		}
	}
	return nil
}

func getNetworkPolicies(topology *machinery.Topology) []*networkingv1.NetworkPolicy {
	policies := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GroupVersionKind().GroupKind().Kind == v1beta1.NetworkPolicyGroupKind.Kind
	})

	if policies == nil {
		return nil
	}

	output := make([]*networkingv1.NetworkPolicy, len(policies))

	for idx, policy := range policies {
		p, ok := policy.(*controller.RuntimeObject).Object.(*networkingv1.NetworkPolicy)
		if ok && p != nil {
			output[idx] = p
		}
	}

	return output
}

func gatewayNamespacePeers(topology *machinery.Topology) []networkingv1.NetworkPolicyPeer {
	gateways := topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().Kind == machinery.GatewayGroupKind.Kind
	})
	var seen []string
	var peers []networkingv1.NetworkPolicyPeer
	for _, gateway := range gateways {
		ns := gateway.GetNamespace()
		if !slices.Contains(seen, ns) {
			seen = append(seen, ns)
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns},
				},
			})
		}
	}
	return peers
}

func generateAuthorinoNetworkPolicy(kObj *v1beta1.Kuadrant, aObj *authorinooperatorv1beta1.Authorino, topology *machinery.Topology) *networkingv1.NetworkPolicy {
	fromNamespaces := gatewayNamespacePeers(topology)

	labels := linkedDeploymentLabels(&controller.RuntimeObject{Object: aObj}, topology)

	if labels == nil {
		labels = map[string]string{"kuadrant.io/managed": "true"}
	}

	// These default port values are hardcode into the authServerCmd in the authorino repo
	// https://github.com/Kuadrant/authorino/blob/58fecc6cdec38376fa7dba5638f1f7ecb6964cd0/main.go#L178-L218
	gRPCport := 50051
	HTTPport := 5051
	OIDCdiscoveryPort := 8083

	if aObj != nil {
		if aObj.Spec.Listener.Ports.GRPC != nil {
			gRPCport = int(*aObj.Spec.Listener.Ports.GRPC)
		}
		if aObj.Spec.Listener.Ports.HTTP != nil {
			HTTPport = int(*aObj.Spec.Listener.Ports.HTTP)
		}
		if aObj.Spec.OIDCServer.Port != nil {
			OIDCdiscoveryPort = int(*aObj.Spec.OIDCServer.Port)
		}
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AuthorinoNetworkPolicy,
			Namespace: kObj.GetNamespace(),
			Labels:    CommonLabels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			PolicyTypes: []networkingv1.PolicyType{"Ingress"},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// gRPC ext-auth from Envoy
				ingressRule(fromNamespaces, gRPCport),
				// HTTP ext-auth from gateway
				ingressRule(fromNamespaces, HTTPport),
				// OIDC discovery endpoint
				ingressRule([]networkingv1.NetworkPolicyPeer{}, OIDCdiscoveryPort),
			},
		},
	}
}

func ingressRule(from []networkingv1.NetworkPolicyPeer, port int) networkingv1.NetworkPolicyIngressRule {
	return networkingv1.NetworkPolicyIngressRule{
		From:  from,
		Ports: networkPolicyPortTCP(port),
	}
}

func networkPolicyPortTCP(port int) []networkingv1.NetworkPolicyPort {
	return []networkingv1.NetworkPolicyPort{
		{
			Protocol: ptr.To(corev1.ProtocolTCP),
			Port:     new(intstr.FromInt(port))},
	}
}

func mergeNetworkPolicy(desired networkingv1.NetworkPolicy, current *networkingv1.NetworkPolicy) (*networkingv1.NetworkPolicy, bool) {
	changed := false

	if current == nil {
		return &desired, true
	}
	// check desiredLabels
	desiredLabels := desired.GetLabels()
	currentLabels := current.GetLabels()
	for key, dValue := range desiredLabels {
		if currentLabels[key] != dValue {
			current.Labels[key] = dValue
			changed = true
		}
	}

	if !reflect.DeepEqual(desired.Spec.Ingress, current.Spec.Ingress) {
		current.Spec.Ingress = desired.Spec.Ingress
		changed = true
	}

	if !reflect.DeepEqual(desired.Spec.PodSelector, current.Spec.PodSelector) {
		current.Spec.PodSelector = desired.Spec.PodSelector
		changed = true
	}

	return current, changed
}

func generateLimitadorNetworkPolicy(kObj *v1beta1.Kuadrant, lObj *limitadorv1alpha1.Limitador, topology *machinery.Topology) *networkingv1.NetworkPolicy {
	fromNamespaces := gatewayNamespacePeers(topology)

	labels := linkedDeploymentLabels(&controller.RuntimeObject{Object: lObj}, topology)

	if labels == nil {
		labels = map[string]string{"kuadrant.io/managed": "true"}
	}

	// These default port values are hardcode into the impl Configuration in the limitador repo
	// https://github.com/Kuadrant/limitador/blob/f73e5f4b3d9af3756d4d772b35d2798693b961f9/limitador-server/src/config.rs#L101-L103
	gRPCport := 8081
	HTTPport := 8080

	if lObj != nil {
		gRPCport = int(lObj.GRPCPort())
		HTTPport = int(lObj.HTTPPort())
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LimitadorNetworkPolicy,
			Namespace: kObj.GetNamespace(),
			Labels:    CommonLabels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			PolicyTypes: []networkingv1.PolicyType{"Ingress"},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// gRPC rate limit checks
				ingressRule(fromNamespaces, gRPCport),
				// HTTP rate limit checks
				ingressRule(fromNamespaces, HTTPport),
			},
		},
	}
}

func linkedDeploymentLabels(resource *controller.RuntimeObject, topology *machinery.Topology) map[string]string {
	if resource == nil {
		return nil
	}

	// Check for typed nil - when Object interface contains a nil pointer
	v := reflect.ValueOf(resource.Object)
	if !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
		return nil
	}

	deployments := lo.FilterMap(topology.All().Children(resource), func(child machinery.Object, _ int) (*appsv1.Deployment, bool) {
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

	if len(deployments) == 1 {
		deployment := deployments[0]
		return deployment.Spec.Template.GetLabels()
	}

	return nil
}
