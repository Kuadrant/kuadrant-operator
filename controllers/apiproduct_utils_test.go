// +build unit

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
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

func TestReplaceAPILabels(t *testing.T) {
	uid1 := "11111"
	uid2 := "22222"
	uid3 := "33333"
	uid4 := "44444"
	apiProduct := &networkingv1beta1.APIProduct{
		ObjectMeta: metav1.ObjectMeta{
			Name: "apiproduct01",
			Labels: map[string]string{
				"app":             "database",
				apiLabelKey(uid1): KuadrantAPILabelValue,
				apiLabelKey(uid2): KuadrantAPILabelValue,
				apiLabelKey(uid3): KuadrantAPILabelValue,
			},
		},
		Spec: networkingv1beta1.APIProductSpec{},
	}

	// missing api1 from apiproduct labels
	// api4 additional to existing apiproduct labels
	desiredAPIUIDs := []string{uid2, uid3, uid4}

	updated := replaceAPILabels(apiProduct, desiredAPIUIDs)

	if !updated {
		t.Error("update expected")
	}

	newLabels := apiProduct.GetLabels()

	expectedLabels := map[string]string{
		"app":             "database",
		apiLabelKey(uid2): KuadrantAPILabelValue,
		apiLabelKey(uid3): KuadrantAPILabelValue,
		apiLabelKey(uid4): KuadrantAPILabelValue,
	}

	if !reflect.DeepEqual(newLabels, expectedLabels) {
		t.Errorf("Resulting expected labels differ: %s", cmp.Diff(newLabels, expectedLabels))
	}
}
