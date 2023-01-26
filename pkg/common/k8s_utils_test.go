//go:build unit
// +build unit

package common

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestObjectKeyListDifference(t *testing.T) {

	key1 := client.ObjectKey{Namespace: "ns1", Name: "obj1"}
	key2 := client.ObjectKey{Namespace: "ns2", Name: "obj2"}
	key3 := client.ObjectKey{Namespace: "ns3", Name: "obj3"}

	testCases := []struct {
		name     string
		a        []client.ObjectKey
		b        []client.ObjectKey
		expected []client.ObjectKey
	}{
		{
			"empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{},
			[]client.ObjectKey{},
		},
		{
			"a empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{key1},
			[]client.ObjectKey{},
		},
		{
			"b empty",
			[]client.ObjectKey{key1, key2},
			[]client.ObjectKey{},
			[]client.ObjectKey{key1, key2},
		},
		{
			"equal",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{},
		},
		{
			"missing key2",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key3},
			[]client.ObjectKey{key2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := ObjectKeyListDifference(tc.a, tc.b)
			if len(res) != len(tc.expected) {
				subT.Errorf("expected len (%d), got (%d)", len(tc.expected), len(res))
			}

			for idx := range res {
				if res[idx] != tc.expected[idx] {
					subT.Errorf("expected object (index %d) does not match. Expected (%s), got (%s)", idx, tc.expected[idx], res[idx])
				}
			}
		})
	}
}

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

	k8sClient := fake.NewFakeClient(service)

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

	k8sClient := fake.NewFakeClient(service)

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
