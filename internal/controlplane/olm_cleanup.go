// OLM migration cleanup — remove this entire file after 2-3 releases.
// It handles one-off cleanup of orphaned child-operator Subscription/CSV
// resources left behind when upgrading from the pre-consolidation
// multi-operator OLM installation. Once all users have upgraded past
// the consolidated release, this code is dead.
//
// Each component gets its own explicit migrateXxx function. There are only
// ever four components to migrate, each with real, not merely cosmetic,
// differences in what needs protecting and in what order (e.g. authorino
// and limitador need their Services and RBAC protected too, not just their
// Deployment, since Envoy calls those Services synchronously on the live
// request path — unlike dns-operator, which has no such dependency).

package controlplane

import (
	"context"
	"fmt"
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

	deploymentGVR = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	crdGVR = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	configMapGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}
)

// OLMCleaner holds the shared low-level primitives (get/strip/update a
// resource, delete a Subscription/CSV for a given package) used by each
// component's explicit migration function. Which specific resources belong
// to which component is hardcoded directly in that component's function
// — see the package doc comment above.
type OLMCleaner struct {
	client    dynamic.Interface
	discovery discovery.DiscoveryInterface
	namespace string
	logger    logr.Logger
	cleaned   bool
}

type OLMCleanupResult struct {
	Skipped    bool
	Summary    string
	Error      string
	Components []ComponentCleanupResult
}

// ComponentCleanupResult reports the OLM resources removed for a single
// component's package. There is at most one Subscription and one CSV
// per component in the pre-consolidation install layout this cleanup targets.
type ComponentCleanupResult struct {
	Package          string
	Namespace        string
	SubscriptionName string
	CSVName          string
}

// RunOLMCleanup is a one-time startup function called from bootstrap.
// It is NOT called during reconciliation.
func RunOLMCleanup(ctx context.Context, client dynamic.Interface, disc discovery.DiscoveryInterface, namespace string, logger logr.Logger) OLMCleanupResult {
	return NewOLMCleaner(client, disc, namespace, logger).Cleanup(ctx)
}

func NewOLMCleaner(client dynamic.Interface, disc discovery.DiscoveryInterface, namespace string, logger logr.Logger) *OLMCleaner {
	return &OLMCleaner{
		client:    client,
		discovery: disc,
		namespace: namespace,
		logger:    logger.WithName("olm-cleanup"),
	}
}

func (c *OLMCleaner) Cleanup(ctx context.Context) OLMCleanupResult {
	if c.cleaned {
		return OLMCleanupResult{Skipped: true}
	}

	if !c.isOLMInstalled() {
		c.logger.V(1).Info("OLM not detected, skipping migration")
		c.cleaned = true
		return OLMCleanupResult{Summary: "OLM not detected, no migration needed"}
	}

	// Add component migrate functions here once it's consolidated.
	migrations := []func(context.Context) (ComponentCleanupResult, error){
		c.migrateDNSOperator,
	}

	var results []ComponentCleanupResult
	var errs []string
	cleanedCount := 0
	for _, migrate := range migrations {
		result, err := migrate(ctx)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", result.Package, err))
			continue
		}
		if result.SubscriptionName == "" && result.CSVName == "" {
			continue
		}
		results = append(results, result)
		cleanedCount++
	}

	c.cleaned = true

	if len(errs) > 0 {
		return OLMCleanupResult{Error: fmt.Sprintf("partial cleanup: %s", strings.Join(errs, "; "))}
	}
	return OLMCleanupResult{
		Summary:    fmt.Sprintf("migrated %d component(s) off OLM", cleanedCount),
		Components: results,
	}
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

// migrateDNSOperator strips OLM ownership from dns-operator's resources so
// they survive its Subscription/CSV deletion, then deletes the  Subscription
// and CSV. In order:
//
//  1. Strip OLM ownerReferences and labels from the Deployment
//     "dns-operator-controller-manager" in this namespace, so it isn't
//     garbage-collected when the CSV is deleted.
//  2. Strip OLM ownerReferences and labels from the cluster-scoped CRDs
//     "dnsrecords.kuadrant.io" and "dnshealthcheckprobes.kuadrant.io" —
//     deleting a CRD also deletes every custom resource of that type
//     cluster-wide.
//  3. Strip OLM ownerReferences and labels from the ConfigMap
//     "dns-operator-controller-env" in this namespace. The chart renders
//     it empty as a user-editable extension point for extra env vars; if
//     it's cascade-deleted and recreated, any user customization is lost.
//  4. If any strip above fails, stop and return the error without touching
//     the Subscription/CSV — deleting them first would cascade-delete the
//     resources the previous steps were protecting.
//  5. Delete the "dns-operator" Subscription in this namespace, if
//     one exists. Matched by its spec.name field, since Subscription
//     object names are catalog-generated, not predictable.
//  6. Delete the dns-operator ClusterServiceVersion in this
//     namespace, if one exists (matched by the "dns-operator." CSV name
//     prefix convention).
//
// Everything else OLM created for dns-operator (ServiceAccount, RBAC, the
// metrics Service) is left alone: it's safe to let OLM garbage-collect,
// since the deployer recreates it from the embedded chart on its own
// reconcile. RBAC deletion/re-creation may cause a momentary permission gap
// for the dns-operator pod, but that's not a concern here since it isn't a
// live-traffic dependency.
func (c *OLMCleaner) migrateDNSOperator(ctx context.Context) (ComponentCleanupResult, error) {
	const pkg = "dns-operator"
	result := ComponentCleanupResult{Package: pkg, Namespace: c.namespace}

	// 1. Deployments
	if err := c.stripResource(ctx, deploymentGVR, c.namespace, "dns-operator-controller-manager"); err != nil {
		return result, fmt.Errorf("stripping dns-operator-controller-manager: %w", err)
	}
	// 2. CRDs
	for _, crd := range []string{"dnsrecords.kuadrant.io", "dnshealthcheckprobes.kuadrant.io"} {
		if err := c.stripResource(ctx, crdGVR, "", crd); err != nil {
			return result, fmt.Errorf("stripping CRD %s: %w", crd, err)
		}
	}
	// 3. ConfigMaps
	if err := c.stripResource(ctx, configMapGVR, c.namespace, "dns-operator-controller-env"); err != nil {
		return result, fmt.Errorf("stripping dns-operator-controller-env: %w", err)
	}

	// 4. Subscription
	subName, err := c.deleteSubscriptionForPackage(ctx, pkg)
	if err != nil {
		return result, fmt.Errorf("deleting Subscription: %w", err)
	}
	result.SubscriptionName = subName

	// 5. CSV
	csvName, err := c.deleteCSVForPackage(ctx, pkg)
	if err != nil {
		return result, fmt.Errorf("deleting CSV: %w", err)
	}
	result.CSVName = csvName

	return result, nil
}

// stripResource removes OLM ownerReferences/labels from a single named
// resource, if present. namespace == "" for cluster-scoped resources.
// Returns nil if the resource doesn't exist or carries no OLM metadata to
// strip. Any other error (including Forbidden) is returned as a failure:
// these are resources a migrateXxx function specifically declared it needs
// to update, so being unable to is a real misconfiguration, not something
// to tolerate silently.
func (c *OLMCleaner) stripResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	nsResource := c.client.Resource(gvr)
	var res dynamic.ResourceInterface = nsResource
	if namespace != "" {
		res = nsResource.Namespace(namespace)
	}

	obj, err := res.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting %s/%s: %w", gvr.Resource, name, err)
	}

	modified := c.stripCSVOwnerRefs(obj)
	modified = c.stripOLMLabels(obj) || modified
	if !modified {
		return nil
	}

	if _, err := res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating %s/%s: %w", gvr.Resource, name, err)
	}

	c.logger.Info("cleaned OLM metadata", "resource", gvr.Resource, "name", name)
	return nil
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

// deleteSubscriptionForPackage deletes the Subscription for the given OLM
// package in this namespace, if one exists. Returns the deleted
// Subscription's name, or "" if none was found. The object's own name is
// catalog-generated, so it's found by matching its spec.name field instead.
func (c *OLMCleaner) deleteSubscriptionForPackage(ctx context.Context, pkg string) (string, error) {
	subs, err := c.client.Resource(subscriptionGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing subscriptions: %w", err)
	}

	for i := range subs.Items {
		sub := &subs.Items[i]
		name, found, err := unstructured.NestedString(sub.Object, "spec", "name")
		if err != nil || !found || name != pkg {
			continue
		}
		c.logger.Info("deleting OLM Subscription", "name", sub.GetName(), "package", pkg)
		if err := c.client.Resource(subscriptionGVR).Namespace(c.namespace).Delete(ctx, sub.GetName(), metav1.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("deleting Subscription %s: %w", sub.GetName(), err)
		}
		return sub.GetName(), nil
	}
	return "", nil
}

// deleteCSVForPackage deletes the ClusterServiceVersion for the given OLM
// package in this namespace, if one exists. Returns the deleted CSV's name,
// or "" if none was found.
func (c *OLMCleaner) deleteCSVForPackage(ctx context.Context, pkg string) (string, error) {
	csvs, err := c.client.Resource(csvGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing CSVs: %w", err)
	}

	for i := range csvs.Items {
		name := csvs.Items[i].GetName()
		if !isCSVForPackage(name, pkg) {
			continue
		}
		c.logger.Info("deleting OLM CSV", "name", name)
		if err := c.client.Resource(csvGVR).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("deleting CSV %s: %w", name, err)
		}
		return name, nil
	}
	return "", nil
}

// isCSVForPackage reports whether csvName is the CSV for pkg, per OLM's
// "<package>.<version>" CSV naming convention (e.g. "dns-operator.v0.8.0"
// belongs to package "dns-operator").
func isCSVForPackage(csvName, pkg string) bool {
	return csvName == pkg || strings.HasPrefix(csvName, pkg+".")
}
