package openshift

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	ImageDigestMirrorSetGVK = schema.GroupVersionKind{
		Group:   configv1.GroupName,
		Version: configv1.GroupVersion.Version,
		Kind:    "ImageDigestMirrorSet",
	}
	ImageDigestMirrorSetResource = configv1.SchemeGroupVersion.WithResource("imagedigestmirrorsets")

	ImageTagMirrorSetGVK = schema.GroupVersionKind{
		Group:   configv1.GroupName,
		Version: configv1.GroupVersion.Version,
		Kind:    "ImageTagMirrorSet",
	}
	ImageTagMirrorSetResource = configv1.SchemeGroupVersion.WithResource("imagetagmirrorsets")

	ImageContentPolicyGVK = schema.GroupVersionKind{
		Group:   configv1.GroupName,
		Version: configv1.GroupVersion.Version,
		Kind:    "ImageContentPolicy",
	}
	ImageContentPolicyResource = configv1.SchemeGroupVersion.WithResource("imagecontentpolicies")
)

func IsImageDigestMirrorSetInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ImageDigestMirrorSetGVK.Group, ImageDigestMirrorSetGVK.Kind, ImageDigestMirrorSetGVK.Version)
}

func IsImageTagMirrorSetInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ImageTagMirrorSetGVK.Group, ImageTagMirrorSetGVK.Kind, ImageTagMirrorSetGVK.Version)
}

func IsImageContentPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ImageContentPolicyGVK.Group, ImageContentPolicyGVK.Kind, ImageContentPolicyGVK.Version)
}

// pullType distinguishes digest-based from tag-based image references.
// IDMS rules only apply to digest references, ITMS rules only to tag references,
// matching the semantics enforced by CRI-O via the pull-from-mirror field in registries.conf.
type pullType int

const (
	pullTypeDigest pullType = iota
	pullTypeTag
)

type mirrorRule struct {
	source   string
	mirrors  []string
	pullType pullType
}

// ResolveImageURL resolves a container image URL through OpenShift mirror configurations.
//
// In disconnected (air-gapped) clusters, images are served from internal mirror registries
// instead of their original source registries. OpenShift exposes this mapping via three
// cluster-scoped CRDs:
//   - ImageDigestMirrorSet (IDMS) — config.openshift.io/v1, for digest-based pulls
//   - ImageTagMirrorSet (ITMS) — config.openshift.io/v1, for tag-based pulls
//   - ImageContentPolicy (ICP) — config.openshift.io/v1, legacy equivalent of ICSP (digest-only)
//
// This function collects mirror rules from all three sources, finds the most specific
// source prefix match, and rewrites the image URL to point to the mirror. This is
// necessary because some components (e.g., Istio, Envoy Gateway) pull images directly
// and do not honor OpenShift's node-level mirror configuration (registries.conf / CRI-O).
//
// The resolution logic follows the prefix-matching semantics used by:
//   - containers/image: pkg/sysregistriesv2 (refMatchingPrefix, refMatchingSubdomainPrefix)
//   - openshift/oc: pkg/cli/image/strategy (alternativeImageSourcesIDMS)
//   - openshift/runtime-utils: pkg/registries (EditRegistriesConfig)
//
// Supports both exact prefix sources (e.g., "registry.redhat.io/kuadrant") and wildcard
// subdomain sources (e.g., "*.redhat.io") as defined in the IDMS/ITMS CRD specs.
// IDMS rules are only applied to digest references (@sha256:...), ITMS rules only to tag
// references (:tag), matching the pull-from-mirror semantics that CRI-O enforces.
//
// Returns the original URL unchanged if no mirrors match or on non-OpenShift clusters.
func ResolveImageURL(ctx context.Context, client dynamic.Interface, imageURL string, isIDMSInstalled, isITMSInstalled, isICPInstalled bool, logger logr.Logger) string {
	var rules []mirrorRule

	if isIDMSInstalled {
		idmsRules, err := collectIDMSRules(ctx, client)
		if err != nil {
			logger.V(1).Info("failed to list ImageDigestMirrorSets", "error", err)
		} else {
			rules = append(rules, idmsRules...)
		}
	}

	if isITMSInstalled {
		itmsRules, err := collectITMSRules(ctx, client)
		if err != nil {
			logger.V(1).Info("failed to list ImageTagMirrorSets", "error", err)
		} else {
			rules = append(rules, itmsRules...)
		}
	}

	if isICPInstalled {
		icpRules, err := collectICPRules(ctx, client)
		if err != nil {
			logger.V(1).Info("failed to list ImageContentPolicies", "error", err)
		} else {
			rules = append(rules, icpRules...)
		}
	}

	if len(rules) == 0 {
		return imageURL
	}

	resolved := resolveFromMirrors(imageURL, rules)
	if resolved != imageURL {
		logger.Info("resolved image via mirror", "original", imageURL, "resolved", resolved)
	}
	return resolved
}

// resolveFromMirrors finds the best mirror for an image URL using most-specific-prefix matching.
//
// The matching follows the containers/image registries.conf semantics
// (refMatchingPrefix / refMatchingSubdomainPrefix):
//   - Exact prefix sources: the source must be a prefix of the image URL, and the match
//     must align on a repository boundary ('/', ':', '@') to prevent partial hostname
//     matches (e.g., "registry.io" must not match "registry.io.evil.com").
//   - Wildcard sources ("*.example.com"): match any subdomain of example.com. The wildcard
//     is replaced with the actual hostname when rewriting, following the IDMS/ITMS spec:
//     "the mirrored location is obtained by replacing the part of the input reference that
//     matches source by the mirrors entry."
//
// The source with the longest matching prefix wins. The first mirror in the winning rule
// is used (mirrors are ordered by admin-specified priority in the CRD).
//
// IDMS and ICP rules (pullTypeDigest) are only applied to digest references (@sha256:...).
// ITMS rules (pullTypeTag) are only applied to tag references (:tag).
// This matches the pull-from-mirror semantics enforced by CRI-O at the node level.
func resolveFromMirrors(imageURL string, rules []mirrorRule) string {
	isDigest := strings.Contains(imageURL, "@")

	var bestMatch mirrorRule
	bestLen := 0
	// Wildcard matches are scored lower than exact prefix matches of the same specificity,
	// so we track whether the best match is a wildcard to allow exact matches to win.
	bestIsWildcard := false

	for _, rule := range rules {
		if len(rule.mirrors) == 0 {
			continue
		}

		// IDMS/ICP rules only apply to digest refs, ITMS rules only to tag refs.
		if rule.pullType == pullTypeDigest && !isDigest {
			continue
		}
		if rule.pullType == pullTypeTag && isDigest {
			continue
		}

		source := strings.TrimRight(rule.source, "/")

		if strings.HasPrefix(source, "*.") {
			matchLen := matchWildcard(imageURL, source)
			if matchLen > 0 && (matchLen > bestLen || (matchLen == bestLen && bestIsWildcard)) {
				bestLen = matchLen
				bestMatch = rule
				bestIsWildcard = true
			}
			continue
		}

		matchLen := matchPrefix(imageURL, source)
		if matchLen > 0 && (matchLen > bestLen || (matchLen == bestLen && bestIsWildcard)) {
			bestLen = matchLen
			bestMatch = rule
			bestIsWildcard = false
		}
	}

	if bestLen == 0 {
		return imageURL
	}

	mirror := strings.TrimRight(bestMatch.mirrors[0], "/")
	source := strings.TrimRight(bestMatch.source, "/")

	if strings.HasPrefix(source, "*.") {
		return rewriteWildcard(imageURL, source, mirror)
	}

	return mirror + imageURL[len(source):]
}

// matchPrefix checks if imageURL starts with the given source on a repository boundary.
// Returns the match length, or 0 if no match.
//
// Boundary characters ('/', ':', '@') prevent partial hostname matches.
// This matches the behavior of containers/image's refMatchingPrefix().
func matchPrefix(imageURL, source string) int {
	if !strings.HasPrefix(imageURL, source) {
		return 0
	}
	if len(imageURL) == len(source) {
		return len(source)
	}
	next := imageURL[len(source)]
	if next == '/' || next == ':' || next == '@' {
		return len(source)
	}
	return 0
}

// matchWildcard checks if imageURL matches a wildcard source like "*.example.com".
// The wildcard matches any single hostname that is a subdomain of the domain suffix.
// Returns the match length (hostname portion length), or 0 if no match.
//
// This follows containers/image's refMatchingSubdomainPrefix() semantics:
// "*.example.com" matches "sub.example.com/foo" but not "example.com/foo"
// or "sub.sub.example.com/foo" (only single-level subdomain).
//
// Per the IDMS CRD spec, the format is "[*.]host" where the wildcard matches subdomains.
func matchWildcard(imageURL, wildcardSource string) int {
	// "*.example.com" -> ".example.com"
	domainSuffix := wildcardSource[1:]

	// Find the hostname portion of the image URL (everything before the first '/')
	hostname := imageURL
	pathStart := strings.IndexByte(imageURL, '/')
	if pathStart >= 0 {
		hostname = imageURL[:pathStart]
	}

	// Strip port from hostname for matching
	hostnameWithoutPort := hostname
	if portIdx := strings.LastIndexByte(hostname, ':'); portIdx >= 0 {
		hostnameWithoutPort = hostname[:portIdx]
	}

	// The hostname must end with the domain suffix (e.g., "sub.example.com" ends with ".example.com")
	if !strings.HasSuffix(hostnameWithoutPort, domainSuffix) {
		return 0
	}

	// The part before the suffix must not contain dots (single-level subdomain only)
	prefix := hostnameWithoutPort[:len(hostnameWithoutPort)-len(domainSuffix)]
	if len(prefix) == 0 || strings.ContainsRune(prefix, '.') {
		return 0
	}

	return len(hostname)
}

// rewriteWildcard rewrites an image URL matched by a wildcard source.
//
// Per the IDMS/ITMS spec: "the mirrored location is obtained by replacing the part of the
// input reference that matches source by the mirrors entry, e.g. for
// registry.redhat.io/product/repo reference, a (source, mirror) pair *.redhat.io,
// mirror.local/redhat causes a mirror.local/redhat/product/repo repository to be used."
//
// The wildcard source's domain suffix is replaced with the mirror, preserving the path.
func rewriteWildcard(imageURL, wildcardSource, mirror string) string {
	// Find where the hostname ends (first '/')
	pathStart := strings.IndexByte(imageURL, '/')
	if pathStart < 0 {
		return mirror
	}
	return mirror + imageURL[pathStart:]
}

func collectIDMSRules(ctx context.Context, client dynamic.Interface) ([]mirrorRule, error) {
	list, err := client.Resource(ImageDigestMirrorSetResource).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var rules []mirrorRule
	for _, item := range list.Items {
		var idms configv1.ImageDigestMirrorSet
		if err := fromUnstructured(item.Object, &idms); err != nil {
			continue
		}
		for _, m := range idms.Spec.ImageDigestMirrors {
			mirrors := make([]string, len(m.Mirrors))
			for i, mirror := range m.Mirrors {
				mirrors[i] = string(mirror)
			}
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors, pullType: pullTypeDigest})
		}
	}
	return rules, nil
}

func collectITMSRules(ctx context.Context, client dynamic.Interface) ([]mirrorRule, error) {
	list, err := client.Resource(ImageTagMirrorSetResource).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var rules []mirrorRule
	for _, item := range list.Items {
		var itms configv1.ImageTagMirrorSet
		if err := fromUnstructured(item.Object, &itms); err != nil {
			continue
		}
		for _, m := range itms.Spec.ImageTagMirrors {
			mirrors := make([]string, len(m.Mirrors))
			for i, mirror := range m.Mirrors {
				mirrors[i] = string(mirror)
			}
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors, pullType: pullTypeTag})
		}
	}
	return rules, nil
}

func collectICPRules(ctx context.Context, client dynamic.Interface) ([]mirrorRule, error) {
	list, err := client.Resource(ImageContentPolicyResource).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var rules []mirrorRule
	for _, item := range list.Items {
		var icp configv1.ImageContentPolicy
		if err := fromUnstructured(item.Object, &icp); err != nil {
			continue
		}
		for _, m := range icp.Spec.RepositoryDigestMirrors {
			mirrors := make([]string, len(m.Mirrors))
			for i, mirror := range m.Mirrors {
				mirrors[i] = string(mirror)
			}
			// ICP (legacy ICSP) is digest-only by design
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors, pullType: pullTypeDigest})
		}
	}
	return rules, nil
}

func fromUnstructured(obj map[string]interface{}, out interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(obj, out)
}
