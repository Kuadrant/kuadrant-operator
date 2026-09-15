// Developer portal finalizer cleanup — remove this file after 2-3 releases.
// Before developer-portal-controller became a KuadrantControlPlane component,
// kuadrant-operator deployed it from a reconciler that put a
// "kuadrant.io/developerportal" finalizer on Kuadrant CRs so it could delete
// the Deployment when the Kuadrant CR went away. That reconciler is gone, so
// the finalizer must be stripped from every Kuadrant CR carrying it, including
// ones already terminating, or they can never finish deleting. Unlike the OLM
// cleanup, which only touches the operator namespace, this lists Kuadrant CRs
// cluster-wide: they can live in any namespace. The Deployment itself is
// deliberately left alone: KuadrantControlPlane owns it now.

package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

const developerPortalFinalizer = "kuadrant.io/developerportal"

// RunDeveloperPortalFinalizerCleanup strips the legacy developer portal
// finalizer from every Kuadrant CR that still carries it. It is a one-time
// startup function called from bootstrap, NOT during reconciliation. Returns
// the namespaced names of the CRs cleaned; a failure on one CR does not stop
// the others, the errors are joined and returned together.
func RunDeveloperPortalFinalizerCleanup(ctx context.Context, c client.Client, logger logr.Logger) ([]string, error) {
	logger = logger.WithName("developerportal-cleanup")

	list := &kuadrantv1beta1.KuadrantList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("listing Kuadrant CRs: %w", err)
	}

	var cleaned []string
	var errs []error
	for i := range list.Items {
		if !controllerutil.ContainsFinalizer(&list.Items[i], developerPortalFinalizer) {
			continue
		}
		key := client.ObjectKeyFromObject(&list.Items[i])
		if err := removeDeveloperPortalFinalizer(ctx, c, key); err != nil {
			errs = append(errs, fmt.Errorf("removing finalizer from Kuadrant %s: %w", key, err))
			continue
		}
		logger.Info("removed legacy developer portal finalizer", "kuadrant", key.String())
		cleaned = append(cleaned, key.String())
	}

	return cleaned, errors.Join(errs...)
}

// removeDeveloperPortalFinalizer re-reads the CR on every attempt so the update
// never clobbers finalizers other controllers add or remove concurrently. A CR
// that disappears mid-way (last finalizer gone, deletion completed) is a success.
func removeDeveloperPortalFinalizer(ctx context.Context, c client.Client, key client.ObjectKey) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		kObj := &kuadrantv1beta1.Kuadrant{}
		if err := c.Get(ctx, key, kObj); err != nil {
			return err
		}
		if !controllerutil.RemoveFinalizer(kObj, developerPortalFinalizer) {
			return nil
		}
		return c.Update(ctx, kObj)
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
