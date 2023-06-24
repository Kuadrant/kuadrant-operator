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
	"fmt"
	"os"
	"reflect"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
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
	GetWrappedNamespace() gatewayapiv1beta1.Namespace
	GetRulesHostnames() []string
}

func Ptr[T any](t T) *T {
	return &t
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
func Contains[T comparable](slice []T, target T) bool {
	for idx := range slice {
		if slice[idx] == target {
			return true
		}
	}
	return false
}

// SameElements checks if the two slices contain the exact same elements. Order does not matter.
func SameElements[T comparable](s1, s2 []T) bool {
	if len(s1) != len(s2) {
		return false
	}
	for _, v := range s1 {
		if !Contains(s2, v) {
			return false
		}
	}
	return true
}

func Intersect[T comparable](slice1, slice2 []T) bool {
	for _, item := range slice1 {
		if Contains(slice2, item) {
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

func MapValues[T comparable, U any](m map[T]U) []U {
	values := make([]U, len(m))
	i := 0
	for k := range m {
		values[i] = m[k]
		i++
	}
	return values
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
	delimIndex := strings.IndexRune(ns, '#')
	if delimIndex == -1 {
		return client.ObjectKey{}, "", fmt.Errorf("failed to split on #")
	}

	gwSplit := ns[:delimIndex]
	domain := ns[delimIndex+1:]

	objKey, err := UnMarshallObjectKey(gwSplit)
	if err != nil {
		return client.ObjectKey{}, "", err
	}

	return objKey, domain, nil
}

// MarshallNamespace serializes limit namespace with format "gwNS/gwName#domain"
func MarshallNamespace(gwKey client.ObjectKey, domain string) string {
	return fmt.Sprintf("%s/%s#%s", gwKey.Namespace, gwKey.Name, domain)
}

// UnMarshallObjectKey takes a string input and converts it into an ObjectKey struct that
// can be used to access a specific Kubernetes object. The input string is expected to be in the format "namespace/name".
// If the input string does not contain a NamespaceSeparator (typically '/')
// or has too few components, this function returns an error.
func UnMarshallObjectKey(keyStr string) (client.ObjectKey, error) {
	namespaceEndIndex := strings.IndexRune(keyStr, NamespaceSeparator)
	if namespaceEndIndex < 0 {
		return client.ObjectKey{}, fmt.Errorf(fmt.Sprintf("failed to split on %s: '%s'", string(NamespaceSeparator), keyStr))
	}

	return client.ObjectKey{Namespace: keyStr[:namespaceEndIndex], Name: keyStr[namespaceEndIndex+1:]}, nil
}

// HostnamesToStrings converts []gatewayapi_v1alpha2.Hostname to []string
func HostnamesToStrings(hostnames []gatewayapiv1beta1.Hostname) []string {
	hosts := make([]string, len(hostnames))
	for i, h := range hostnames {
		hosts[i] = string(h)
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
