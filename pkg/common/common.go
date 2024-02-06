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
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TODO: move the const to a proper place, or get it from config
const (
	KuadrantRateLimitClusterName       = "kuadrant-rate-limiting-service"
	RateLimitPoliciesBackRefAnnotation = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyBackRefAnnotation   = "kuadrant.io/ratelimitpolicy"
	AuthPoliciesBackRefAnnotation      = "kuadrant.io/authpolicies"
	AuthPolicyBackRefAnnotation        = "kuadrant.io/authpolicy"
	TLSPoliciesBackRefAnnotation       = "kuadrant.io/tlspolicies"
	TLSPolicyBackRefAnnotation         = "kuadrant.io/tlspolicy"
	KuadrantNamespaceLabel             = "kuadrant.io/namespace"
	NamespaceSeparator                 = '/'
	LimitadorName                      = "limitador"
)

type KuadrantPolicy interface {
	client.Object
	GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference
	GetWrappedNamespace() gatewayapiv1.Namespace
	GetRulesHostnames() []string
	Kind() string
}

type KuadrantPolicyList interface {
	GetItems() []KuadrantPolicy
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

// SameElements checks if the two slices contain the exact same elements. Order does not matter.
func SameElements[T comparable](s1, s2 []T) bool {
	if len(s1) != len(s2) {
		return false
	}
	for _, v := range s1 {
		if !slices.Contains(s2, v) {
			return false
		}
	}
	return true
}

func Intersect[T comparable](slice1, slice2 []T) bool {
	for _, item := range slice1 {
		if slices.Contains(slice2, item) {
			return true
		}
	}
	return false
}

func Intersection[T comparable](slice1, slice2 []T) []T {
	smallerSlice := slice1
	largerSlice := slice2
	if len(slice1) > len(slice2) {
		smallerSlice = slice2
		largerSlice = slice1
	}
	var result []T
	for _, item := range smallerSlice {
		if slices.Contains(largerSlice, item) {
			result = append(result, item)
		}
	}
	return result
}

func Find[T any](slice []T, match func(T) bool) (*T, bool) {
	for _, item := range slice {
		if match(item) {
			return &item, true
		}
	}
	return nil, false
}

func Contains[T any](slice []T, match func(T) bool) bool {
	_, ok := Find(slice, match)
	return ok
}

// Map applies the given mapper function to each element in the input slice and returns a new slice with the results.
func Map[T, U any](slice []T, f func(T) U) []U {
	arr := make([]U, len(slice))
	for i, e := range slice {
		arr[i] = f(e)
	}
	return arr
}

// Filter filters the input slice using the given predicate function and returns a new slice with the results.
func Filter[T any](slice []T, f func(T) bool) []T {
	arr := make([]T, 0)
	for _, e := range slice {
		if f(e) {
			arr = append(arr, e)
		}
	}
	return arr
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

// HostnamesToStrings converts []gatewayapiv1.Hostname to []string
func HostnamesToStrings(hostnames []gatewayapiv1.Hostname) []string {
	return Map(hostnames, func(hostname gatewayapiv1.Hostname) string {
		return string(hostname)
	})
}

// ValidSubdomains returns (true, "") when every single subdomains item
// is a subset of at least one of the domains.
// Domains and subdomains may be prefixed with a wildcard label (*.).
// The wildcard label must appear by itself as the first label.
// When one of the subdomains is not a subset of the domains, it returns false and
// the subdomain not being subset of the domains
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

// FilterValidSubdomains returns every subdomain that is a subset of at least one of the (super) domains specified in the first argument.
func FilterValidSubdomains(domains, subdomains []gatewayapiv1.Hostname) []gatewayapiv1.Hostname {
	arr := make([]gatewayapiv1.Hostname, 0)
	for _, subsubdomain := range subdomains {
		if _, found := Find(domains, func(domain gatewayapiv1.Hostname) bool {
			return Name(subsubdomain).SubsetOf(Name(domain))
		}); found {
			arr = append(arr, subsubdomain)
		}
	}
	return arr
}
