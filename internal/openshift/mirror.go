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

func resolveFromMirrors(imageURL string, rules []mirrorRule) string {
	var bestMatch mirrorRule
	bestLen := 0

	for _, rule := range rules {
		if len(rule.mirrors) == 0 {
			continue
		}
		// The source must be a prefix of the image URL, followed by either
		// end-of-string, '/', ':', or '@' to ensure we match on a repository boundary.
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
