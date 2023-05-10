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

package common

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TODO: move the const to a proper place, or get it from config
const (
	KuadrantRateLimitClusterName       = "kuadrant-rate-limiting-service"
	HTTPRouteKind                      = "HTTPRoute"
	RateLimitPoliciesBackRefAnnotation = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyBackRefAnnotation   = "kuadrant.io/ratelimitpolicy"
	AuthPoliciesBackRefAnnotation      = "kuadrant.io/authpolicies"
	AuthPolicyBackRefAnnotation        = "kuadrant.io/authpolicy"
	KuadrantNamespaceLabel             = "kuadrant.io/namespace"
	NamespaceSeparator                 = '/'
	LimitadorName                      = "limitador"
)

type KuadrantPolicy interface {
	client.Object
	GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference
	GetWrappedNamespace() gatewayapiv1alpha2.Namespace
	GetRulesHostnames() []string
}

// FetchEnv fetches the value of the environment variable with the specified key,
// or returns the default value if the variable is not found or has an empty value.
// If an error occurs during the lookup, the function returns the default value.
// The key and default value parameters must be valid strings.
func FetchEnv(key string, def string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}

	return val
}

// GetDefaultIfNil returns the value of a pointer argument, or a default value if the pointer is nil.
func GetDefaultIfNil[T any](val *T, def T) T {
	if reflect.ValueOf(val).IsNil() {
		return def
	}
	return *val
}

// GetEmptySliceIfNil returns a provided slice, or an empty slice of the same type if the input slice is nil.
func GetEmptySliceIfNil[T any](val []T) []T {
	if val == nil {
		return make([]T, 0)
	}
	return val
}

// NamespacedNameToObjectKey converts <namespace/name> format string to k8s object key.
// It's common for K8s to reference an object using this format. For e.g. gateways in VirtualService.
func NamespacedNameToObjectKey(namespacedName, defaultNamespace string) client.ObjectKey {
	if i := strings.IndexRune(namespacedName, '/'); i >= 0 {
		return client.ObjectKey{Namespace: namespacedName[:i], Name: namespacedName[i+1:]}
	}
	return client.ObjectKey{Namespace: defaultNamespace, Name: namespacedName}
}

// Contains checks if the given target string is present in the slice of strings 'slice'.
// It returns true if the target string is found in the slice, false otherwise.
func Contains(slice []string, target string) bool {
	for idx := range slice {
		if slice[idx] == target {
			return true
		}
	}
	return false
}

func Find[T any](slice []T, match func(T) bool) (*T, bool) {
	for _, item := range slice {
		if match(item) {
			return &item, true
		}
	}
	return nil, false
}

// Map applies the given mapper function to each element in the input slice and returns a new slice with the results.
func Map[T, U any](slice []T, f func(T) U) []U {
	arr := make([]U, len(slice))
	for i, e := range slice {
		arr[i] = f(e)
	}
	return arr
}

// SliceCopy copies the elements from the input slice into the output slice, and returns the output slice.
func SliceCopy[T any](s1 []T) []T {
	s2 := make([]T, len(s1))
	copy(s2, s1)
	return s2
}

// ReverseSlice creates a reversed copy of the input slice.
func ReverseSlice[T any](input []T) []T {
	inputLen := len(input)
	output := make([]T, inputLen)

	for i, n := range input {
		j := inputLen - i - 1

		output[j] = n
	}

	return output
}

// MergeMapStringString Merge desired into existing.
// Not Thread-Safe. Does it matter?
func MergeMapStringString(existing *map[string]string, desired map[string]string) bool {
	modified := false

	if *existing == nil {
		*existing = map[string]string{}
	}

	for k, v := range desired {
		if existingVal, ok := (*existing)[k]; !ok || v != existingVal {
			(*existing)[k] = v
			modified = true
		}
	}

	return modified
}

// UnMarshallLimitNamespace parses limit namespace with format "gwNS/gwName#domain"
func UnMarshallLimitNamespace(ns string) (client.ObjectKey, string, error) {
	split := strings.Split(ns, "#")
	if len(split) != 2 {
		return client.ObjectKey{}, "", errors.New("failed to split on #")
	}

	domain := split[1]

	gwKey, err := UnMarshallObjectKey(split[0])
	if err != nil {
		return client.ObjectKey{}, "", err
	}

	return gwKey, domain, nil
}

// MarshallNamespace serializes limit namespace with format "gwNS/gwName#domain"
func MarshallNamespace(gwKey client.ObjectKey, domain string) string {
	return fmt.Sprintf("%s/%s#%s", gwKey.Namespace, gwKey.Name, domain)
}

func UnMarshallObjectKey(keyStr string) (client.ObjectKey, error) {
	keySplit := strings.Split(keyStr, string(NamespaceSeparator))
	if len(keySplit) < 2 {
		return client.ObjectKey{}, fmt.Errorf("failed to split on %s: '%s'", string(NamespaceSeparator), keyStr)
	}

	return client.ObjectKey{Namespace: keySplit[0], Name: keySplit[1]}, nil
}

// HostnamesToStrings converts []gatewayapi_v1alpha2.Hostname to []string
func HostnamesToStrings(hostnames []gatewayapiv1alpha2.Hostname) []string {
	hosts := []string{}
	for idx := range hostnames {
		hosts = append(hosts, string(hostnames[idx]))
	}
	return hosts
}

// ValidSubdomains returns (true, "") when every single subdomains item
// is a subset of at least one of the domains.
// Domains and subdomains may be prefixed with a wildcard label (*.).
// The wildcard label must appear by itself as the first label.
// When one of the subdomains is not a subset of any of the domains, it returns false and
// the subdomain not being subset of any of the domains
func ValidSubdomains(domains, subdomains []string) (bool, string) {
	for _, subdomain := range subdomains {
		validSubdomain := false
		for _, domain := range domains {
			if Name(subdomain).SubsetOf(Name(domain)) {
				validSubdomain = true
				break
			}
		}

		if !validSubdomain {
			return false, subdomain
		}
	}
	return true, ""
}
