// +build unit

package controllers

import (
	"context"
	"testing"

	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"

	"github.com/jarcoal/httpmock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (

	// Don't using the const here just because if you change the const, the behaviour
	// will change, and things will not work as expected, keep it separate.

	TestKuadrantDiscoveryAnnotationOASConfigMap = "discovery.kuadrant.io/oas-configmap"
	TestKuadrantDiscoveryAnnotationOASPath      = "discovery.kuadrant.io/oas-path"
	TestKuadrantDiscoveryAnnotationOASNamePort  = "discovery.kuadrant.io/oas-name-port"

	PetStoreOAS = `
openapi: "3.0.0"
info:
	title: "Petstore Service"
	version: "1.0.0"
servers:
	- url: https://petstore.swagger.io/v1
paths:
	/pets:
		get:
			operationId: "getPets"
			responses:
				405:
					description: "invalid input"`
)

func getReconciler() *reconcilers.BaseReconciler {

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		return nil
	}

	CatOASConfigMap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cat-oas",
			Namespace: "test",
		},
		Data: map[string]string{
			"openapi.yaml": PetStoreOAS,
		},
	}

	DogOASConfigMap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dog-oas",
			Namespace: "test",
		},
		Data: map[string]string{
			"openapi-invalid.yaml": PetStoreOAS,
		},
	}
	// Objects to track in the fake client.
	objs := []runtime.Object{CatOASConfigMap, DogOASConfigMap}

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	clientAPIReader := fake.NewFakeClient(objs...)
	recorder := record.NewFakeRecorder(10000)

	baseReconciler := reconcilers.NewBaseReconciler(cl, s, clientAPIReader, LOGTEST, recorder)
	return baseReconciler
}

func getServiceReconciler() *ServiceReconciler {
	return &ServiceReconciler{getReconciler()}
}

func getSampleService() *v1.Service {
	return &v1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "test",
			Labels:      map[string]string{KuadrantDiscoveryLabel: "true"},
			Annotations: map[string]string{},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{Port: 8080, Name: "test", Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(3000)},
				{Port: 10000, Name: "openapi", Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(3000)},
			},
			Selector: map[string]string{"svc": "cats"},
		},
	}
}

// TestOasDiscoveryNoAnnotation test that check if no annotation nothing
// happens at all
func TestOasDiscoveryNoAnnotation(t *testing.T) {
	serviceReconciler := getServiceReconciler()
	svc := getSampleService()
	log := ctrl.Log.WithName("test")
	hasOas, result, err := serviceReconciler.isOASDefined(context.Background(), svc, log)
	if hasOas {
		t.Errorf("Returning OAS when it shouldn't res='%+v', err='%s'", result, err)
	}

	if err != nil {
		t.Errorf("Returning error when it shouldn't err='%s'", err)
	}
}

// TestOasDiscoveryServiceDiscovery check isOASDefined function, and check all
// OAS discovery options, due to invalid connection, getting the first port,
// and checking the port Name.
// A complete example of this can be found on samples/api-oas-discovery.yaml
func TestOasDiscoveryServiceDiscovery(t *testing.T) {
	serviceReconciler := getServiceReconciler()
	svc := getSampleService()
	svc.ObjectMeta.Annotations = map[string]string{
		TestKuadrantDiscoveryAnnotationOASPath:     "/openapi",
		TestKuadrantDiscoveryAnnotationOASNamePort: "openapi",
	}

	// first test, should return error, API is not working at all

	log := ctrl.Log.WithName("test")
	hasOas, result, err := serviceReconciler.isOASDefined(context.Background(), svc, log)
	if err == nil {
		t.Errorf("HTTP request cannot perform, should return an error")
	}

	// Second test, should return the OpenAPI spec
	httpmock.Activate()
	defer httpmock.Deactivate()
	httpmock.RegisterResponder(
		"GET",
		"http://test.test.svc:10000/openapi",
		httpmock.NewStringResponder(200, PetStoreOAS))
	hasOas, result, err = serviceReconciler.isOASDefined(context.Background(), svc, log)

	httpmock.Deactivate()
	if err != nil {
		t.Errorf("returning error when it shouldn't")
	}

	if !hasOas {
		t.Errorf("HasOas should be true, because the annotation")
	}

	if result != PetStoreOAS {
		t.Errorf("URL body does not match with PetStoreOas, expected='%v' got='%s'", result, PetStoreOAS)
	}

	// Third test, use the first port, if OasNamePort is not defined.
	svc.ObjectMeta.Annotations = map[string]string{
		TestKuadrantDiscoveryAnnotationOASPath: "/openapi",
	}

	httpmock.Activate()
	defer httpmock.Deactivate()
	httpmock.RegisterResponder(
		"GET",
		"http://test.test.svc:8080/openapi",
		httpmock.NewStringResponder(200, PetStoreOAS))
	hasOas, result, err = serviceReconciler.isOASDefined(context.Background(), svc, log)

	if err != nil {
		t.Errorf("returning error when it shouldn't")
	}

	if !hasOas {
		t.Errorf("HasOas should be true, because the annotation")
	}

	if result != PetStoreOAS {
		t.Errorf("URL body does not match with PetStoreOas, expected='%v' got='%s'", result, PetStoreOAS)
	}

}

// TestOasDiscoveryConfigMapDiscovery checks the configmap annotations, and
// retrieve the information from there, it check all options, missing CM or
// valid one,  and valid one with wrong data.
func TestOasDiscoveryConfigMapDiscovery(t *testing.T) {
	serviceReconciler := getServiceReconciler()
	svc := getSampleService()
	svc.ObjectMeta.Annotations = map[string]string{
		TestKuadrantDiscoveryAnnotationOASConfigMap: "cat-oas-invalid",
	}
	// First test, no configmap

	log := ctrl.Log.WithName("test")
	hasOas, result, err := serviceReconciler.isOASDefined(context.Background(), svc, log)
	if err != nil && err.Error() != "configmaps \"cat-oas-invalid\" not found" {
		t.Errorf("Should return a error err='%s'", err)
	}

	if !hasOas {
		t.Errorf("HasOas should be true, because the annotation")
	}

	if result != "" {
		t.Errorf("Result is invalid, should be empty string")
	}

	// Second test, return correctly
	svc.ObjectMeta.Annotations = map[string]string{
		TestKuadrantDiscoveryAnnotationOASConfigMap: "cat-oas",
	}
	hasOas, result, err = serviceReconciler.isOASDefined(context.Background(), svc, log)
	if err != nil {
		t.Errorf("returning error when it shouldn't")
	}

	if !hasOas {
		t.Errorf("HasOas should be true, because the annotation")
	}

	if result != PetStoreOAS {
		t.Errorf("URL body does not match with PetStoreOas, expected='%v' got='%s'", result, PetStoreOAS)
	}

	svc.ObjectMeta.Annotations = map[string]string{
		TestKuadrantDiscoveryAnnotationOASConfigMap: "dog-oas",
	}
	hasOas, result, err = serviceReconciler.isOASDefined(context.Background(), svc, log)
	if err != nil && err.Error() != "oas configmap is missing the openapi.yaml entry" {
		t.Errorf("Returned error is not a valid one err='%s'", err)
	}

	if !hasOas {
		t.Errorf("HasOas should be true, because the annotation")
	}
}
