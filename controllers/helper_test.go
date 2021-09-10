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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func ApplyResources(fileName string, k8sClient client.Client, ns string) error {
	logf.Log.Info("ApplyResources", "Resource file", fileName)
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	codec := serializer.NewCodecFactory(scheme.Scheme)
	decoder := codec.UniversalDeserializer()

	// the maximum size used to buffer a doc 5M
	buf := make([]byte, 5*1024*1024)
	docDecoder := yaml.NewDocumentDecoder(ioutil.NopCloser(bytes.NewReader(data)))

	for {
		n, err := docDecoder.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		if n == 0 {
			// empty docs
			continue
		}

		docData := buf[:n]
		obj, _, err := decoder.Decode(docData, nil, nil)
		if err != nil {
			logf.Log.Info("Document decode error", "error", err)
			continue
		}

		metadata, err := meta.Accessor(obj)
		if err != nil {
			return err
		}
		metadata.SetNamespace(ns)

		err = CreateOrUpdateK8SObject(obj, k8sClient)
		if err != nil {
			return err
		}
	}
	return nil
}

// IsDeploymentAvailable returns true when the provided Deployment
// has the "Available" condition set to true
func IsDeploymentConfigAvailable(dc *appsv1.Deployment) bool {
	dcConditions := dc.Status.Conditions
	for _, dcCondition := range dcConditions {
		if dcCondition.Type == appsv1.DeploymentAvailable && dcCondition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func CreateOrUpdateK8SObject(obj runtime.Object, k8sClient client.Client) error {
	k8sObj, ok := obj.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}
	logf.Log.Info("CreateOrUpdateK8SObject", "GKV", k8sObj.GetObjectKind(), "name", k8sObj.GetName())

	err := k8sClient.Create(context.Background(), k8sObj)
	if err == nil {
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	// Already exists
	currentObj := k8sObj.DeepCopyObject()
	k8sCurrentObj, ok := currentObj.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObj), k8sCurrentObj)
	if err != nil {
		return err
	}

	objCopy := k8sObj.DeepCopyObject()

	objCopyMetadata, err := meta.Accessor(objCopy)
	if err != nil {
		return err
	}

	objCopyMetadata.SetResourceVersion(k8sCurrentObj.GetResourceVersion())

	k8sObjCopy, ok := objCopy.(client.Object)
	if !ok {
		return errors.New("runtime.Object could not be casted to client.Object")
	}

	return k8sClient.Update(context.Background(), k8sObjCopy)
}

func WaitForDeploymentsReady(k8sClient client.Client, keys ...client.ObjectKey) error {
	for idx := range keys {
		Eventually(func() bool {
			existingDeployment := &appsv1.Deployment{}
			err := k8sClient.Get(context.Background(), keys[idx], existingDeployment)
			if err != nil {
				if apierrors.IsNotFound(err) {
					fmt.Fprintf(GinkgoWriter, "Deployment %s available\n", keys[idx].Name)
				} else {
					fmt.Fprintf(GinkgoWriter, "error reading deployments: %s\n", err.Error())
				}
				return false
			}

			if !IsDeploymentConfigAvailable(existingDeployment) {
				fmt.Fprintf(GinkgoWriter, "Deployment %s. Available: %d. Desired: %d", existingDeployment.Name, existingDeployment.Status.AvailableReplicas, *existingDeployment.Spec.Replicas)
				return false
			}

			fmt.Fprintf(GinkgoWriter, "Deployment %s available", existingDeployment.Name)
			return true
		}, 5*time.Minute, 5*time.Second).Should(BeTrue())
	}

	return nil
}
