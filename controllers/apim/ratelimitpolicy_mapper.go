package apim

import (
	"context"

	"github.com/go-logr/logr"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// TODO(eastizle): KuadrantAddVSAnnotation annotation does not support multiple VirtualServices having reference to the same RateLimitPolicy
	// The annotation key is fixed, the RLP name is in the value
	KuadrantAddVSAnnotation    = "kuadrant.io/add-virtualservice"
	KuadrantDeleteVSAnnotation = "kuadrant.io/delete-virtualservice"
	KuadrantAddHRAnnotation    = "kuadrant.io/add-httproute"
	KuadrantDeleteHRAnnotation = "kuadrant.io/delete-httproute"
)

// routingPredicate is used by routing objects' controllers to filter for
// Kuadrant annotations signaling API protection.
func routingPredicate(m *rateLimitPolicyMapper) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if _, toRateLimit := e.Object.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]; toRateLimit {
				if err := m.SignalCreate(e.Object); err != nil {
					m.Logger.Error(err, "failed to signal create event to referenced RateLimitPolicy")
					// lets still try for auth annotation
				}
			}

			// only create reconcile request for routing objects' controllers when auth
			// annotation is present.
			_, toProtect := e.Object.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return toProtect
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, toRateLimitOld := e.ObjectOld.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
			_, toRateLimitNew := e.ObjectNew.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
			if toRateLimitNew || toRateLimitOld {
				if err := m.SignalUpdate(e.ObjectOld, e.ObjectNew); err != nil {
					m.Logger.Error(err, "failed to signal update event to referenced RateLimitPolicy")
				}
			}

			_, toProtectOld := e.ObjectOld.GetAnnotations()[KuadrantAuthProviderAnnotation]
			_, toProtectNew := e.ObjectNew.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return toProtectOld || toProtectNew
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			_, toRateLimit := e.Object.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
			if toRateLimit {
				if err := m.SignalDelete(e.Object); err != nil {
					m.Logger.Error(err, "failed to signal delete event to referenced RateLimitPolicy")
				}
			}

			_, toProtect := e.Object.GetAnnotations()[KuadrantAuthProviderAnnotation]
			return toProtect
		},
	}
}

// rateLimitPolicyMapper helps signal the change in the routing objects (VirtualService and HTTPRoute)
// to the referenced RateLimitPolicy in the annotation
type rateLimitPolicyMapper struct {
	K8sClient client.Client
	Logger    logr.Logger
}

func (m *rateLimitPolicyMapper) SignalCreate(obj client.Object) error {
	addAnnotation := KuadrantAddVSAnnotation
	if obj.GetObjectKind().GroupVersionKind().Kind == "HTTPRoute" {
		addAnnotation = KuadrantAddHRAnnotation
	}
	rlpName := obj.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
	m.Logger.Info("Signaling create event to RateLimitPolicy", "RateLimitPolicy", rlpName)
	rlpKey := types.NamespacedName{
		Name:      rlpName,
		Namespace: obj.GetNamespace(),
	}

	// get the referenced rlp
	rlp := &apimv1alpha1.RateLimitPolicy{}
	if err := m.K8sClient.Get(context.Background(), rlpKey, rlp); err != nil {
		return err
	}

	// signal addition by adding 'add' annotation
	rlp.Annotations[addAnnotation] = obj.GetName()
	err := m.K8sClient.Update(context.Background(), rlp)
	return err
}

func (m *rateLimitPolicyMapper) SignalDelete(obj client.Object) error {
	deleteAnnotation := KuadrantDeleteVSAnnotation
	if obj.GetObjectKind().GroupVersionKind().Kind == "HTTPRoute" {
		deleteAnnotation = KuadrantAddHRAnnotation
	}
	rlpName := obj.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
	m.Logger.Info("Signaling delete event to RateLimitPolicy", "RateLimitPolicy", rlpName)
	rlpKey := types.NamespacedName{
		Name:      rlpName,
		Namespace: obj.GetNamespace(),
	}

	// get the referenced rlp
	rlp := &apimv1alpha1.RateLimitPolicy{}
	if err := m.K8sClient.Get(context.Background(), rlpKey, rlp); err != nil {
		return err
	}

	// signal deletion by adding 'delete' annotation
	rlp.Annotations[deleteAnnotation] = obj.GetName()
	err := m.K8sClient.Update(context.Background(), rlp)
	return err
}

// SignalUpdate is used when either old or new object had/has the ratelimit annotaiton
func (m *rateLimitPolicyMapper) SignalUpdate(oldObj, newObj client.Object) error {
	m.Logger.Info("Signaling update event to RateLimitPolicy")
	oldRlpName, toRateLimitOld := oldObj.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]
	newRlpName, toRateLimitNew := newObj.GetAnnotations()[KuadrantRateLimitPolicyAnnotation]

	// case when rlp name is added (same as create event)
	if !toRateLimitOld && toRateLimitNew {
		if err := m.SignalCreate(newObj); err != nil {
			return err
		}
	}

	// case when rlp name is removed (same as delete event)
	if toRateLimitOld && !toRateLimitNew {
		if err := m.SignalDelete(oldObj); err != nil {
			return err
		}
	}

	// case when rlp name is changed (same as delete for old and create event for new)
	if toRateLimitNew && toRateLimitOld && oldRlpName != newRlpName {
		// signal deletion to old RLP
		if err := m.SignalDelete(oldObj); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}

		// signal addition to new RLP
		if err := m.SignalCreate(newObj); err != nil {
			return err
		}
	}

	// case when there is no change in the annotation, no action is required
	return nil
}
