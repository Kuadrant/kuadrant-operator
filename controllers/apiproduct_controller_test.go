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
	"net/http"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

var _ = Describe("APIPRoduct controller", func() {
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

	Context("Run directly without existing APIPRoduct", func() {
		It("Should create successfully", func() {
			Expect(1).To(Equal(1))
		})
	})

	// Test basic APIPRoduct deployment
	Context("Run with basic APIPRoduct deployment", func() {
		It("Should create successfully", func() {
			const (
				retryInterval = time.Second * 5
				timeout       = time.Second * 60
			)

			start := time.Now()

			// Ingress Provider: Istio
			err := ApplyResources(filepath.Join("..", "utils", "local-deployment", "istio-manifests", "Base", "Base.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "utils", "local-deployment", "istio-manifests", "Base", "Pilot", "Pilot.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "utils", "local-deployment", "istio-manifests", "Base", "Pilot", "IngressGateways", "IngressGateways.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "utils", "local-deployment", "istio-manifests", "default-gateway.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "utils", "local-deployment", "authorino.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "samples", "secret.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())
			err = ApplyResources(filepath.Join("..", "samples", "api1.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err := CheckForDeploymentsReady(testNamespace, k8sClient)
				if err != nil {
					logf.Log.Info("Waiting for full availability", "err", err)
				}
				return err == nil
			}, 5*time.Minute, 5*time.Second).Should(BeTrue())

			// Create APIPRoduct
			apiProduct := apiProduct(testNamespace)
			err = k8sClient.Create(context.Background(), apiProduct)
			Expect(err).ToNot(HaveOccurred())

			// Currently APIProduct status is not implemented to check availability
			// Polling will be used
			Eventually(func() bool {
				httpClient := &http.Client{}
				req, err := http.NewRequest("GET", "http://127.0.0.1:9080/cats/toys", nil)
				if err != nil {
					logf.Log.Info("Error creating HTTP request", "error", err)
					return false
				}
				// Host defined in APIProduct spec
				req.Host = "petstore.127.0.0.1.nip.io"
				// api key defined in the secret
				req.Header.Add("Authorization", "APIKEY JUSTFORDEMOSOBVIOUSLYqDQsqSPMHkRhriEOtcRx")
				resp, err := httpClient.Do(req)
				if err != nil {
					logf.Log.Info("Error on HTTP request", "error", err)
					return false
				}
				if resp.StatusCode != 200 {
					logf.Log.Info("Expecting HTTP response status code 200", "received status code", resp.StatusCode)
					return false
				}

				return true
			}, 5*time.Minute, 5*time.Second).Should(BeTrue())

			elapsed := time.Since(start)
			logf.Log.Info("e2e APIProduct", "APIProduct creation and availability took", elapsed)
		})
	})
})

func apiProduct(ns string) *networkingv1beta1.APIProduct {
	return &networkingv1beta1.APIProduct{
		ObjectMeta: metav1.ObjectMeta{Name: "apiproduct01", Namespace: ns},
		Spec: networkingv1beta1.APIProductSpec{
			Information: networkingv1beta1.ProductInformation{
				Description: "My super nice API Product",
				Owner:       "whoever@mycompany.com",
			},
			Routing: networkingv1beta1.Routing{
				Hosts: []string{"petstore.127.0.0.1.nip.io"},
			},
			APIs: []*networkingv1beta1.APISelector{
				{
					Name:      "cats",
					Namespace: ns,
					Tag:       "production",
					Mapping: networkingv1beta1.Mapping{
						Prefix: "/cats",
					},
				},
			},
			SecurityScheme: []*networkingv1beta1.SecurityScheme{
				{
					Name: "MyAPIKey",
					APIKeyAuth: &networkingv1beta1.APIKeyAuth{
						Location: "authorization_header",
						Name:     "APIKEY",
						CredentialSource: networkingv1beta1.APIKeyAuthCredentials{
							LabelSelectors: map[string]string{
								"secret.kuadrant.io/managed-by": "authorino",
								"api":                           "animaltoys",
							},
						},
					},
				},
			},
		},
	}
}
