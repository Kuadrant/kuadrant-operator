// OLM migration cleanup — remove this entire file after 2-3 releases.
// It handles one-off cleanup of orphaned dns-operator Subscription/CSV
// resources left behind when upgrading from the pre-consolidation
// multi-operator OLM installation. Once all users have upgraded past
// the consolidated release, this code is dead.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

var (
	subscriptionGVR = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	csvGVR = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}
)

type OLMCleaner struct {
	client          dynamic.Interface
	discovery       discovery.DiscoveryInterface
	namespace       string
	deploymentNames []string
	packageNames    []string
	logger          logr.Logger
	cleaned         bool
}

type OLMCleanupResult struct {
	Skipped    bool
	Summary    string
	Error      string
	Components []ComponentCleanupResult
}

// ComponentCleanupResult reports the orphaned OLM resources removed for a
// single component's package. There is at most one Subscription and one CSV
// per component in the pre-consolidation install layout this cleanup targets.
type ComponentCleanupResult struct {
	Package          string
	SubscriptionName string
	CSVName          string
	MetadataCount    int
}

// RunOLMCleanup is a one-time startup function called from bootstrap.
// It is NOT called during reconciliation.
func RunOLMCleanup(ctx context.Context, deployer *Deployer, namespace string, logger logr.Logger) OLMCleanupResult {
	cleaner := NewOLMCleaner(
		deployer.DynamicClient(),
		deployer.DiscoveryClient(),
		namespace,
		deployer.DeploymentNames(),
		deployer.OLMPackageNames(),
		logger,
	)
	return cleaner.Cleanup(ctx)
}

func NewOLMCleaner(client dynamic.Interface, disc discovery.DiscoveryInterface, namespace string, deploymentNames, packageNames []string, logger logr.Logger) *OLMCleaner {
	return &OLMCleaner{
		client:          client,
		discovery:       disc,
		namespace:       namespace,
		deploymentNames: deploymentNames,
		packageNames:    packageNames,
		logger:          logger.WithName("olm-cleanup"),
	}
}

func (c *OLMCleaner) Cleanup(ctx context.Context) OLMCleanupResult {
	if c.cleaned {
		return OLMCleanupResult{Skipped: true}
	}

	if !c.isOLMInstalled() {
		c.logger.V(1).Info("OLM not detected, skipping cleanup")
		c.cleaned = true
		return OLMCleanupResult{Summary: "OLM not detected, no migration needed"}
	}

	metadataCountByCSV, err := c.removeOrphanedOLMMetadata(ctx)
	if err != nil {
		c.logger.Error(err, "failed to strip OLM metadata, skipping Subscription/CSV deletion to avoid ownership cascade")
		return OLMCleanupResult{Error: fmt.Sprintf("failed to strip OLM metadata: %v", err)}
	}

	var errs []string

	subsByPackage, err := c.deleteOrphanedSubscriptions(ctx)
	if err != nil {
		c.logger.Error(err, "failed to delete orphaned Subscriptions (non-fatal)")
		errs = append(errs, fmt.Sprintf("Subscriptions: %v", err))
	}

	csvsByPackage, err := c.deleteOrphanedCSVs(ctx)
	if err != nil {
		c.logger.Error(err, "failed to delete orphaned CSVs (non-fatal)")
		errs = append(errs, fmt.Sprintf("CSVs: %v", err))
	}

	c.cleaned = true

	metadataCount := 0
	for _, count := range metadataCountByCSV {
		metadataCount += count
	}

	if len(errs) > 0 {
		return OLMCleanupResult{Error: fmt.Sprintf("partial cleanup: %s", strings.Join(errs, "; "))}
	}
	return OLMCleanupResult{
		Summary: fmt.Sprintf("stripped OLM metadata from %d resources, deleted %d Subscriptions and %d CSVs",
			metadataCount, len(subsByPackage), len(csvsByPackage)),
		Components: c.buildComponentResults(subsByPackage, csvsByPackage, metadataCountByCSV),
	}
}

// buildComponentResults joins per-package Subscription/CSV deletions with the
// per-CSV metadata strip count, one entry per package that had something
// actually removed.
func (c *OLMCleaner) buildComponentResults(subsByPackage, csvsByPackage map[string]string, metadataCountByCSV map[string]int) []ComponentCleanupResult {
	var results []ComponentCleanupResult
	for _, pkg := range c.packageNames {
		subName := subsByPackage[pkg]
		csvName := csvsByPackage[pkg]
		metadataCount := metadataCountByCSV[csvName]
		if subName == "" && csvName == "" && metadataCount == 0 {
			continue
		}
		results = append(results, ComponentCleanupResult{
			Package:          pkg,
			SubscriptionName: subName,
			CSVName:          csvName,
			MetadataCount:    metadataCount,
		})
	}
	return results
}

func (c *OLMCleaner) isOLMInstalled() bool {
	_, resources, err := c.discovery.ServerGroupsAndResources()
	if err != nil && resources == nil {
		c.logger.V(1).Info("discovery failed, assuming OLM not installed", "error", err)
		return false
	}
	for _, list := range resources {
		if strings.HasPrefix(list.GroupVersion, "operators.coreos.com/") {
			return true
		}
	}
	return false
}

// removeOrphanedOLMMetadata finds all resources owned by orphaned child
// operator CSVs using the olm.owner label and strips OLM ownerReferences
// and labels from them. This works for any resource type OLM manages —
// no hardcoded resource list needed.
func (c *OLMCleaner) removeOrphanedOLMMetadata(ctx context.Context) (map[string]int, error) {
	var errs []error
	countByCSV := make(map[string]int)
	for _, csvName := range c.findOrphanedCSVNames(ctx) {
		count, err := c.cleanResourcesOwnedByCSV(ctx, csvName)
		countByCSV[csvName] = count
		if err != nil {
			errs = append(errs, err)
		}
	}
	return countByCSV, errors.Join(errs...)
}

func (c *OLMCleaner) findOrphanedCSVNames(ctx context.Context) []string {
	csvs, err := c.client.Resource(csvGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var names []string
	for i := range csvs.Items {
		name := csvs.Items[i].GetName()
		if c.isOrphanedCSV(name) {
			names = append(names, name)
		}
	}
	return names
}

// olmManagedResourceTypes lists the resource types OLM labels when managing
// child operator resources. Restricting to these avoids full API discovery.
var olmManagedResourceTypes = []struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}{
	{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false},
	{schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}, false},
}

func (c *OLMCleaner) cleanResourcesOwnedByCSV(ctx context.Context, csvName string) (int, error) {
	// OLM uses different labels on different resource types:
	// - Most resources: olm.owner=<csv-name>
	// - CRDs and some others: operators.coreos.com/<package>.<namespace>=
	pkg := packageFromCSVName(csvName)
	labelSelectors := []string{
		fmt.Sprintf("olm.owner=%s", csvName),
		fmt.Sprintf("operators.coreos.com/%s.%s=", pkg, c.namespace),
	}

	cleaned := make(map[string]bool)
	var errs []error

	for _, rt := range olmManagedResourceTypes {
		for _, labelSelector := range labelSelectors {
			var list *unstructured.UnstructuredList
			var err error

			if rt.namespaced {
				list, err = c.client.Resource(rt.gvr).Namespace(c.namespace).List(ctx, metav1.ListOptions{
					LabelSelector: labelSelector,
				})
			} else {
				list, err = c.client.Resource(rt.gvr).List(ctx, metav1.ListOptions{
					LabelSelector: labelSelector,
				})
			}
			if err != nil {
				c.logger.V(1).Info("failed to list resources for OLM cleanup",
					"resource", rt.gvr.Resource, "labelSelector", labelSelector, "error", err)
				continue
			}

			for i := range list.Items {
				obj := &list.Items[i]
				key := fmt.Sprintf("%s/%s/%s", rt.gvr.Resource, obj.GetNamespace(), obj.GetName())
				if cleaned[key] {
					continue
				}

				if _, ok := obj.GetLabels()["olm.owner.kind"]; ok {
					c.logger.V(1).Info("skipping OLM-generated resource",
						"resource", rt.gvr.Resource, "name", obj.GetName())
					continue
				}

				modified := c.stripCSVOwnerRefs(obj)
				modified = c.stripOLMLabels(obj) || modified

				if !modified {
					continue
				}

				c.logger.Info("cleaned OLM metadata",
					"resource", rt.gvr.Resource,
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
					"csv", csvName,
				)

				if rt.namespaced {
					_, err = c.client.Resource(rt.gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
				} else {
					_, err = c.client.Resource(rt.gvr).Update(ctx, obj, metav1.UpdateOptions{})
				}
				if err != nil {
					if apierrors.IsForbidden(err) {
						c.logger.Info("skipping resource: insufficient permissions to strip OLM metadata",
							"resource", rt.gvr.Resource,
							"name", obj.GetName(),
						)
						continue
					}
					c.logger.Error(err, "failed to strip OLM metadata from resource",
						"resource", rt.gvr.Resource,
						"name", obj.GetName(),
					)
					errs = append(errs, fmt.Errorf("updating %s/%s: %w", rt.gvr.Resource, obj.GetName(), err))
					continue
				}
				cleaned[key] = true
			}
		}
	}
	return len(cleaned), errors.Join(errs...)
}

func (c *OLMCleaner) stripCSVOwnerRefs(obj *unstructured.Unstructured) bool {
	ownerRefs, found, err := unstructured.NestedSlice(obj.Object, "metadata", "ownerReferences")
	if err != nil || !found || len(ownerRefs) == 0 {
		return false
	}

	modified := false
	var filtered []interface{}
	for _, ref := range ownerRefs {
		refMap, ok := ref.(map[string]interface{})
		if !ok {
			filtered = append(filtered, ref)
			continue
		}
		kind, _ := refMap["kind"].(string)
		if kind == "ClusterServiceVersion" {
			modified = true
			continue
		}
		filtered = append(filtered, ref)
	}

	if !modified {
		return false
	}

	if len(filtered) == 0 {
		unstructured.RemoveNestedField(obj.Object, "metadata", "ownerReferences")
	} else {
		_ = unstructured.SetNestedSlice(obj.Object, filtered, "metadata", "ownerReferences")
	}
	return true
}

func (c *OLMCleaner) stripOLMLabels(obj *unstructured.Unstructured) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}

	modified := false
	cleaned := make(map[string]string, len(labels))
	for k, v := range labels {
		if strings.HasPrefix(k, "olm.") || strings.HasPrefix(k, "operators.coreos.com/") {
			modified = true
			continue
		}
		cleaned[k] = v
	}

	if modified {
		obj.SetLabels(cleaned)
	}
	return modified
}

// deleteOrphanedSubscriptions returns the deleted Subscription name keyed by
// package. If more than one Subscription exists for the same package, the
// last one deleted wins the map entry — the migration this targets expects
// at most one Subscription per component.
func (c *OLMCleaner) deleteOrphanedSubscriptions(ctx context.Context) (map[string]string, error) {
	subs, err := c.client.Resource(subscriptionGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing subscriptions: %w", err)
	}

	var errs []error
	deleted := make(map[string]string)
	for i := range subs.Items {
		sub := &subs.Items[i]
		pkg, found, err := unstructured.NestedString(sub.Object, "spec", "name")
		if err != nil || !found {
			continue
		}
		if !slices.Contains(c.packageNames, pkg) {
			continue
		}
		c.logger.Info("deleting orphaned OLM Subscription", "name", sub.GetName(), "package", pkg)
		if err := c.client.Resource(subscriptionGVR).Namespace(c.namespace).Delete(ctx, sub.GetName(), metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("deleting Subscription %s: %w", sub.GetName(), err))
			}
		} else {
			deleted[pkg] = sub.GetName()
		}
	}
	return deleted, errors.Join(errs...)
}

// deleteOrphanedCSVs returns the deleted CSV name keyed by package. If more
// than one CSV exists for the same package, the last one deleted wins the
// map entry — the migration this targets expects at most one CSV per component.
func (c *OLMCleaner) deleteOrphanedCSVs(ctx context.Context) (map[string]string, error) {
	csvs, err := c.client.Resource(csvGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing CSVs: %w", err)
	}

	var errs []error
	deleted := make(map[string]string)
	for i := range csvs.Items {
		csv := &csvs.Items[i]
		name := csv.GetName()
		if !c.isOrphanedCSV(name) {
			continue
		}
		c.logger.Info("deleting orphaned OLM CSV", "name", name)
		if err := c.client.Resource(csvGVR).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("deleting CSV %s: %w", name, err))
			}
		} else {
			deleted[packageFromCSVName(name)] = name
		}
	}
	return deleted, errors.Join(errs...)
}

func (c *OLMCleaner) isOrphanedCSV(name string) bool {
	for _, p := range c.packageNames {
		if strings.HasPrefix(name, p+".") || name == p {
			return true
		}
	}
	return false
}

// packageFromCSVName extracts the OLM package name from a CSV name of the
// form "<package>.<version>" (e.g. "dns-operator.v0.8.0" -> "dns-operator").
func packageFromCSVName(name string) string {
	return strings.SplitN(name, ".", 2)[0]
}
