//go:build unit

/*
Copyright 2021 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconcilers

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestFetchValidGateway(t *testing.T) {
	var (
		namespace = "operator-unittest"
		gwName    = "my-gateway"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	existingGateway := &gatewayapiv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.GatewaySpec{
			GatewayClassName: "istio",
		},
		Status: gatewayapiv1beta1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingGateway}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	key := client.ObjectKey{Name: gwName, Namespace: namespace}

	res, err := targetRefReconciler.FetchValidGateway(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if !reflect.DeepEqual(res.Spec, existingGateway.Spec) {
		t.Fatal("res spec not as expected")
	}
}

func TestFetchValidHTTPRoute(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	key := client.ObjectKey{Name: routeName, Namespace: namespace}

	res, err := targetRefReconciler.FetchValidHTTPRoute(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if !reflect.DeepEqual(res.Spec, existingRoute.Spec) {
		t.Fatal("res spec not as expected")
	}
}

func TestFetchValidTargetRef(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	targetRef := gatewayapiv1alpha2.PolicyTargetReference{
		Group: "gateway.networking.k8s.io",
		Kind:  "HTTPRoute",
		Name:  gatewayapiv1beta1.ObjectName(routeName),
	}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	res, err := targetRefReconciler.FetchValidTargetRef(ctx, targetRef, namespace)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	switch obj := res.(type) {
	case *gatewayapiv1beta1.HTTPRoute:
		if !reflect.DeepEqual(obj.Spec, existingRoute.Spec) {
			t.Fatal("res spec not as expected")
		}
	default:
		t.Fatal("res type not known")
	}
}

func TestReconcileTargetBackReference(t *testing.T) {
	var (
		namespace             = "operator-unittest"
		routeName             = "my-route"
		annotationName string = "some-annotation"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	policyKey := client.ObjectKey{Name: "someName", Namespace: "someNamespace"}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	err = targetRefReconciler.ReconcileTargetBackReference(ctx, policyKey, existingRoute, annotationName)
	if err != nil {
		t.Fatal(err)
	}

	res := &gatewayapiv1beta1.HTTPRoute{}
	err = cl.Get(ctx, client.ObjectKey{Name: routeName, Namespace: namespace}, res)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if len(res.GetAnnotations()) == 0 {
		t.Fatal("annotations are empty")
	}

	val, ok := res.GetAnnotations()[annotationName]
	if !ok {
		t.Fatal("expected annotation not found")
	}

	if val != policyKey.String() {
		t.Fatalf("annotation value (%s) does not match expected (%s)", val, policyKey.String())
	}
}

func TestTargetedGatewayKeys(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	keys := targetRefReconciler.TargetedGatewayKeys(ctx, existingRoute)

	if len(keys) != 1 {
		t.Fatalf("gateway key slice length is %d and it was expected to be 1", len(keys))
	}

	expectedKey := client.ObjectKey{Name: "gwName", Namespace: namespace}

	if keys[0] != expectedKey {
		t.Fatalf("gwKey value (%+v) does not match expected (%+v)", keys[0], expectedKey)
	}
}

func TestDeleteTargetBackReference(t *testing.T) {
	var (
		namespace             = "operator-unittest"
		routeName             = "my-route"
		annotationName string = "some-annotation"
	)
	baseCtx := context.Background()
	ctx := logr.NewContext(baseCtx, log.Log)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	policyKey := client.ObjectKey{Name: "someName", Namespace: "someNamespace"}

	existingRoute := &gatewayapiv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
			Annotations: map[string]string{
				annotationName: "annotationValue",
			},
		},
		Spec: gatewayapiv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1beta1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1beta1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1beta1.RouteStatus{
				Parents: []gatewayapiv1beta1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1beta1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{existingRoute}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(1000)

	baseReconciler := NewBaseReconciler(cl, s, clientAPIReader, log.Log, recorder)
	targetRefReconciler := TargetRefReconciler{
		BaseReconciler: baseReconciler,
	}

	err = targetRefReconciler.DeleteTargetBackReference(ctx, policyKey, existingRoute, annotationName)
	if err != nil {
		t.Fatal(err)
	}

	res := &gatewayapiv1beta1.HTTPRoute{}
	err = cl.Get(ctx, client.ObjectKey{Name: routeName, Namespace: namespace}, res)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil {
		t.Fatal("res is nil")
	}

	if len(res.GetAnnotations()) > 0 {
		_, ok := res.GetAnnotations()[annotationName]
		if ok {
			t.Fatal("expected annotation found and it should have been deleted")
		}
	}
}
