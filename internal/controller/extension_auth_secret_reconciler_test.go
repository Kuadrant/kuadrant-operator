//go:build unit

package controllers

import (
	"context"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kuadrant/kuadrant-operator/internal/extension"
)

const (
	testAuthSecretNamespace = "kuadrant-system"
	testAuthSecretName      = "kuadrant-extension-auth"
)

func authSecretRuntimeObject(data map[string][]byte) *controller.RuntimeObject {
	return &controller.RuntimeObject{
		Object: &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       SecretGroupKind.Kind,
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAuthSecretName,
				Namespace: testAuthSecretNamespace,
			},
			Data: data,
		},
	}
}

func TestExtensionAuthSecretReconciler_SyncsCredentialsFromSecret(t *testing.T) {
	store := extension.NewSessionStore(logr.Discard())
	reconciler := NewExtensionAuthSecretReconciler(store, testAuthSecretNamespace, testAuthSecretName)

	cred := []byte("0123456789abcdef0123456789abcdef")
	topology, err := machinery.NewTopology(machinery.WithObjects(authSecretRuntimeObject(map[string][]byte{"standalone": cred})))
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}

	if err := reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{}); err != nil {
		t.Fatalf("expected reconcile to succeed, got: %v", err)
	}

	if _, err := store.Authenticate("standalone", cred, "StandalonePolicy"); err != nil {
		t.Fatalf("expected credential to be synced into store, got: %v", err)
	}
}

func TestExtensionAuthSecretReconciler_AbsentSecretPrunesCredentials(t *testing.T) {
	store := extension.NewSessionStore(logr.Discard())
	reconciler := NewExtensionAuthSecretReconciler(store, testAuthSecretNamespace, testAuthSecretName)

	cred := []byte("0123456789abcdef0123456789abcdef")
	store.SyncSecretCredentials(map[string][]byte{"standalone": cred})

	topology, err := machinery.NewTopology()
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}

	if err := reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{}); err != nil {
		t.Fatalf("expected reconcile to succeed, got: %v", err)
	}

	if _, err := store.Authenticate("standalone", cred, "StandalonePolicy"); err == nil {
		t.Fatal("expected credential to be pruned when secret is absent")
	}
}

func TestExtensionAuthSecretReconciler_UnexpectedObjectTypeReturnsError(t *testing.T) {
	store := extension.NewSessionStore(logr.Discard())
	reconciler := NewExtensionAuthSecretReconciler(store, testAuthSecretNamespace, testAuthSecretName)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(SecretGroupKind.WithVersion("v1"))
	obj.SetName(testAuthSecretName)
	obj.SetNamespace(testAuthSecretNamespace)

	topology, err := machinery.NewTopology(machinery.WithObjects(&controller.RuntimeObject{Object: obj}))
	if err != nil {
		t.Fatalf("failed to create topology: %v", err)
	}

	if err := reconciler.Reconcile(context.Background(), nil, topology, nil, &sync.Map{}); err == nil {
		t.Fatal("expected reconcile to return an error for unexpected object type")
	}
}
