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
	"strings"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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
	DNSPoliciesBackRefAnnotation       = "kuadrant.io/dnspolicies"
	DNSPolicyBackRefAnnotation         = "kuadrant.io/dnspolicy"
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
	return utils.Map(hostnames, func(hostname gatewayapiv1.Hostname) string {
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
		if _, found := utils.Find(domains, func(domain gatewayapiv1.Hostname) bool {
			return Name(subsubdomain).SubsetOf(Name(domain))
		}); found {
			arr = append(arr, subsubdomain)
		}
	}
	return arr
}
