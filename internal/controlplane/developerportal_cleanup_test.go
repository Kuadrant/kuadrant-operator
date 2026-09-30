//go:build unit

package controlplane

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

func newKuadrant(namespace, name string, finalizers ...string) *kuadrantv1beta1.Kuadrant {
	return &kuadrantv1beta1.Kuadrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			Finalizers: finalizers,
		},
	}
}

func newCleanupClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return cleanupClientBuilder(t).WithObjects(objs...).Build()
}

func cleanupClientBuilder(t *testing.T) *fake.ClientBuilder {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := kuadrantv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	if err := kuadrantv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme)
}

// eventReasons drains a fake recorder the code under test has finished with.
func eventReasons(recorder *events.FakeRecorder) []string {
	close(recorder.Events)
	var reasons []string
	for e := range recorder.Events {
		reasons = append(reasons, strings.Fields(e)[1])
	}
	return reasons
}

func TestRunDeveloperPortalFinalizerCleanup(t *testing.T) {
	ctx := context.Background()

	t.Run("no Kuadrant CRs", func(t *testing.T) {
		cleaned, err := RunDeveloperPortalFinalizerCleanup(ctx, newCleanupClient(t), logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cleaned) != 0 {
			t.Fatalf("expected nothing cleaned, got %v", cleaned)
		}
	})

	t.Run("removes only the developer portal finalizer", func(t *testing.T) {
		c := newCleanupClient(t, newKuadrant("kuadrant-system", "kuadrant", "other.io/keep", developerPortalFinalizer))

		cleaned, err := RunDeveloperPortalFinalizerCleanup(ctx, c, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cleaned) != 1 || cleaned[0] != "kuadrant-system/kuadrant" {
			t.Fatalf("cleaned = %v, want [kuadrant-system/kuadrant]", cleaned)
		}

		got := &kuadrantv1beta1.Kuadrant{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: "kuadrant-system", Name: "kuadrant"}, got); err != nil {
			t.Fatalf("getting Kuadrant: %v", err)
		}
		if len(got.Finalizers) != 1 || got.Finalizers[0] != "other.io/keep" {
			t.Fatalf("finalizers = %v, want [other.io/keep]", got.Finalizers)
		}
	})

	t.Run("lets a terminating Kuadrant CR finish deleting", func(t *testing.T) {
		terminating := newKuadrant("kuadrant-system", "kuadrant", developerPortalFinalizer)
		terminating.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
		c := newCleanupClient(t, terminating)

		cleaned, err := RunDeveloperPortalFinalizerCleanup(ctx, c, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cleaned) != 1 {
			t.Fatalf("cleaned = %v, want one entry", cleaned)
		}

		err = c.Get(ctx, client.ObjectKey{Namespace: "kuadrant-system", Name: "kuadrant"}, &kuadrantv1beta1.Kuadrant{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected Kuadrant to be gone once its last finalizer was removed, got err=%v", err)
		}
	})

	t.Run("leaves Kuadrant CRs without the finalizer untouched", func(t *testing.T) {
		c := newCleanupClient(t, newKuadrant("kuadrant-system", "kuadrant", "other.io/keep"))

		before := &kuadrantv1beta1.Kuadrant{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: "kuadrant-system", Name: "kuadrant"}, before); err != nil {
			t.Fatalf("getting Kuadrant: %v", err)
		}

		cleaned, err := RunDeveloperPortalFinalizerCleanup(ctx, c, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cleaned) != 0 {
			t.Fatalf("expected nothing cleaned, got %v", cleaned)
		}

		after := &kuadrantv1beta1.Kuadrant{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: "kuadrant-system", Name: "kuadrant"}, after); err != nil {
			t.Fatalf("getting Kuadrant: %v", err)
		}
		if after.ResourceVersion != before.ResourceVersion {
			t.Fatalf("Kuadrant was written to: resourceVersion %s -> %s", before.ResourceVersion, after.ResourceVersion)
		}
	})

	t.Run("cleans every namespace", func(t *testing.T) {
		c := newCleanupClient(t,
			newKuadrant("ns-a", "kuadrant", developerPortalFinalizer),
			newKuadrant("ns-b", "kuadrant", developerPortalFinalizer),
		)

		cleaned, err := RunDeveloperPortalFinalizerCleanup(ctx, c, logr.Discard())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cleaned) != 2 {
			t.Fatalf("cleaned = %v, want two entries", cleaned)
		}
		for _, ns := range []string{"ns-a", "ns-b"} {
			got := &kuadrantv1beta1.Kuadrant{}
			if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: "kuadrant"}, got); err != nil {
				t.Fatalf("getting Kuadrant in %s: %v", ns, err)
			}
			if len(got.Finalizers) != 0 {
				t.Fatalf("finalizers in %s = %v, want none", ns, got.Finalizers)
			}
		}
	})
}

func TestRunDeveloperPortalCleanupRetries(t *testing.T) {
	backoff := wait.Backoff{Duration: time.Millisecond, Factor: 1, Steps: math.MaxInt32}
	controlPlane := &kuadrantv1alpha1.KuadrantControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName},
	}
	unavailable := apierrors.NewServiceUnavailable("apiserver restarting")

	t.Run("retries failed passes until the finalizer is removed", func(t *testing.T) {
		failures := 2
		c := cleanupClientBuilder(t).
			WithObjects(controlPlane.DeepCopy(), newKuadrant("kuadrant-system", "kuadrant", developerPortalFinalizer)).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, inner client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if failures > 0 {
						failures--
						return unavailable
					}
					return inner.Update(ctx, obj, opts...)
				},
			}).
			Build()
		recorder := events.NewFakeRecorder(10)
		r := &BootstrapRunnable{recorder: recorder, logger: logr.Discard()}

		r.runDeveloperPortalCleanup(context.Background(), c, backoff)

		got := &kuadrantv1beta1.Kuadrant{}
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: "kuadrant-system", Name: "kuadrant"}, got); err != nil {
			t.Fatalf("getting Kuadrant: %v", err)
		}
		if len(got.Finalizers) != 0 {
			t.Fatalf("finalizers = %v, want none", got.Finalizers)
		}
		want := []string{"DeveloperPortalMigrationIncomplete", "DeveloperPortalMigrationIncomplete", "DeveloperPortalFinalizerRemoved"}
		if reasons := eventReasons(recorder); !slices.Equal(reasons, want) {
			t.Fatalf("event reasons = %v, want %v", reasons, want)
		}
	})

	t.Run("stops once leadership is lost", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		lists := 0
		c := cleanupClientBuilder(t).
			WithObjects(controlPlane.DeepCopy()).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					lists++
					if lists == 3 {
						cancel()
					}
					return unavailable
				},
			}).
			Build()
		recorder := events.NewFakeRecorder(10)
		r := &BootstrapRunnable{recorder: recorder, logger: logr.Discard()}

		r.runDeveloperPortalCleanup(ctx, c, backoff)

		if lists != 3 {
			t.Fatalf("list calls = %d, want 3", lists)
		}
		// the pass cut short by the cancellation reports nothing
		want := []string{"DeveloperPortalMigrationIncomplete", "DeveloperPortalMigrationIncomplete"}
		if reasons := eventReasons(recorder); !slices.Equal(reasons, want) {
			t.Fatalf("event reasons = %v, want %v", reasons, want)
		}
	})
}
