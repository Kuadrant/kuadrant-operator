/*
Copyright 2025 Red Hat, Inc.

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

package extension

import (
	"reflect"

	"github.com/samber/lo"
)

const (
	TriggerTimeAnnotation   = "extensions.kuadrant.io/trigger-time"
	TriggerReasonAnnotation = "extensions.kuadrant.io/trigger-reason"
)

func AnnotationsChanged(oldAnnotations, newAnnotations map[string]string) bool {
	extensionKeys := []string{
		TriggerTimeAnnotation,
		TriggerReasonAnnotation,
	}

	oldExtensionAnnotations := lo.PickByKeys(oldAnnotations, extensionKeys)
	newExtensionAnnotations := lo.PickByKeys(newAnnotations, extensionKeys)

	return !reflect.DeepEqual(oldExtensionAnnotations, newExtensionAnnotations)
}
