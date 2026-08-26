//go:build unit

package controllers

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func testLogger() logr.Logger {
	return logr.Discard()
}

func TestNewNetworkPolicyReconcilerB(t *testing.T) {
	t.Run("constructor returns non-nil", func(t *testing.T) {
		reconciler := NewOperatorNetworkPolicyReconciler(nil)
		assert.Assert(t, reconciler != nil)
	})

	t.Run("subscription has 2 events", func(t *testing.T) {
		reconciler := NewOperatorNetworkPolicyReconciler(nil)
		subscription := reconciler.Subscription()

		assert.Assert(t, subscription != nil)
		assert.Assert(t, is.Len(subscription.Events, 2))
	})

	t.Run("events match expected kinds", func(t *testing.T) {
		reconciler := NewOperatorNetworkPolicyReconciler(nil)
		subscription := reconciler.Subscription()
		events := subscription.Events

		assert.DeepEqual(t, events[0].Kind, &kuadrantv1beta1.NetworkPolicyGroupKind)
		assert.DeepEqual(t, events[1].Kind, &kuadrantv1beta1.DeploymentGroupKind)
	})
}

func TestGetManagerPortValue(t *testing.T) {
	t.Run("port found", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "manager",
								Ports: []corev1.ContainerPort{
									{Name: "metrics", ContainerPort: 8080},
									{Name: "grpc", ContainerPort: 50051},
								},
							},
						},
					},
				},
			},
		}

		port, err := getManagerPortValue("metrics", deployment)
		assert.NilError(t, err)
		assert.Equal(t, port, 8080)

		port, err = getManagerPortValue("grpc", deployment)
		assert.NilError(t, err)
		assert.Equal(t, port, 50051)
	})

	t.Run("port not found", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "manager",
								Ports: []corev1.ContainerPort{},
							},
						},
					},
				},
			},
		}

		_, err := getManagerPortValue("metrics", deployment)
		assert.ErrorContains(t, err, "no port value found")
	})

	t.Run("no manager container", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "sidecar",
								Ports: []corev1.ContainerPort{
									{Name: "metrics", ContainerPort: 8080},
								},
							},
						},
					},
				},
			},
		}

		_, err := getManagerPortValue("metrics", deployment)
		assert.ErrorContains(t, err, "no port value found")
	})

	t.Run("multiple containers only manager checked", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "sidecar",
								Ports: []corev1.ContainerPort{
									{Name: "metrics", ContainerPort: 9999},
								},
							},
							{
								Name: "manager",
								Ports: []corev1.ContainerPort{
									{Name: "metrics", ContainerPort: 8080},
								},
							},
						},
					},
				},
			},
		}

		port, err := getManagerPortValue("metrics", deployment)
		assert.NilError(t, err)
		assert.Equal(t, port, 8080)
	})
}

func makeDeployment(name, namespace string, labels map[string]string, ports []corev1.ContainerPort) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			UID:       types.UID(name + "-uid"),
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "manager",
							Ports: ports,
						},
					},
				},
			},
		},
	}
}

func makeTopologyWithDeployments(deployments []*appsv1.Deployment, gateways []*gatewayapiv1.Gateway) *machinery.Topology {
	var objs []machinery.Object
	for _, d := range deployments {
		objs = append(objs, &controller.RuntimeObject{Object: d})
	}

	store := make(controller.Store)
	for _, obj := range objs {
		store[string(obj.(metav1.Object).GetUID())] = obj.(*controller.RuntimeObject)
	}

	opts := []machinery.TopologyOptionsFunc{
		machinery.WithObjects(objs...),
		machinery.WithLinks(kuadrantv1beta1.LinkDeploymentToNetworkPolicy(store)),
		machinery.WithLinks(kuadrantv1beta1.LinkSubDeploymentsToKuadrantDeployment(store)),
	}

	for _, gw := range gateways {
		opts = append(opts, machinery.WithTargetables(&machinery.Gateway{Gateway: gw}))
	}

	topology, _ := machinery.NewTopology(opts...)
	return topology
}

func TestGetRootDeployment(t *testing.T) {
	t.Run("finds kuadrant operator deployment", func(t *testing.T) {
		kuadrantDeploy := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system", map[string]string{"app": "kuadrant"}, nil)
		otherDeploy := makeDeployment("other", "kuadrant-system", nil, nil)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{kuadrantDeploy, otherDeploy}, nil)

		result := getRootDeployment(topology)
		assert.Assert(t, result != nil)
		assert.Equal(t, result.GetName(), kuadrantOperatorDeployment)
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		otherDeploy := makeDeployment("other", "kuadrant-system", nil, nil)
		topology := makeTopologyWithDeployments([]*appsv1.Deployment{otherDeploy}, nil)

		result := getRootDeployment(topology)
		assert.Assert(t, result == nil)
	})

	t.Run("empty topology", func(t *testing.T) {
		topology := makeTopologyWithDeployments(nil, nil)

		result := getRootDeployment(topology)
		assert.Assert(t, result == nil)
	})
}

func TestGetDeployments(t *testing.T) {
	t.Run("empty topology", func(t *testing.T) {
		topology := makeTopologyWithDeployments(nil, nil)

		result := getDeployments(topology)
		assert.Assert(t, is.Len(result, 0))
	})

	t.Run("single deployment", func(t *testing.T) {
		deploy := makeDeployment("test-deploy", "ns", nil, nil)
		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deploy}, nil)

		result := getDeployments(topology)
		assert.Assert(t, is.Len(result, 1))
		assert.Equal(t, result[0].GetName(), "test-deploy")
	})

	t.Run("multiple deployments", func(t *testing.T) {
		d1 := makeDeployment("deploy-1", "ns", nil, nil)
		d2 := makeDeployment("deploy-2", "ns", nil, nil)
		topology := makeTopologyWithDeployments([]*appsv1.Deployment{d1, d2}, nil)

		result := getDeployments(topology)
		assert.Assert(t, is.Len(result, 2))
	})
}

func TestSetOwnerRef(t *testing.T) {
	t.Run("nil policy returns error", func(t *testing.T) {
		deployment := makeDeployment("test", "ns", nil, nil)
		_, err := setOwnerRef(nil, nil, deployment)
		assert.ErrorContains(t, err, "nil policy found")
	})

	t.Run("new policy gets owner ref and update=true", func(t *testing.T) {
		deployment := makeDeployment("test", "ns", nil, nil)
		deployment.Kind = "Deployment"
		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
			},
		}

		update, err := setOwnerRef(policy, nil, deployment)
		assert.NilError(t, err)
		assert.Assert(t, update, "should return update=true for new owner ref")
		assert.Assert(t, is.Len(policy.GetOwnerReferences(), 1))
		assert.Equal(t, policy.GetOwnerReferences()[0].Name, "test")
	})

	t.Run("existing policy with same owner ref - no duplicate added", func(t *testing.T) {
		deployment := makeDeployment("test", "ns", nil, nil)
		deployment.Kind = "Deployment"

		existingPolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "test",
						UID:  types.UID("test-uid"),
					},
				},
			},
		}
		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
			},
		}

		update, err := setOwnerRef(policy, existingPolicy, deployment)
		assert.NilError(t, err)
		// update is true because policy has no owner refs (line 380 check)
		assert.Assert(t, update)
		// but the ref should not be duplicated
		assert.Assert(t, is.Len(policy.GetOwnerReferences(), 1))
	})

	t.Run("existing policy with different owner ref - appends", func(t *testing.T) {
		deployment := makeDeployment("test", "ns", nil, nil)
		deployment.Kind = "Deployment"

		existingPolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "other-owner",
						UID:  types.UID("other-uid"),
					},
				},
			},
		}
		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "ns",
			},
		}

		update, err := setOwnerRef(policy, existingPolicy, deployment)
		assert.NilError(t, err)
		assert.Assert(t, update, "should update when adding new owner ref")
		assert.Assert(t, is.Len(policy.GetOwnerReferences(), 2))
	})
}

func TestProcessDeployment(t *testing.T) {
	logger := testLogger()
	topology := makeTopologyWithDeployments(nil, nil)

	t.Run("kuadrant operator deployment", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		resp, err := processDeployment(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), kuadrantOperatorDeployment)
	})

	t.Run("authorino operator deployment", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"control-plane": "authorino-operator"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		resp, err := processDeployment(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), authorinoOperatorDeployment)
	})

	t.Run("dns operator deployment", func(t *testing.T) {
		deployment := makeDeployment(dnsOperatorDeployment, "kuadrant-system",
			map[string]string{"control-plane": "dns-operator-controller-manager"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		resp, err := processDeployment(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), dnsOperatorDeployment)
	})

	t.Run("limitador operator deployment", func(t *testing.T) {
		deployment := makeDeployment(limitadorOperatorDeployment, "kuadrant-system",
			map[string]string{"control-plane": "limitador-operator-controller-manager"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		resp, err := processDeployment(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), limitadorOperatorDeployment)
	})

	t.Run("unknown deployment returns error", func(t *testing.T) {
		deployment := makeDeployment("unknown-deployment", "kuadrant-system", nil, nil)

		_, err := processDeployment(logger, deployment, topology)
		assert.ErrorContains(t, err, "no default function found")
	})
}

func TestCommonOperatorPolicy(t *testing.T) {
	logger := testLogger()

	t.Run("creates policy with metrics ingress rule", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"control-plane": "authorino-operator"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8443},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), authorinoOperatorDeployment)
		assert.Equal(t, resp.Policy.GetNamespace(), "kuadrant-system")
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress, 1), "should have one ingress rule for metrics")
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8443)))
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[0].From, 0), "metrics should not have peers")
	})

	t.Run("uses default metrics port when not found", func(t *testing.T) {
		deployment := makeDeployment(dnsOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "dns"},
			[]corev1.ContainerPort{},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.Assert(t, err != nil, "should return error for missing port")
		assert.Assert(t, resp.Policy != nil)
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8080)))
	})

	t.Run("sets common labels", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "authorino"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)

		commonLabels := CommonLabels()
		for key, value := range commonLabels {
			assert.Equal(t, resp.Policy.Labels[key], value, "common label %s should be set", key)
		}
	})

	t.Run("uses pod template labels for pod selector", func(t *testing.T) {
		podLabels := map[string]string{"app": "my-operator", "version": "v1"}
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"different": "deployment-label"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)
		deployment.Spec.Template.Labels = podLabels

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.DeepEqual(t, resp.Policy.Spec.PodSelector.MatchLabels, podLabels)
	})

	t.Run("nil deployment labels uses default", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			nil,
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.DeepEqual(t, resp.Policy.Spec.PodSelector.MatchLabels, map[string]string{"kuadrant.io/managed": "true"})
	})

	t.Run("check flags set to create when no existing policy", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "authorino"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Check.Create, "should be set to create")
	})

	t.Run("check flags set to update when existing policy in topology", func(t *testing.T) {
		deployment := makeDeployment(authorinoOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "authorino"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
			},
		)

		existingPolicy := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      authorinoOperatorDeployment,
				Namespace: "kuadrant-system",
				UID:       "existing-policy-uid",
				Labels:    CommonLabels(),
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "authorino"},
				},
				PolicyTypes: []networkingv1.PolicyType{"Ingress"},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					ingressRule([]networkingv1.NetworkPolicyPeer{}, 8080),
				},
			},
		}

		deploymentObj := &controller.RuntimeObject{Object: deployment}
		policyObj := &controller.RuntimeObject{Object: existingPolicy}

		store := make(controller.Store)
		store[string(deploymentObj.GetUID())] = deploymentObj
		store[string(policyObj.GetUID())] = policyObj

		topology, err := machinery.NewTopology(
			machinery.WithObjects(deploymentObj, policyObj),
			machinery.WithLinks(kuadrantv1beta1.LinkDeploymentToNetworkPolicy(store)),
		)
		assert.NilError(t, err)

		resp, err := commonOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, !resp.Check.Create, "should not create when policy exists")
	})
}

func TestKuadrantOperatorPolicy(t *testing.T) {
	logger := testLogger()

	t.Run("creates policy with metrics only when no gateways", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, resp.Policy != nil)
		assert.Equal(t, resp.Policy.GetName(), kuadrantOperatorDeployment)
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress, 1), "should have only metrics ingress without gateways")
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8080)))
	})

	t.Run("creates policy with metrics, grpc, wasm when gateways present", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw1",
				Namespace: "gateway-ns",
			},
		}

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, []*gatewayapiv1.Gateway{gateway})

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress, 3), "should have 3 ingress rules with gateways")

		// metrics - no peers
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8080)))
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[0].From, 0), "metrics should not have peers")

		// grpc - gateway peers
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(50051)))
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[1].From, 1), "grpc should have gateway peer")
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[1].From[0].NamespaceSelector.MatchLabels,
			map[string]string{"kubernetes.io/metadata.name": "gateway-ns"})

		// wasm - gateway peers
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[2].Ports[0].Port, new(intstr.FromInt(8082)))
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[2].From, 1), "wasm should have gateway peer")
	})

	t.Run("uses default ports when container ports missing", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{},
		)

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw1",
				Namespace: "gateway-ns",
			},
		}

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, []*gatewayapiv1.Gateway{gateway})

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.Assert(t, err != nil, "should return errors for missing ports")
		assert.Assert(t, resp.Policy != nil)
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress, 3))

		// defaults: metrics=8080, grpc=50051, wasm=8082
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8080)))
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(50051)))
		assert.DeepEqual(t, resp.Policy.Spec.Ingress[2].Ports[0].Port, new(intstr.FromInt(8082)))
	})

	t.Run("multiple gateways different namespaces", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		gw1 := &gatewayapiv1.Gateway{
			TypeMeta:   metav1.TypeMeta{Kind: "Gateway", APIVersion: "gateway.networking.k8s.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "ns1"},
		}
		gw2 := &gatewayapiv1.Gateway{
			TypeMeta:   metav1.TypeMeta{Kind: "Gateway", APIVersion: "gateway.networking.k8s.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "gw2", Namespace: "ns2"},
		}

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, []*gatewayapiv1.Gateway{gw1, gw2})

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)

		// grpc and wasm rules should both have 2 peers
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[1].From, 2), "grpc should have 2 gateway peers")
		assert.Assert(t, is.Len(resp.Policy.Spec.Ingress[2].From, 2), "wasm should have 2 gateway peers")
	})

	t.Run("sets common labels", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)

		commonLabels := CommonLabels()
		for key, value := range commonLabels {
			assert.Equal(t, resp.Policy.Labels[key], value, "common label %s should be set", key)
		}
	})

	t.Run("nil deployment labels uses default", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			nil,
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.DeepEqual(t, resp.Policy.Spec.PodSelector.MatchLabels, map[string]string{"kuadrant.io/managed": "true"})
	})

	t.Run("sets Ingress policy type", func(t *testing.T) {
		deployment := makeDeployment(kuadrantOperatorDeployment, "kuadrant-system",
			map[string]string{"app": "kuadrant"},
			[]corev1.ContainerPort{
				{Name: "metrics", ContainerPort: 8080},
				{Name: "grpc", ContainerPort: 50051},
				{Name: "wasm", ContainerPort: 8082},
			},
		)

		topology := makeTopologyWithDeployments([]*appsv1.Deployment{deployment}, nil)

		resp, err := kuadrantOperatorPolicy(logger, deployment, topology)
		assert.NilError(t, err)
		assert.Assert(t, is.Len(resp.Policy.Spec.PolicyTypes, 1))
		assert.Equal(t, resp.Policy.Spec.PolicyTypes[0], networkingv1.PolicyType("Ingress"))
	})
}
