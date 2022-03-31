package apim

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/mappers"
)

const (
	// TODO(eastizle): KuadrantAddVSAnnotation annotation does not support multiple VirtualServices having reference to the same RateLimitPolicy
	// These annotations are put on RateLimitPolicy resource to signal network change.
	// Note: the annotation key is fixed, the RLP name is in the value
	KuadrantAddVSAnnotation    = "kuadrant.io/attach-virtualservice"
	KuadrantDeleteVSAnnotation = "kuadrant.io/detach-virtualservice"
	KuadrantAddHRAnnotation    = "kuadrant.io/attach-httproute"
	KuadrantDeleteHRAnnotation = "kuadrant.io/detach-httproute"

	// These annotations help reconcilers know which signal to send to the RateLimitPolicy.
	KuadrantAttachNetwork = "kuadrant.io/attach-network"
	KuadrantDetachNetwork = "kuadrant.io/detach-network"
)

// TODO(rahulanand16nov): separate auth and ratelimit (single responsibility principle)
// routingPredicate is used by routing objects' controllers to filter for Kuadrant annotations signaling API protection.
func RoutingPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if rlpName, toRateLimit := e.Object.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]; toRateLimit {
				SignalAttach(e.Object, rlpName)
				return true
			}
			_, toProtect := e.Object.GetAnnotations()[mappers.KuadrantAuthProviderAnnotation]
			return toProtect
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, toRateLimitOld := e.ObjectOld.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]
			_, toRateLimitNew := e.ObjectNew.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]
			if toRateLimitNew || toRateLimitOld {
				SignalUpdate(e.ObjectOld, e.ObjectNew)
				return true
			}

			_, toProtectOld := e.ObjectOld.GetAnnotations()[mappers.KuadrantAuthProviderAnnotation]
			_, toProtectNew := e.ObjectNew.GetAnnotations()[mappers.KuadrantAuthProviderAnnotation]
			return toProtectOld || toProtectNew
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			if rlpName, toRateLimit := e.Object.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]; toRateLimit {
				SignalDetach(e.Object, rlpName)
				return true
			}

			_, toProtect := e.Object.GetAnnotations()[mappers.KuadrantAuthProviderAnnotation]
			return toProtect
		},
	}
}

// SignalDetach adds annotation conveying attachment of network
func SignalAttach(obj client.Object, rlpToAttach string) {
	annotations := obj.GetAnnotations()
	annotations[KuadrantAttachNetwork] = rlpToAttach
	obj.SetAnnotations(annotations)
}

// SignalDetach adds annotation conveying detachment of network
func SignalDetach(obj client.Object, rlpToDetach string) {
	annotations := obj.GetAnnotations()
	annotations[KuadrantDetachNetwork] = rlpToDetach
	obj.SetAnnotations(annotations)
}

func SignalUpdate(oldObj, newObj client.Object) {
	oldRlpName, toRateLimitOld := oldObj.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]
	newRlpName, toRateLimitNew := newObj.GetAnnotations()[mappers.KuadrantRateLimitPolicyAnnotation]

	// case when rlp name is added (same as create event)
	if !toRateLimitOld && toRateLimitNew {
		SignalAttach(newObj, newRlpName)
	}

	// case when rlp name is removed (same as delete event)
	if toRateLimitOld && !toRateLimitNew {
		SignalDetach(newObj, oldRlpName)
	}

	// case when rlp name is changed (same as delete for old and create event for new)
	if toRateLimitNew && toRateLimitOld && oldRlpName != newRlpName {
		SignalDetach(newObj, oldRlpName)
		SignalAttach(newObj, newRlpName)
	}
}

// SendSignal retrieves RateLimitPolicy and add the network change annotations.
func SendSignal(ctx context.Context, K8sClient client.Client, routingObj client.Object) error {
	logger := logr.FromContext(ctx)
	logger.Info("sending signal to RateLimitPolicy")
	attachAnnotation := KuadrantAddVSAnnotation
	detachAnnotation := KuadrantDeleteVSAnnotation
	if routingObj.GetObjectKind().GroupVersionKind().Kind == common.HTTPRouteKind {
		attachAnnotation = KuadrantAddHRAnnotation
		detachAnnotation = KuadrantDeleteHRAnnotation
	}

	if rlpName, toDeatch := routingObj.GetAnnotations()[KuadrantDetachNetwork]; toDeatch {
		rlp := &apimv1alpha1.RateLimitPolicy{}
		rlpKey := client.ObjectKey{Namespace: routingObj.GetNamespace(), Name: rlpName}
		if err := K8sClient.Get(ctx, rlpKey, rlp); err != nil {
			return err
		}
		if rlp.Annotations == nil {
			rlp.Annotations = make(map[string]string)
		}
		rlp.Annotations[detachAnnotation] = routingObj.GetName()
		if err := K8sClient.Update(ctx, rlp); err != nil {
			logger.Error(err, "failed to send detachment signal to RateLimitPolicy", "RateLimitPolicy", rlpKey.String())
			return err
		}
	}
	if rlpName, toAttach := routingObj.GetAnnotations()[KuadrantAttachNetwork]; toAttach {
		rlp := &apimv1alpha1.RateLimitPolicy{}
		rlpKey := client.ObjectKey{Namespace: routingObj.GetNamespace(), Name: rlpName}
		if err := K8sClient.Get(ctx, rlpKey, rlp); err != nil {
			return err
		}
		if rlp.Annotations == nil {
			rlp.Annotations = make(map[string]string)
		}
		rlp.Annotations[attachAnnotation] = routingObj.GetName()
		if err := K8sClient.Update(ctx, rlp); err != nil {
			logger.Error(err, "failed to send attachment signal to RateLimitPolicy", "RateLimitPolicy", rlpKey.String())
			return err
		}
	}
	return nil
}
