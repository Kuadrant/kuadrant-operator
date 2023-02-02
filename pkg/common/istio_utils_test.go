//go:build unit
// +build unit

package common

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	istiocommon "istio.io/api/type/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestIstioWorkloadSelectorFromGateway(t *testing.T) {
	hostnameAddress := gatewayapiv1alpha2.AddressType("Hostname")
	gateway := &gatewayapiv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
			Labels: map[string]string{
				"app":           "foo",
				"control-plane": "kuadrant",
			},
		},
		Status: gatewayapiv1alpha2.GatewayStatus{
			Addresses: []gatewayapiv1alpha2.GatewayAddress{
				{
					Type:  &hostnameAddress,
					Value: "my-gw-svc.my-ns.svc.cluster.local:80",
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayapiv1alpha2.AddToScheme(scheme)
	k8sClient := fake.NewFakeClientWithScheme(scheme, gateway, service)

	var selector *istiocommon.WorkloadSelector

	selector = IstioWorkloadSelectorFromGateway(context.TODO(), k8sClient, gateway)
	if selector == nil || len(selector.MatchLabels) != 1 || selector.MatchLabels["a-selector"] != "what-we-are-looking-for" {
		t.Error("should have built the istio workload selector from the gateway service")
	}
}

func TestIstioWorkloadSelectorFromGatewayMissingHostnameAddress(t *testing.T) {
	gateway := &gatewayapiv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw",
			Labels: map[string]string{
				"app":           "foo",
				"control-plane": "kuadrant",
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-gw-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = gatewayapiv1alpha2.AddToScheme(scheme)
	k8sClient := fake.NewFakeClientWithScheme(scheme, gateway, service)

	var selector *istiocommon.WorkloadSelector

	selector = IstioWorkloadSelectorFromGateway(logr.NewContext(context.TODO(), log.Log), k8sClient, gateway)
	if selector == nil || len(selector.MatchLabels) != 2 || selector.MatchLabels["app"] != "foo" || selector.MatchLabels["control-plane"] != "kuadrant" {
		t.Error("should have built the istio workload selector from the gateway labels")
	}
}
