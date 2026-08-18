package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
)

type ExtensionAuthSecretReconciler struct {
	store      *extension.SessionStore
	namespace  string
	secretName string
}

func NewExtensionAuthSecretReconciler(store *extension.SessionStore, namespace, secretName string) *ExtensionAuthSecretReconciler {
	return &ExtensionAuthSecretReconciler{
		store:      store,
		namespace:  namespace,
		secretName: secretName,
	}
}

func (r *ExtensionAuthSecretReconciler) Reconcile(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(eventCtx).WithName("ExtensionAuthSecretReconciler")

	credentials := map[string][]byte{}
	secrets := topology.Objects().Items(func(o machinery.Object) bool {
		return o.GetName() == r.secretName &&
			o.GetNamespace() == r.namespace &&
			o.GroupVersionKind().GroupKind() == SecretGroupKind
	})
	if len(secrets) > 0 {
		runtimeObj, ok := secrets[0].(*controller.RuntimeObject)
		if !ok {
			return fmt.Errorf("auth secret %s/%s has unexpected topology type %T", r.namespace, r.secretName, secrets[0])
		}
		secret, ok := runtimeObj.Object.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("auth secret %s/%s has unexpected object type %T", r.namespace, r.secretName, runtimeObj.Object)
		}
		credentials = secret.Data
	}

	logger.V(1).Info("syncing extension auth secret credentials", "count", len(credentials))
	r.store.SyncSecretCredentials(credentials)
	return nil
}
