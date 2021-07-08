// +build integration

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

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	discoveryv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

var OAS = `
openapi: "3.0.0"
info:
  title: "toy API"
  description: "toy API"
  version: "1.0.0"
servers:
  - url: http://toys/
paths:
  /toys:
    get:
      operationId: "getToys"
      responses:
        405:
          description: "invalid input"
`

var _ = Describe("Service controller", func() {
	BeforeEach(func() {
		namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		err := k8sClient.Delete(context.Background(), namespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if err != nil && apierrors.IsNotFound(err) {
			err = nil
		}
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNamespace}, &v1.Namespace{})
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}, 5*time.Minute, 5*time.Second).Should(BeTrue())

		// Add any setup steps that needs to be executed before each test
		err = k8sClient.Create(context.Background(), namespace)
		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNamespace}, existingNamespace)
			if err != nil {
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second).Should(BeTrue())
	})

	AfterEach(func() {
		desiredTestNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		// Add any teardown steps that needs to be executed after each test
		err := k8sClient.Delete(context.Background(), desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))

		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNamespace}, existingNamespace)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}, 5*time.Minute, 5*time.Second).Should(BeTrue())
	})

	// Test basic service deployment
	Context("Run with basic service deployment", func() {
		It("Should create API successfully", func() {
			const (
				retryInterval = time.Second * 5
				timeout       = time.Second * 60
				serviceName   = "myservice"
				oasCMName     = "cats-oas"
				apiName       = "cats"
				tag           = "production"
			)

			start := time.Now()

			oasConfigMap := oasConfigMapObject(oasCMName, testNamespace)
			err := k8sClient.Create(context.Background(), oasConfigMap)
			Expect(err).ToNot(HaveOccurred())

			serviceObj := serviceObject(serviceName, testNamespace, apiName, tag, oasCMName)
			err = k8sClient.Create(context.Background(), serviceObj)
			Expect(err).ToNot(HaveOccurred())

			// Currently API status is not implemented to check availability
			// Polling will be used
			apiObj := &discoveryv1beta1.API{}
			Eventually(func() bool {
				apiObjName := networkingv1beta1.APIObjectName(apiName, tag)
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: apiObjName, Namespace: testNamespace}, apiObj)
				if err != nil {
					return false
				}
				return true
			}, 5*time.Minute, 5*time.Second).Should(BeTrue())

			Expect(apiObj.Spec.Destination.Schema).Should(Equal("http"))
			Expect(apiObj.Spec.Destination.ServiceReference).ShouldNot(BeNil())
			Expect(apiObj.Spec.Destination.Name).Should(Equal(serviceName))
			Expect(apiObj.Spec.Destination.Namespace).Should(Equal(testNamespace))
			Expect(apiObj.Spec.Destination.Port).ShouldNot(BeNil())
			Expect(*apiObj.Spec.Destination.Port).Should(BeNumerically("==", 80))
			Expect(apiObj.Spec.Mappings.OAS).ShouldNot(BeNil())
			Expect(*apiObj.Spec.Mappings.OAS).Should(Equal(OAS))

			elapsed := time.Since(start)
			logf.Log.Info("e2e Service controller", "API creation and availability took", elapsed)
		})
	})
})

func serviceObject(name, ns, apiName, tag, oasName string) *v1.Service {
	return &v1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{KuadrantDiscoveryLabel: "true"},
			Annotations: map[string]string{
				KuadrantDiscoveryAnnotationScheme:       "http",
				KuadrantDiscoveryAnnotationAPIName:      apiName,
				KuadrantDiscoveryAnnotationTag:          tag,
				KuadrantDiscoveryAnnotationPort:         "80",
				KuadrantDiscoveryAnnotationOASConfigMap: oasName,
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{Port: 80, Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(3000)},
			},
			Selector: map[string]string{"svc": "cats"},
		},
	}
}

func oasConfigMapObject(name, ns string) *v1.ConfigMap {
	return &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: map[string]string{
			"openapi.yaml": OAS,
		},
	}
}
