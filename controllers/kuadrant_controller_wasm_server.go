/*
Copyright 2021.

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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	WASMServerImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "quay.io/kuadrant/wasm-server:latest")
)

func (r *KuadrantReconciler) reconcileWasmServer(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	if err := r.reconcileWamsServerDeployment(ctx, kObj); err != nil {
		return err
	}

	if err := r.reconcileWamsServerService(ctx, kObj); err != nil {
		return err
	}

	return r.reconcileWamsServerConfigMap(ctx, kObj)
}

func WasmServerConfigMapName(kObj *kuadrantv1beta1.Kuadrant) string {
	return fmt.Sprintf("wasm-server-%s", kObj.Name)
}

func WasmServerServiceName(kObj *kuadrantv1beta1.Kuadrant) string {
	return fmt.Sprintf("wasm-server-%s", kObj.Name)
}

func WasmServerDeploymentName(kObj *kuadrantv1beta1.Kuadrant) string {
	return fmt.Sprintf("wasm-server-%s", kObj.Name)
}

func WasmServerLabels() map[string]string {
	return map[string]string{
		"kuadrant_component_element": "wasm-server",
		"app":                        "nginx",
	}
}

func (r *KuadrantReconciler) reconcileWamsServerConfigMap(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	desiredDeployment := wasmServerDeployment(kObj)

	deployment := &appsv1.Deployment{}
	if err := r.Client().Get(ctx, client.ObjectKeyFromObject(desiredDeployment), deployment); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("wasm-server deployment not found yet. waiting",
				"key", client.ObjectKeyFromObject(desiredDeployment))
			return nil
		}
		return err
	}

	if deployment.Generation != deployment.Status.ObservedGeneration {
		logger.Info("wasm-server deployment updating and generation not ready yet. waiting",
			"key", client.ObjectKeyFromObject(desiredDeployment))
		return nil
	}

	availableCondition := utils.FindDeploymentStatusCondition(
		deployment.Status.Conditions, string(appsv1.DeploymentAvailable),
	)
	if availableCondition == nil {
		logger.Info("wasm-server deployment Available condition not found. waiting",
			"key", client.ObjectKeyFromObject(desiredDeployment))
		return nil
	}

	if availableCondition.Status != corev1.ConditionTrue {
		logger.Info("wasm-server deployment not available yet. waiting",
			"key", client.ObjectKeyFromObject(desiredDeployment),
			"message", availableCondition.Message,
		)
		return nil
	}

	podList := &corev1.PodList{}

	err = r.Client().List(ctx, podList, client.InNamespace(deployment.Namespace), client.MatchingLabels(deployment.Spec.Template.Labels))
	if err != nil {
		return err
	}

	if len(podList.Items) == 0 {
		logger.Info("wasm-server pods not found yet. waiting",
			"key", client.ObjectKeyFromObject(desiredDeployment))
		return nil
	}

	pod := podList.Items[0]

	podLogOpts := corev1.PodLogOptions{Container: "compute-rate-limit-wasm-sha256"}

	config, err := config.GetConfig()
	//config, err := rest.InClusterConfig()
	if err != nil {
		logger.V(1).Info("wasm-server: error in getting config")
		return err
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.V(1).Info("wasm-server: error in getting access to K8S")
		return err
	}

	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		logger.V(1).Info("wasm-server: error in opening stream")
		return err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		logger.V(1).Info("wasm-server: error in copy information from podLogs to buf")
		return err
	}
	wasmSha256 := strings.TrimSuffix(buf.String(), "\n")

	logger.V(1).Info("wasm-server: got rate limit wasm sha256", "sha256", wasmSha256,
		"image", WASMServerImageURL)

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WasmServerConfigMapName(kObj),
			Namespace: kObj.Namespace,
			Labels:    WasmServerLabels(),
		},
		Data: map[string]string{
			"rate-limit-wasm-sha256": wasmSha256,
		},
	}

	// controller reference
	if err := r.SetOwnerReference(kObj, configMap); err != nil {
		logger.V(1).Info("set ownerref", "error", err)
		return err
	}

	configMapMutator := reconcilers.ConfigMapMutator(func(desired, existing *corev1.ConfigMap) bool {
		return reconcilers.ConfigMapReconcileField(desired, existing, "rate-limit-wasm-sha256")
	})

	err = r.ReconcileResource(ctx, &corev1.ConfigMap{}, configMap, configMapMutator)
	logger.V(1).Info("reconcile configmap", "error", err)
	if err != nil {
		return err
	}

	return nil
}

func (r *KuadrantReconciler) reconcileWamsServerService(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WasmServerServiceName(kObj),
			Namespace: kObj.Namespace,
			Labels:    WasmServerLabels(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromString("http"),
				},
			},
			Selector: WasmServerLabels(),
			Type:     corev1.ServiceTypeClusterIP,
		},
	}

	// controller reference
	if err := r.SetOwnerReference(kObj, service); err != nil {
		logger.V(1).Info("set ownerref", "error", err)
		return err
	}

	serviceMutator := reconcilers.ServiceMutator(reconcilers.ServicePortsMutator)

	err = r.ReconcileResource(ctx, &corev1.Service{}, service, serviceMutator)
	logger.V(1).Info("reconcile service", "error", err)
	if err != nil {
		return err
	}

	return nil
}

func wasmServerDeployment(kObj *kuadrantv1beta1.Kuadrant) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WasmServerDeploymentName(kObj),
			Namespace: kObj.Namespace,
			Labels:    WasmServerLabels(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: WasmServerLabels(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: WasmServerLabels(),
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "compute-rate-limit-wasm-sha256",
							Image:           WASMServerImageURL,
							Command:         []string{"cat", "/data/plugin.wasm.sha256"},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "wasm-server",
							Image: WASMServerImageURL,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/health",
										Port:   intstr.FromInt(80),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      2,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/health",
										Port:   intstr.FromInt(80),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 5,
								TimeoutSeconds:      5,
								PeriodSeconds:       10,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}
}

func (r *KuadrantReconciler) reconcileWamsServerDeployment(ctx context.Context, kObj *kuadrantv1beta1.Kuadrant) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	deployment := wasmServerDeployment(kObj)

	// controller reference
	err = r.SetOwnerReference(kObj, deployment)
	if err != nil {
		logger.V(1).Info("set ownerref", "error", err)
		return err
	}

	customDeploymentImageMutator := func(desired, existing *appsv1.Deployment) bool {
		update := false

		if existing.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
			existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
			update = true
		}

		if existing.Spec.Template.Spec.InitContainers[0].Image != desired.Spec.Template.Spec.InitContainers[0].Image {
			existing.Spec.Template.Spec.InitContainers[0].Image = desired.Spec.Template.Spec.InitContainers[0].Image
			update = true
		}

		return update
	}

	deploymentMutators := []reconcilers.DeploymentMutateFn{
		reconcilers.DeploymentContainerListMutator,
		customDeploymentImageMutator,
		reconcilers.DeploymentPortsMutator,
		reconcilers.DeploymentLivenessProbeMutator,
	}

	err = r.ReconcileResource(ctx, &appsv1.Deployment{}, deployment, reconcilers.DeploymentMutator(deploymentMutators...))
	logger.V(1).Info("reconcile deployment", "error", err)
	if err != nil {
		return err
	}

	return nil
}
