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
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TODO: move the const to a proper place, or get it from config
const (
	KuadrantNamespace                    = "kuadrant-system"
	KuadrantRateLimitClusterName         = "kuadrant-rate-limiting-service"
	HTTPRouteKind                        = "HTTPRoute"
	KuadrantRateLimitPolicyRefAnnotation = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyBackRefAnnotation     = "kuadrant.io/ratelimitpolicy-direct-backref"
	AuthPolicyBackRefAnnotation          = "kuadrant.io/authpolicy-backref"
	NamespaceSeparator                   = '/'
)

func FetchEnv(key string, def string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}

	return val
}

// NamespacedNameToObjectKey converts <namespace/name> format string to k8s object key.
// It's common for K8s to reference an object using this format. For e.g. gateways in VirtualService.
func NamespacedNameToObjectKey(namespacedName, defaultNamespace string) client.ObjectKey {
	split := strings.Split(namespacedName, "/")
	if len(split) == 2 {
		return client.ObjectKey{Name: split[1], Namespace: split[0]}
	}
	return client.ObjectKey{Namespace: defaultNamespace, Name: split[0]}
}

func Contains(slice []string, target string) bool {
	for idx := range slice {
		if slice[idx] == target {
			return true
		}
	}
	return false
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

// converts []gatewayapi_v1alpha2.Hostname to []string
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
