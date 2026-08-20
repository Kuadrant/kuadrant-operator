//go:build unit

package controllers

import (
	"maps"
	"testing"

	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func TestNetworkPolicyPortTCP(t *testing.T) {
	port := 8080
	result := networkPolicyPortTCP(port)

	assert.Assert(t, is.Len(result, 1), "should return single element slice")
	assert.DeepEqual(t, result[0].Protocol, ptr.To(corev1.ProtocolTCP))
	assert.DeepEqual(t, result[0].Port, new(intstr.FromInt(port)))
}

func TestIngressRule(t *testing.T) {
	t.Run("empty peers", func(t *testing.T) {
		port := 8080
		result := ingressRule([]networkingv1.NetworkPolicyPeer{}, port)

		assert.Assert(t, is.Len(result.From, 0), "From should be empty")
		assert.Assert(t, is.Len(result.Ports, 1), "Ports should have one entry")
		assert.DeepEqual(t, result.Ports[0].Port, new(intstr.FromInt(port)))
	})

	t.Run("single peer", func(t *testing.T) {
		port := 9090
		peers := []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ns1"},
				},
			},
		}
		result := ingressRule(peers, port)

		assert.Assert(t, is.Len(result.From, 1), "From should have one peer")
		assert.Assert(t, is.Len(result.Ports, 1), "Ports should have one entry")
		assert.DeepEqual(t, result.From[0], peers[0])
		assert.DeepEqual(t, result.Ports[0].Port, new(intstr.FromInt(port)))
	})

	t.Run("multiple peers", func(t *testing.T) {
		port := 7070
		peers := []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ns1"},
				},
			},
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ns2"},
				},
			},
		}
		result := ingressRule(peers, port)

		assert.Assert(t, is.Len(result.From, 2), "From should have two peers")
		assert.Assert(t, is.Len(result.Ports, 1), "Ports should have one entry")
		assert.DeepEqual(t, result.From[0], peers[0])
		assert.DeepEqual(t, result.From[1], peers[1])
		assert.DeepEqual(t, result.Ports[0].Port, new(intstr.FromInt(port)))
	})
}

func TestGatewayNamespacePeers(t *testing.T) {
	t.Run("empty topology", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := gatewayNamespacePeers(topology)
		assert.Assert(t, is.Len(result, 0), "should return empty slice for empty topology")
	})

	t.Run("single gateway", func(t *testing.T) {
		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw1",
				Namespace: "ns1",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithTargetables(&machinery.Gateway{Gateway: gateway}),
		)
		assert.NilError(t, err)

		result := gatewayNamespacePeers(topology)
		assert.Assert(t, is.Len(result, 1), "should return one peer for single gateway")
		assert.DeepEqual(t, result[0].NamespaceSelector.MatchLabels, map[string]string{"kubernetes.io/metadata.name": "ns1"})
	})

	t.Run("two gateways same namespace (dedup)", func(t *testing.T) {
		gateway1 := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw1",
				Namespace: "ns1",
			},
		}
		gateway2 := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw2",
				Namespace: "ns1",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithTargetables(
				&machinery.Gateway{Gateway: gateway1},
				&machinery.Gateway{Gateway: gateway2},
			),
		)
		assert.NilError(t, err)

		result := gatewayNamespacePeers(topology)
		assert.Assert(t, is.Len(result, 1), "should deduplicate same namespace")
		assert.DeepEqual(t, result[0].NamespaceSelector.MatchLabels, map[string]string{"kubernetes.io/metadata.name": "ns1"})
	})

	t.Run("two gateways different namespaces", func(t *testing.T) {
		gateway1 := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw1",
				Namespace: "ns1",
			},
		}
		gateway2 := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gw2",
				Namespace: "ns2",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithTargetables(
				&machinery.Gateway{Gateway: gateway1},
				&machinery.Gateway{Gateway: gateway2},
			),
		)
		assert.NilError(t, err)

		result := gatewayNamespacePeers(topology)
		assert.Assert(t, is.Len(result, 2), "should return two peers for different namespaces")
		// Check that both namespaces are present (order not guaranteed)
		namespaces := make(map[string]bool)
		for _, peer := range result {
			ns := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			namespaces[ns] = true
		}
		assert.Assert(t, namespaces["ns1"], "should contain ns1")
		assert.Assert(t, namespaces["ns2"], "should contain ns2")
	})
}

func TestMergeNetworkPolicy(t *testing.T) {
	t.Run("nil current", func(t *testing.T) {
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
		}
		result, changed := mergeNetworkPolicy(*desired, nil)

		assert.Assert(t, changed, "should report changed=true")
		assert.DeepEqual(t, result, desired)
	})

	t.Run("identical", func(t *testing.T) {
		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "pod"},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{Ports: networkPolicyPortTCP(8080)},
				},
			},
		}
		desired := policy.DeepCopy()
		current := policy.DeepCopy()

		result, changed := mergeNetworkPolicy(*desired, current)

		assert.Assert(t, !changed, "should report changed=false for identical policies")
		assert.DeepEqual(t, result, current)
	})

	t.Run("different labels", func(t *testing.T) {
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "new-value"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			},
		}
		current := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "old-value"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			},
		}

		result, changed := mergeNetworkPolicy(*desired, current)

		assert.Assert(t, changed, "should report changed=true for different labels")
		assert.Equal(t, result.Labels["app"], "new-value")
	})

	t.Run("different ingress", func(t *testing.T) {
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{Ports: networkPolicyPortTCP(9090)},
				},
			},
		}
		current := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{Ports: networkPolicyPortTCP(8080)},
				},
			},
		}

		result, changed := mergeNetworkPolicy(*desired, current)

		assert.Assert(t, changed, "should report changed=true for different ingress")
		assert.DeepEqual(t, result.Spec.Ingress, desired.Spec.Ingress)
	})

	t.Run("different pod selector", func(t *testing.T) {
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "new-pod"},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{},
			},
		}
		current := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "old-pod"},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{},
			},
		}

		result, changed := mergeNetworkPolicy(*desired, current)

		assert.Assert(t, changed, "should report changed=true for different pod selector")
		assert.DeepEqual(t, result.Spec.PodSelector, desired.Spec.PodSelector)
	})

	t.Run("current has extra labels not in desired", func(t *testing.T) {
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			},
		}
		current := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
				Labels:    map[string]string{"app": "test", "extra": "label"},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			},
		}

		result, changed := mergeNetworkPolicy(*desired, current)

		assert.Assert(t, !changed, "should report changed=false when extra labels exist")
		assert.Equal(t, result.Labels["app"], "test")
		assert.Equal(t, result.Labels["extra"], "label", "extra labels should be retained")
	})
}

func TestGenerateAuthorinoNetworkPolicy(t *testing.T) {
	kuadrant := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: "kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: "kuadrant-system",
		},
	}

	t.Run("nil authorino - default ports", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateAuthorinoNetworkPolicy(kuadrant, nil, topology)

		assert.Equal(t, result.Name, AuthorinoNetworkPolicy)
		assert.Equal(t, result.Namespace, "kuadrant-system")
		assert.DeepEqual(t, result.Spec.PodSelector.MatchLabels, map[string]string{"kuadrant.io/managed": "true"})
		assert.Assert(t, is.Len(result.Spec.Ingress, 3), "should have 3 ingress rules")

		// Verify gRPC port (default 50051)
		assert.DeepEqual(t, result.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(50051)))
		// Verify HTTP port (default 5051)
		assert.DeepEqual(t, result.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(5051)))
		// Verify OIDC port (default 8083)
		assert.DeepEqual(t, result.Spec.Ingress[2].Ports[0].Port, new(intstr.FromInt(8083)))
	})

	t.Run("authorino with custom ports", func(t *testing.T) {
		authorino := &authorinooperatorv1beta1.Authorino{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Authorino",
				APIVersion: "operator.authorino.kuadrant.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authorino",
				Namespace: "kuadrant-system",
			},
			Spec: authorinooperatorv1beta1.AuthorinoSpec{
				Listener: authorinooperatorv1beta1.Listener{
					Ports: authorinooperatorv1beta1.Ports{
						GRPC: new(int32(9000)),
						HTTP: new(int32(9001)),
					},
				},
				OIDCServer: authorinooperatorv1beta1.OIDCServer{
					Port: new(int32(9002)),
				},
			},
		}
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateAuthorinoNetworkPolicy(kuadrant, authorino, topology)

		// Verify custom gRPC port
		assert.DeepEqual(t, result.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(9000)))
		// Verify custom HTTP port
		assert.DeepEqual(t, result.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(9001)))
		// Verify custom OIDC port
		assert.DeepEqual(t, result.Spec.Ingress[2].Ports[0].Port, new(intstr.FromInt(9002)))
	})

	t.Run("gateway in topology - peers in gRPC and HTTP but not OIDC", func(t *testing.T) {
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
		topology, err := machinery.NewTopology(
			machinery.WithTargetables(&machinery.Gateway{Gateway: gateway}),
		)
		assert.NilError(t, err)

		result := generateAuthorinoNetworkPolicy(kuadrant, nil, topology)

		// gRPC rule should have gateway peer
		assert.Assert(t, is.Len(result.Spec.Ingress[0].From, 1), "gRPC should have gateway peer")
		assert.DeepEqual(t, result.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels,
			map[string]string{"kubernetes.io/metadata.name": "gateway-ns"})

		// HTTP rule should have gateway peer
		assert.Assert(t, is.Len(result.Spec.Ingress[1].From, 1), "HTTP should have gateway peer")
		assert.DeepEqual(t, result.Spec.Ingress[1].From[0].NamespaceSelector.MatchLabels,
			map[string]string{"kubernetes.io/metadata.name": "gateway-ns"})

		// OIDC rule should NOT have gateway peer (empty From)
		assert.Assert(t, is.Len(result.Spec.Ingress[2].From, 0), "OIDC should not have gateway peer")
	})

	t.Run("common labels are set", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateAuthorinoNetworkPolicy(kuadrant, nil, topology)

		commonLabels := CommonLabels()
		for key, value := range commonLabels {
			assert.Equal(t, result.Labels[key], value, "common label %s should be set", key)
		}
	})
}

func TestGenerateLimitadorNetworkPolicy(t *testing.T) {
	kuadrant := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: "kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kuadrant",
			Namespace: "kuadrant-system",
		},
	}

	t.Run("nil limitador - default ports", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateLimitadorNetworkPolicy(kuadrant, nil, topology)

		assert.Equal(t, result.Name, LimitadorNetworkPolicy)
		assert.Equal(t, result.Namespace, "kuadrant-system")
		assert.DeepEqual(t, result.Spec.PodSelector.MatchLabels, map[string]string{"kuadrant.io/managed": "true"})
		assert.Assert(t, is.Len(result.Spec.Ingress, 2), "should have 2 ingress rules")

		// Verify gRPC port (default 8081)
		assert.DeepEqual(t, result.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(8081)))
		// Verify HTTP port (default 8080)
		assert.DeepEqual(t, result.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(8080)))
	})

	t.Run("limitador with custom ports", func(t *testing.T) {
		limitador := &limitadorv1alpha1.Limitador{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Limitador",
				APIVersion: "limitador.kuadrant.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "limitador",
				Namespace: "kuadrant-system",
			},
			Spec: limitadorv1alpha1.LimitadorSpec{
				Listener: &limitadorv1alpha1.Listener{
					GRPC: &limitadorv1alpha1.TransportProtocol{Port: new(int32(7001))},
					HTTP: &limitadorv1alpha1.TransportProtocol{Port: new(int32(7002))},
				},
			},
		}
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateLimitadorNetworkPolicy(kuadrant, limitador, topology)

		// Verify custom gRPC port
		assert.DeepEqual(t, result.Spec.Ingress[0].Ports[0].Port, new(intstr.FromInt(7001)))
		// Verify custom HTTP port
		assert.DeepEqual(t, result.Spec.Ingress[1].Ports[0].Port, new(intstr.FromInt(7002)))
	})

	t.Run("gateway peers appear in both ingress rules", func(t *testing.T) {
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
		topology, err := machinery.NewTopology(
			machinery.WithTargetables(&machinery.Gateway{Gateway: gateway}),
		)
		assert.NilError(t, err)

		result := generateLimitadorNetworkPolicy(kuadrant, nil, topology)

		// gRPC rule should have gateway peer
		assert.Assert(t, is.Len(result.Spec.Ingress[0].From, 1), "gRPC should have gateway peer")
		assert.DeepEqual(t, result.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels,
			map[string]string{"kubernetes.io/metadata.name": "gateway-ns"})

		// HTTP rule should have gateway peer
		assert.Assert(t, is.Len(result.Spec.Ingress[1].From, 1), "HTTP should have gateway peer")
		assert.DeepEqual(t, result.Spec.Ingress[1].From[0].NamespaceSelector.MatchLabels,
			map[string]string{"kubernetes.io/metadata.name": "gateway-ns"})
	})

	t.Run("common labels are set", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := generateLimitadorNetworkPolicy(kuadrant, nil, topology)

		commonLabels := CommonLabels()
		for key, value := range commonLabels {
			assert.Equal(t, result.Labels[key], value, "common label %s should be set", key)
		}
	})
}

func TestLinkedDeploymentLabels(t *testing.T) {
	t.Run("nil resource", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := linkedDeploymentLabels(nil, topology)
		assert.Assert(t, result == nil, "should return nil for nil resource")
	})

	t.Run("resource with no deployment children", func(t *testing.T) {
		authorino := &authorinooperatorv1beta1.Authorino{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Authorino",
				APIVersion: "operator.authorino.kuadrant.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authorino",
				Namespace: "kuadrant-system",
				UID:       "authorino-uid",
			},
		}
		authorinoRuntimeObj := &controller.RuntimeObject{Object: authorino}

		topology, err := machinery.NewTopology(
			machinery.WithObjects(&controller.RuntimeObject{Object: authorino}),
		)
		assert.NilError(t, err)

		result := linkedDeploymentLabels(authorinoRuntimeObj, topology)
		assert.Assert(t, result == nil, "should return nil when no deployment children")
	})

	t.Run("resource with one deployment child linked via topology", func(t *testing.T) {
		authorino := &authorinooperatorv1beta1.Authorino{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Authorino",
				APIVersion: "operator.authorino.kuadrant.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authorino",
				Namespace: "kuadrant-system",
				UID:       "authorino-uid",
			},
		}
		deployment := &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "authorino",
				Namespace: "kuadrant-system",
				UID:       "deployment-uid",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":     "authorino",
							"version": "v1",
						},
					},
				},
			},
		}

		authorinoRuntimeObj := &controller.RuntimeObject{Object: authorino}
		deploymentRuntimeObj := &controller.RuntimeObject{Object: deployment}

		// Create store and link the objects
		store := make(controller.Store)
		store[string(authorinoRuntimeObj.GetUID())] = authorinoRuntimeObj
		store[string(deploymentRuntimeObj.GetUID())] = deploymentRuntimeObj

		linkFunc := kuadrantv1beta1.LinkAuthorinoToDeployment(store)

		topology, err := machinery.NewTopology(
			machinery.WithObjects(authorinoRuntimeObj, deploymentRuntimeObj),
			machinery.WithLinks(linkFunc),
		)
		assert.NilError(t, err)

		result := linkedDeploymentLabels(authorinoRuntimeObj, topology)

		assert.Assert(t, result != nil, "should return labels for linked deployment")
		expectedLabels := map[string]string{
			"app":     "authorino",
			"version": "v1",
		}
		assert.Assert(t, maps.Equal(result, expectedLabels), "should return pod template labels")
	})
}

func TestGetNetworkPolicies(t *testing.T) {
	t.Run("empty topology", func(t *testing.T) {
		topology, err := machinery.NewTopology()
		assert.NilError(t, err)

		result := getNetworkPolicies(topology)
		assert.Assert(t, is.Len(result, 0), "should return empty slice for empty topology")
	})

	t.Run("topology with network policies", func(t *testing.T) {
		policy := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithObjects(&controller.RuntimeObject{Object: policy}),
		)
		assert.NilError(t, err)

		result := getNetworkPolicies(topology)
		assert.Assert(t, is.Len(result, 1), "should return one policy")
		assert.Equal(t, result[0].Name, "test-policy")
	})

	t.Run("multiple policies", func(t *testing.T) {
		policy1 := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "policy1",
				Namespace: "test-ns",
			},
		}
		policy2 := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "policy2",
				Namespace: "test-ns",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithObjects(
				&controller.RuntimeObject{Object: policy1},
				&controller.RuntimeObject{Object: policy2},
			),
		)
		assert.NilError(t, err)

		result := getNetworkPolicies(topology)
		assert.Assert(t, is.Len(result, 2), "should return all policies")
	})

	t.Run("mixed objects - only NetworkPolicy kind returned", func(t *testing.T) {
		policy := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "test-ns",
			},
		}
		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "gateway.networking.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-gateway",
				Namespace: "test-ns",
			},
		}
		topology, err := machinery.NewTopology(
			machinery.WithObjects(&controller.RuntimeObject{Object: policy}),
			machinery.WithTargetables(&machinery.Gateway{Gateway: gateway}),
		)
		assert.NilError(t, err)

		result := getNetworkPolicies(topology)
		assert.Assert(t, is.Len(result, 1), "should return only NetworkPolicy objects")
		assert.Equal(t, result[0].Name, "test-policy")
	})
}

func TestNewNetworkPolicyReconciler(t *testing.T) {
	t.Run("constructor returns non-nil", func(t *testing.T) {
		reconciler := NewNetworkPolicyReconciler(nil)
		assert.Assert(t, reconciler != nil, "constructor should return non-nil reconciler")
	})

	t.Run("subscription has 9 events", func(t *testing.T) {
		reconciler := NewNetworkPolicyReconciler(nil)
		subscription := reconciler.Subscription()

		assert.Assert(t, subscription != nil, "subscription should not be nil")
		assert.Assert(t, is.Len(subscription.Events, 9), "subscription should have 9 events")
	})

	t.Run("events match expected kinds and event types", func(t *testing.T) {
		reconciler := NewNetworkPolicyReconciler(nil)
		subscription := reconciler.Subscription()
		events := subscription.Events

		// Kuadrant Create
		assert.DeepEqual(t, events[0].Kind, &kuadrantv1beta1.KuadrantGroupKind)
		assert.DeepEqual(t, events[0].EventType, ptr.To(controller.CreateEvent))

		// Kuadrant Delete
		assert.DeepEqual(t, events[1].Kind, &kuadrantv1beta1.KuadrantGroupKind)
		assert.DeepEqual(t, events[1].EventType, ptr.To(controller.DeleteEvent))

		// Authorino Create
		assert.DeepEqual(t, events[2].Kind, &kuadrantv1beta1.AuthorinoGroupKind)
		assert.DeepEqual(t, events[2].EventType, ptr.To(controller.CreateEvent))

		// Authorino Update
		assert.DeepEqual(t, events[3].Kind, &kuadrantv1beta1.AuthorinoGroupKind)
		assert.DeepEqual(t, events[3].EventType, ptr.To(controller.UpdateEvent))

		// Limitador Create
		assert.DeepEqual(t, events[4].Kind, &kuadrantv1beta1.LimitadorGroupKind)
		assert.DeepEqual(t, events[4].EventType, ptr.To(controller.CreateEvent))

		// Limitador Update
		assert.DeepEqual(t, events[5].Kind, &kuadrantv1beta1.LimitadorGroupKind)
		assert.DeepEqual(t, events[5].EventType, ptr.To(controller.UpdateEvent))

		// NetworkPolicy (all events)
		assert.DeepEqual(t, events[6].Kind, &kuadrantv1beta1.NetworkPolicyGroupKind)
		assert.Assert(t, events[6].EventType == nil, "NetworkPolicy should not have EventType specified")

		// Gateway Create
		assert.DeepEqual(t, events[7].Kind, &machinery.GatewayGroupKind)
		assert.DeepEqual(t, events[7].EventType, ptr.To(controller.CreateEvent))

		// Gateway Delete
		assert.DeepEqual(t, events[8].Kind, &machinery.GatewayGroupKind)
		assert.DeepEqual(t, events[8].EventType, ptr.To(controller.DeleteEvent))
	})
}
