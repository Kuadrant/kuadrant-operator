package kuadrant

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func policyReferenceAnnotationKey(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf("%s/policyreference", gvk.GroupKind().String())
}

// SetPolicyReference sets a PolicyReference on obj for the given kind
// Returns true if the obj has been updated
func SetPolicyReference(obj metav1.Object, ownerKind schema.GroupVersionKind, ownerKey client.ObjectKey) bool {
	annotations := obj.GetAnnotations()
	if val, ok := annotations[policyReferenceAnnotationKey(ownerKind)]; ok && val == ownerKey.String() {
		return false
	}

	if annotations == nil {
		annotations = map[string]string{}
	}

	// annotation value is the object key serialization format "{namespace}/{name}"
	annotations[policyReferenceAnnotationKey(ownerKind)] = ownerKey.String()

	obj.SetAnnotations(annotations)

	return true
}

// GetPolicyReference gets the PolicyReference for the given kind from the obj
// Returns owner object key for the same kind
func GetPolicyReference(obj metav1.Object, ownerKind schema.GroupVersionKind) *client.ObjectKey {
	annotations := obj.GetAnnotations()
	if val, ok := annotations[policyReferenceAnnotationKey(ownerKind)]; ok {
		// annotation value is the object key serialization format "{namespace}/{name}"
		return ptr.To(utils.NamespacedNameToObjectKey(val, "default"))
	}

	return nil
}

// RemovePolicyReference removes the PolicyReference for the given kind from the obj
// Returns true if the obj has been updated
func RemovePolicyReference(obj metav1.Object, ownerKind schema.GroupVersionKind) bool {
	annotations := obj.GetAnnotations()
	if _, ok := annotations[policyReferenceAnnotationKey(ownerKind)]; ok {
		delete(annotations, policyReferenceAnnotationKey(ownerKind))
		obj.SetAnnotations(annotations)
		return true
	}
	return false
}

func ReconcilePolicyReferenceOnObject(
	ctx context.Context, cl client.Client, ownerKind schema.GroupVersionKind,
	obj client.Object, attachedPolicies []kuadrantgatewayapi.Policy,
) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	updateObject := false

	acceptedPolicies := utils.Filter(attachedPolicies, func(p kuadrantgatewayapi.Policy) bool {
		return meta.IsStatusConditionTrue(
			p.GetStatus().GetConditions(),
			string(gatewayapiv1alpha2.PolicyConditionAccepted),
		)
	})

	if len(acceptedPolicies) == 0 {
		updateObject = RemovePolicyReference(obj, ownerKind)
	} else {
		// Read policyref from obj
		// if policyRef exists:
		//   if policyRef is one of the attached policies -> NO OP
		//   else set policy ref first of accepted attached policies
		// else set policy ref first of accepted attached policies

		currPolicyRefKey := GetPolicyReference(obj, ownerKind)
		if currPolicyRefKey == nil {
			updateObject = SetPolicyReference(
				obj, ownerKind, client.ObjectKeyFromObject(acceptedPolicies[0]),
			)
		} else {
			policyRefIdx := utils.Index(acceptedPolicies, func(p kuadrantgatewayapi.Policy) bool {
				return client.ObjectKeyFromObject(p) == *currPolicyRefKey
			})
			if policyRefIdx < 0 {
				policyRefIdx = 0
			}
			updateObject = SetPolicyReference(obj, ownerKind, client.ObjectKeyFromObject(acceptedPolicies[policyRefIdx]))
		}
	}

	if updateObject {
		err := cl.Update(ctx, obj)
		logger.V(1).Info("reconcileNetworkObjectDirectBackReferenceAnnotation: update network resource",
			"kind", obj.GetObjectKind().GroupVersionKind(),
			"name", client.ObjectKeyFromObject(obj), "err", err)
		return err
	}

	return nil
}
