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

type mirrorRule struct {
	source  string
	mirrors []string
}

// ResolveImageURL resolves a container image URL through OpenShift mirror configurations.
//
// In disconnected (air-gapped) clusters, images are served from internal mirror registries
// instead of their original source registries. OpenShift exposes this mapping via three
// cluster-scoped CRDs:
//   - ImageDigestMirrorSet (IDMS) — config.openshift.io/v1, for digest-based pulls (preferred)
//   - ImageTagMirrorSet (ITMS) — config.openshift.io/v1, for tag-based pulls
//   - ImageContentPolicy (ICP) — config.openshift.io/v1, legacy equivalent of ICSP
//
// This function collects mirror rules from all three sources, finds the most specific
// source prefix match, and rewrites the image URL to point to the mirror. This is
// necessary because Istio and Envoy Gateway pull wasm-shim images directly and do not
// honor OpenShift's node-level mirror configuration (registries.conf / CRI-O).
//
// The resolution logic follows the same prefix-matching semantics used by:
//   - openshift/oc: pkg/cli/image/strategy (alternativeImageSourcesIDMS)
//   - openshift/runtime-utils: pkg/registries (EditRegistriesConfig)
//   - containers/image: pkg/sysregistriesv2 (FindRegistry / PullSourcesFromReference)
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
		logger.V(1).Info("resolved wasm-shim image via mirror", "original", imageURL, "resolved", resolved)
	}
	return resolved
}

// resolveFromMirrors finds the best mirror for an image URL using most-specific-prefix matching.
//
// The matching follows the containers/image registries.conf semantics: the source with the
// longest matching prefix wins, and the match must align on a repository boundary
// ('/', ':', '@') to prevent partial hostname matches (e.g., "registry.io" must not match
// "registry.io.evil.com"). The first mirror in the winning rule is used.
//
// Example: for image "registry.redhat.io/kuadrant/wasm-shim@sha256:abc" with rules:
//   - source: "registry.redhat.io"         -> mirrors: ["mirror.local"]
//   - source: "registry.redhat.io/kuadrant" -> mirrors: ["mirror.local/kuadrant-mirror"]
//
// The second rule wins (longer prefix), producing "mirror.local/kuadrant-mirror/wasm-shim@sha256:abc".
func resolveFromMirrors(imageURL string, rules []mirrorRule) string {
	var bestMatch mirrorRule
	bestLen := 0

	for _, rule := range rules {
		if len(rule.mirrors) == 0 {
			continue
		}
		if !strings.HasPrefix(imageURL, rule.source) {
			continue
		}
		rest := imageURL[len(rule.source):]
		if len(rest) > 0 && rest[0] != '/' && rest[0] != ':' && rest[0] != '@' {
			continue
		}
		if len(rule.source) > bestLen {
			bestLen = len(rule.source)
			bestMatch = rule
		}
	}

	if bestLen == 0 {
		return imageURL
	}

	return bestMatch.mirrors[0] + imageURL[bestLen:]
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
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors})
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
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors})
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
			rules = append(rules, mirrorRule{source: m.Source, mirrors: mirrors})
		}
	}
	return rules, nil
}

func fromUnstructured(obj map[string]interface{}, out interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(obj, out)
}
