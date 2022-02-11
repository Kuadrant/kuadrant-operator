package apim

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	patchesFinalizer = "kuadrant.io/ratelimitpatches"

	ownerRlpSeparator = ","

	envoyFilterAnnotationOwnerRLPs = "kuadrant.io/ownerRateLimitPolicies"
)

// finalizeEnvoyFilters makes sure orphan EnvoyFilter resources are not left when deleting the owner RateLimitPolicy.
func (r *RateLimitPolicyReconciler) finalizeEnvoyFilters(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	logger.Info("Removing/Updating EnvoyFilter resources")
	ownerRlp := client.ObjectKeyFromObject(rlp).String()

	for _, networkingRef := range rlp.Spec.NetworkingRef {
		switch networkingRef.Type {
		case apimv1alpha1.NetworkingRefTypeHR:
			logger.Info("HTTPRoute is not implemented yet") // TODO(rahulanand16nov)
			continue
		case apimv1alpha1.NetworkingRefTypeVS:
			logger.Info("Removing/Updating EnvoyFilter resources using VirtualService")
			vs := istio.VirtualService{}
			vsKey := client.ObjectKey{Namespace: rlp.Namespace, Name: networkingRef.Name}

			if err := r.Client().Get(ctx, vsKey, &vs); err != nil {
				logger.Error(err, "failed to get VirutalService")
				return err
			}

			for _, gateway := range vs.Spec.Gateways {
				gwKey := common.NamespacedNameToObjectKey(gateway, vs.Namespace)

				filtersPatchkey := client.ObjectKey{
					Namespace: gwKey.Namespace,
					Name:      rlFiltersPatchName(gwKey.Name),
				}
				filtersPatch := &istio.EnvoyFilter{}
				if err := r.Client().Get(ctx, filtersPatchkey, filtersPatch); err != nil {
					logger.Error(err, "failed to fetch ratelimit filters patch")
					return err
				}

				if err := removeOwnerRlpEntry(ctx, r.Client(), filtersPatch, ownerRlp); err != nil {
					logger.Error(err, "failed to remove ownerRLP tag on filters patch")
					return err
				}

				logger.Info("successfully removed ownerRLP entry on the filters patch")

				ratelimitsPatchKey := client.ObjectKey{
					Namespace: gwKey.Namespace,
					Name:      ratelimitsPatchName(rlp.Name, gwKey.Name),
				}
				ratelimitsPatch := &istio.EnvoyFilter{}
				if err := r.Client().Get(ctx, ratelimitsPatchKey, ratelimitsPatch); err != nil {
					logger.Error(err, "failed to fetch ratelimits patch")
					return err
				}

				if err := removeOwnerRlpEntry(ctx, r.Client(), ratelimitsPatch, ownerRlp); err != nil {
					logger.Error(err, "failed to remove ownerRLP tag on ratelimits patch")
					return err
				}
				logger.Info("successfully removed ownerRLP tag on ratelimits patch")
			}
		default:
			return fmt.Errorf(InvalidNetworkingRefTypeErr)
		}
	}
	return nil
}

func removeOwnerRlpEntry(ctx context.Context, client client.Client, patch *istio.EnvoyFilter, owner string) error {
	logger := logr.FromContext(ctx)
	logger.Info("removing ownerRLP entry from EnvoyFilter", "EnvoyFilter", patch.Name)

	// find the annotation
	ownerRlpsVal, present := patch.Annotations[envoyFilterAnnotationOwnerRLPs]
	if !present {
		logger.V(1).Info("Deleting the patch since ownerRLP annotation was not present to avoid orphans")
		// if it's not deleted then the patch will remain as an orphan once all the rlps are removed.
		if err := client.Delete(ctx, patch); err != nil {
			logger.Error(err, "failed to delete the patch")
			return err
		}
		return nil
	}

	// split into array of RateLimitPolicy names
	ownerRlps := strings.Split(ownerRlpsVal, ownerRlpSeparator)

	// remove the target owner
	finalOwnerRlps := []string{}
	for idx := range ownerRlps {
		if ownerRlps[idx] == owner {
			continue
		}
		finalOwnerRlps = append(finalOwnerRlps, ownerRlps[idx])
	}

	if len(finalOwnerRlps) == 0 {
		logger.V(1).Info("Deleting filters patch because 0 ownerRLP entries found on it")
		if err := client.Delete(ctx, patch); err != nil {
			logger.Error(err, "failed to delete the patch")
			return err
		}
	} else {
		finalOwnerRlpsVal := strings.Join(finalOwnerRlps, ownerRlpSeparator)
		patch.Annotations[envoyFilterAnnotationOwnerRLPs] = finalOwnerRlpsVal
		if err := client.Update(ctx, patch); err != nil {
			logger.Error(err, "failed to update the patch")
			return err
		}
	}
	return nil
}
