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
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/martinlindhe/base36"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// TODO: move the const to a proper place, or get it from config
const (
	KuadrantRateLimitClusterName = "kuadrant-rate-limiting-service"
	AuthPolicyBackRefAnnotation  = "kuadrant.io/authpolicy"
	NamespaceSeparator           = '/'
	LimitadorName                = "limitador"
)

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

// FilterValidSubdomains returns every subdomain that is a subset of at least one of the (super) domains specified in the first argument.
func FilterValidSubdomains(domains, subdomains []gatewayapiv1.Hostname) []gatewayapiv1.Hostname {
	arr := make([]gatewayapiv1.Hostname, 0)
	for _, subsubdomain := range subdomains {
		if _, found := utils.Find(domains, func(domain gatewayapiv1.Hostname) bool {
			return utils.Name(subsubdomain).SubsetOf(utils.Name(domain))
		}); found {
			arr = append(arr, subsubdomain)
		}
	}
	return arr
}

func ToBase36Hash(s string) string {
	hash := sha256.Sum224([]byte(s))
	// convert the hash to base36 (alphanumeric) to decrease collision probabilities
	return strings.ToLower(base36.EncodeBytes(hash[:]))
}

func ToBase36HashLen(s string, l int) string {
	return ToBase36Hash(s)[:l]
}
