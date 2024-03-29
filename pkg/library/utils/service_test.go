package utils

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetService(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "svc-ns",
			Name:      "my-svc",
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

	k8sClient := fake.NewClientBuilder().WithRuntimeObjects(service).Build()

	var svc *corev1.Service
	var err error

	svc, err = GetService(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "my-svc"})
	if err != nil || svc == nil || svc.GetNamespace() != service.GetNamespace() || svc.GetName() != service.GetName() {
		t.Error("should have gotten Service svc-ns/my-svc")
	}

	svc, err = GetService(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "unknown"})
	if err == nil || !apierrors.IsNotFound(err) || svc != nil {
		t.Error("should have gotten no Service")
	}
}

func TestGetServiceWorkloadSelector(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "svc-ns",
			Name:      "my-svc",
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

	k8sClient := fake.NewClientBuilder().WithRuntimeObjects(service).Build()

	var selector map[string]string
	var err error

	selector, err = GetServiceWorkloadSelector(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "my-svc"})
	if err != nil || len(selector) != 1 || selector["a-selector"] != "what-we-are-looking-for" {
		t.Error("should not have failed to get the service workload selector")
	}

	selector, err = GetServiceWorkloadSelector(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "unknown-svc"})
	if err == nil || !apierrors.IsNotFound(err) || selector != nil {
		t.Error("should have failed to get the service workload selector")
	}
}
