//go:build unit

package common

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

type testCase struct {
	name       string
	fileData   []byte
	objects    []runtime.Object
	expectErr  bool
	expectLogs bool
}

func TestDecodeFile(t *testing.T) {
	testCases := []testCase{
		{
			name: "when decoding doc with known valid Kubernetes object then return no error or logs",
			fileData: []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        ports:
        - containerPort: 80
`),
			objects: []runtime.Object{&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
			}},
			expectErr:  false,
			expectLogs: false,
		},
		{
			name: "when decoding multidoc YAML file with valid Kubernetes objects then return no error or logs",
			fileData: []byte(`
---
apiVersion: apps/v1
kind: Pod
metadata:
  name: example-pod
spec:
  containers:
  - name: nginx
    image: nginx:latest
    ports:
    - containerPort: 80
---
apiVersion: apps/v1
kind: Service
metadata:
  name: example-service
spec:
  selector:
    app: nginx
  ports:
  - protocol: TCP
    port: 80
    targetPort: 80
`),
			objects: []runtime.Object{&corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "apps/v1",
				},
			}, &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "apps/v1",
				},
			}},
			expectErr:  false,
			expectLogs: false,
		},
		{
			name: "when decoding doc with invalid object then return error and logs",
			fileData: []byte(`
apiVersion: v1
kind: InvalidObject
metadata:
  name: example-invalid
spec:
  invalidField: invalidValue
`),
			objects: []runtime.Object{&corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "apps/v1",
				},
			}, &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "apps/v1",
				},
			}},
			expectErr:  true,
			expectLogs: true,
		},
		{
			name: "when decoding doc with known Kubernetes object which misses Kind then return error and logs",
			fileData: []byte(`
apiVersion: v1
metadata:
  name: example-object
`),
			objects:    []runtime.Object{},
			expectErr:  true,
			expectLogs: true,
		},
		{
			name: "when decoding empty doc (consists of '---') then return error and logs",
			fileData: []byte(`---
---
`),
			objects: []runtime.Object{&corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "apps/v1",
				},
			}},
			expectErr:  true,
			expectLogs: true,
		},
		{
			name:     "when decoding empty doc (empty file data) then return error and logs",
			fileData: []byte(``),
			objects: []runtime.Object{&corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "apps/v1",
				},
			}},
			expectErr:  false,
			expectLogs: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logBuffer := bytes.Buffer{}
			logger := log.NewLogger(
				log.WriteTo(&logBuffer),
				log.SetLevel(log.DebugLevel),
				log.SetMode(log.ModeDev))

			scheme := runtime.NewScheme()

			// Add the necessary scheme information for decoding objects
			for _, obj := range tc.objects {
				gvk := schema.GroupVersionKind{
					Group:   obj.GetObjectKind().GroupVersionKind().Group,
					Version: obj.GetObjectKind().GroupVersionKind().Version,
					Kind:    obj.GetObjectKind().GroupVersionKind().Kind,
				}
				scheme.AddKnownTypeWithName(gvk, obj)
			}

			ctx := logr.NewContext(context.Background(), logger)

			callback := func(obj runtime.Object) error {
				// Fake callback function to handle the decoded object,
				// perform validation and return an error if the object is invalid
				switch obj := obj.(type) {
				case *corev1.Pod, *appsv1.Deployment, *corev1.Service:
					// valid object types
				default:
					return fmt.Errorf("unexpected object type: %T", obj)
				}

				return nil
			}

			// Call the DecodeFile function with the provided context, file data, scheme, and callback
			err := DecodeFile(ctx, tc.fileData, scheme, callback)

			if (err != nil) != tc.expectErr {
				if tc.expectErr {
					t.Errorf("expected error, but got nil")
				} else {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if tc.expectLogs && logBuffer.Len() == 0 {
				t.Errorf("expected logs, but got none")
			}
			if !tc.expectLogs && logBuffer.Len() > 0 {
				t.Errorf("unexpected logs: %s", logBuffer.String())
			}
		})
	}
}

func TestDecodeFileDetailedValidation(t *testing.T) {
	t.Run("when decoding a valid Kubernetes object with detailed validation in callback then validate the object", func(t *testing.T) {
		fileData := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        ports:
        - containerPort: 80
`)

		logBuffer := bytes.Buffer{}
		logger := log.NewLogger(
			log.WriteTo(&logBuffer),
			log.SetLevel(log.DebugLevel),
			log.SetMode(log.ModeDev))

		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(schema.GroupVersion{
			Group:   "apps",
			Version: "v1",
		}, &appsv1.Deployment{})

		ctx := logr.NewContext(context.Background(), logger)

		callback := func(obj runtime.Object) error {
			deployment, ok := obj.(*appsv1.Deployment)
			if !ok {
				return fmt.Errorf("unexpected object type: %T", obj)
			}

			// Perform validations on the deployment object
			if deployment.Name != "example-deployment" {
				t.Errorf("unexpected deployment name: %s", deployment.Name)
			}
			if *deployment.Spec.Replicas != int32(3) {
				t.Errorf("unexpected number of replicas: %d", *deployment.Spec.Replicas)
			}
			if len(deployment.Spec.Template.Spec.Containers) != 1 {
				t.Errorf("unexpected number of containers: %d", len(deployment.Spec.Template.Spec.Containers))
			}
			if deployment.Spec.Template.Spec.Containers[0].Name != "nginx" {
				t.Errorf("unexpected container name: %s", deployment.Spec.Template.Spec.Containers[0].Name)
			}
			if deployment.Spec.Template.Spec.Containers[0].Image != "nginx:latest" {
				t.Errorf("unexpected container image: %s", deployment.Spec.Template.Spec.Containers[0].Image)
			}
			if deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != 80 {
				t.Errorf("unexpected container port: %d", deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
			}

			return nil
		}

		err := DecodeFile(ctx, fileData, scheme, callback)
		if (err != nil) != false {
			t.Errorf("unexpected error: %v", err)
		}

		if logBuffer.Len() > 0 {
			t.Errorf("unexpected logs: %s", logBuffer.String())
		}
	})
}
