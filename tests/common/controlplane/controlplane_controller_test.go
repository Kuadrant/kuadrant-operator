//go:build integration

package controlplane

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const (
	operatorNamespace       = "kuadrant-system"
	dnsOperatorDeployment   = "dns-operator-controller-manager"
	dnsOperatorEnvConfigMap = "dns-operator-controller-env"
	devSetupFieldOwner      = "dev-setup"
)

// devDNSOperatorConfigMapPath is the same file make local-env-setup applies
// (see make/development-environments.mk's deploy-dependencies target) to
// enable the "inmemory" DNS provider for local development and this test
// suite. The dns-operator chart renders this ConfigMap with no "data" at
// all, so normal reconciles never touch it -- but tests that delete the
// KuadrantControlPlane cascade-delete it along with everything else it owns,
// and the deployer's redeploy re-creates it blank. restoreDevEnvOverrides
// (called from AfterEach) re-applies it so a destructive test here doesn't
// silently break dnspolicy tests relying on the inmemory provider.
var devDNSOperatorConfigMapPath = filepath.Join("..", "..", "..", "config", "dev", "dns-operator-configmap.yaml")

func restoreDevEnvOverrides(ctx SpecContext) {
	data, err := os.ReadFile(devDNSOperatorConfigMapPath)
	Expect(err).ToNot(HaveOccurred())

	want := &corev1.ConfigMap{}
	Expect(yaml.Unmarshal(data, want)).To(Succeed())
	want.Namespace = operatorNamespace

	applyConfig := corev1apply.ConfigMap(want.Name, want.Namespace).WithData(want.Data)

	Expect(testClient().Apply(ctx, applyConfig,
		client.ForceOwnership, client.FieldOwner(devSetupFieldOwner))).To(Succeed())

	got := &corev1.ConfigMap{}
	Expect(testClient().Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: want.Name}, got)).To(Succeed())
	Expect(got.Data).To(Equal(want.Data))
}

// Serial: KuadrantControlPlane is a cluster-scoped singleton. Destructive tests
// (deletion, drift) must not run in parallel with status or deployment tests.
var _ = Describe("KuadrantControlPlane controller", Serial, func() {
	var (
		testTimeOut      = SpecTimeout(2 * time.Minute)
		afterEachTimeOut = NodeTimeout(3 * time.Minute)
	)

	AfterEach(func(ctx SpecContext) {
		// Ensure the default CR is restored if a test deleted it.
		cp := &kuadrantv1alpha1.KuadrantControlPlane{}
		err := testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)
		if apierrors.IsNotFound(err) {
			// The same deletion that took out the CR cascade-deleted
			// everything it owned, including dns-operator's env ConfigMap
			// carrying the local-dev "inmemory" provider override. GC
			// processes that cascade asynchronously, so wait for the old
			// ConfigMap to actually be gone before restoring the override.
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				err := testClient().Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: dnsOperatorEnvConfigMap}, cm)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected dns-operator env ConfigMap to be cascade-deleted, got error: %v", err)
			}).WithContext(ctx).Should(Succeed())

			// Restore the override before recreating the CR.
			restoreDevEnvOverrides(ctx)

			cp = &kuadrantv1alpha1.KuadrantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName},
			}
			_ = testClient().Create(ctx, cp)
		}
	}, afterEachTimeOut)

	Context("auto-creation on startup", func() {
		It("creates a default KuadrantControlPlane CR", func(ctx SpecContext) {
			cp := &kuadrantv1alpha1.KuadrantControlPlane{}
			err := testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)
			Expect(err).ToNot(HaveOccurred())
			Expect(cp.Name).To(Equal(kuadrantv1alpha1.KuadrantControlPlaneDefaultName))
		}, testTimeOut)

		// Events are stored by the API server — requires a real cluster (UseExistingCluster: true).
		It("emits an OLM migration event on the KuadrantControlPlane", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				events := &corev1.EventList{}
				g.Expect(testClient().List(ctx, events, client.InNamespace("default"))).To(Succeed())

				var found bool
				for _, e := range events.Items {
					if e.InvolvedObject.Name != kuadrantv1alpha1.KuadrantControlPlaneDefaultName {
						continue
					}
					if e.Reason == "OLMMigrationComplete" || e.Reason == "OLMMigrationIncomplete" {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(), "expected OLM migration event on KuadrantControlPlane")
			}).WithContext(ctx).Should(Succeed())
		}, SpecTimeout(10*time.Second))
	})

	Context("singleton enforcement", func() {
		It("rejects creation of KuadrantControlPlane with non-default name", func(ctx SpecContext) {
			cp := &kuadrantv1alpha1.KuadrantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "not-default"},
			}
			err := testClient().Create(ctx, cp)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err) || apierrors.IsForbidden(err)).To(BeTrue(),
				"expected invalid or forbidden error, got: %v", err)
		}, testTimeOut)
	})

	Context("component deployment", func() {
		It("deploys dns-operator Deployment in operator namespace", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: operatorNamespace,
					Name:      dnsOperatorDeployment,
				}, deploy)).To(Succeed())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("sets a controller ownerReference to the KuadrantControlPlane on dns-operator Deployment", func(ctx SpecContext) {
			cp := &kuadrantv1alpha1.KuadrantControlPlane{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{
					Namespace: operatorNamespace,
					Name:      dnsOperatorDeployment,
				}, deploy)).To(Succeed())

				owner := metav1.GetControllerOf(deploy)
				g.Expect(owner).ToNot(BeNil())
				g.Expect(owner.Kind).To(Equal("KuadrantControlPlane"))
				g.Expect(owner.Name).To(Equal(cp.Name))
				g.Expect(owner.UID).To(Equal(cp.GetUID()))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})

	Context("status reporting", func() {
		It("reports Ready=True when dns-operator is available", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1alpha1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				cond := meta.FindStatusCondition(cp.Status.Conditions, kuadrantv1alpha1.ControlPlaneConditionReady)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(kuadrantv1alpha1.ControlPlaneReasonComponentsHealthy))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports component status with CRD establishment", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1alpha1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				g.Expect(cp.Status.Components).ToNot(BeEmpty())
				var dnsComponent *kuadrantv1alpha1.ComponentStatus
				for i := range cp.Status.Components {
					if cp.Status.Components[i].Name == "dns-operator" {
						dnsComponent = &cp.Status.Components[i]
						break
					}
				}
				g.Expect(dnsComponent).ToNot(BeNil(), "dns-operator component not found in status")
				g.Expect(dnsComponent.Ready).To(BeTrue())
				g.Expect(dnsComponent.CRDs).To(HaveLen(2))
				for _, crd := range dnsComponent.CRDs {
					g.Expect(crd.Established).To(BeTrue())
				}
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports chart version for each component", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1alpha1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				for _, cs := range cp.Status.Components {
					g.Expect(cs.ChartVersion).ToNot(BeEmpty(), "component %s should have a chart version", cs.Name)
				}
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports dns-operator image status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1alpha1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				var dns *kuadrantv1alpha1.ComponentStatus
				for i := range cp.Status.Components {
					if cp.Status.Components[i].Name == "dns-operator" {
						dns = &cp.Status.Components[i]
						break
					}
				}
				g.Expect(dns).ToNot(BeNil(), "dns-operator component not found in status")
				g.Expect(dns.Images).ToNot(BeEmpty())
				g.Expect(dns.Images[0].Name).To(Equal("manager"))
				g.Expect(dns.Images[0].Image).ToNot(BeEmpty())
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("reports mcp-gateway image status", func(ctx SpecContext) {
			Eventually(func(g Gomega) {
				cp := &kuadrantv1alpha1.KuadrantControlPlane{}
				g.Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())

				var mcp *kuadrantv1alpha1.ComponentStatus
				for i := range cp.Status.Components {
					if cp.Status.Components[i].Name == "mcp-gateway" {
						mcp = &cp.Status.Components[i]
						break
					}
				}
				g.Expect(mcp).ToNot(BeNil(), "mcp-gateway component not found in status")
				g.Expect(mcp.Images).ToNot(BeEmpty())

				imageNames := map[string]bool{}
				for _, img := range mcp.Images {
					g.Expect(img.Image).ToNot(BeEmpty())
					imageNames[img.Name] = true
				}
				g.Expect(imageNames).To(HaveKey("mcp-controller"))
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

	})

	Context("deletion", func() {
		It("does not recreate KuadrantControlPlane CR when deleted", func(ctx SpecContext) {
			cpKey := client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}

			cp := &kuadrantv1alpha1.KuadrantControlPlane{}
			Expect(testClient().Get(ctx, cpKey, cp)).To(Succeed())

			Expect(testClient().Delete(ctx, cp)).To(Succeed())

			// The reconcile loop no longer self-heals a deleted CR -- only the
			// one-shot BootstrapRunnable creates it, at manager startup. Since
			// the manager is already running for the whole test suite, deleting
			// it here must not bring it back on its own.
			Consistently(func(g Gomega) {
				got := &kuadrantv1alpha1.KuadrantControlPlane{}
				err := testClient().Get(ctx, cpKey, got)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected KuadrantControlPlane to remain deleted, got error: %v", err)
			}, 5*time.Second, 1*time.Second).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("cascade-deletes child Deployments when KuadrantControlPlane is deleted", func(ctx SpecContext) {
			deployKey := client.ObjectKey{Namespace: operatorNamespace, Name: dnsOperatorDeployment}

			// Ensure dns-operator is running and owned by the KuadrantControlPlane
			var cpUID types.UID
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, deployKey, deploy)).To(Succeed())
				g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">", 0))

				owner := metav1.GetControllerOf(deploy)
				g.Expect(owner).ToNot(BeNil(), "expected dns-operator Deployment to have a controller ownerReference")
				g.Expect(owner.Kind).To(Equal("KuadrantControlPlane"))
				cpUID = owner.UID
			}).WithContext(ctx).Should(Succeed())

			cp := &kuadrantv1alpha1.KuadrantControlPlane{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: kuadrantv1alpha1.KuadrantControlPlaneDefaultName}, cp)).To(Succeed())
			Expect(cp.GetUID()).To(Equal(cpUID), "expected Deployment to be owned by the current KuadrantControlPlane instance")
			Expect(testClient().Delete(ctx, cp)).To(Succeed())

			// Garbage collection cascade-deletes the Deployment once its owner is gone.
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				err := testClient().Get(ctx, deployKey, deploy)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected dns-operator Deployment to be cascade-deleted, got error: %v", err)
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)

		It("does not set an ownerReference on CRDs", func(ctx SpecContext) {
			// Deleting a CRD deletes every custom resource of that type
			// cluster-wide, so CRDs must never be tied to the KCP's lifecycle
			// regardless of what happens to Deployments/Services/etc.
			crd := &apiextensionsv1.CustomResourceDefinition{}
			Expect(testClient().Get(ctx, client.ObjectKey{Name: "dnsrecords.kuadrant.io"}, crd)).To(Succeed())
			Expect(crd.OwnerReferences).To(BeEmpty(), "CRDs must not have owner references")
		}, testTimeOut)
	})

	Context("drift reconciliation", func() {
		It("recreates dns-operator Deployment when deleted", func(ctx SpecContext) {
			deployKey := client.ObjectKey{Namespace: operatorNamespace, Name: dnsOperatorDeployment}

			// Ensure it exists first and capture UID
			var originalUID types.UID
			Eventually(func(g Gomega) {
				deploy := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, deployKey, deploy)).To(Succeed())
				originalUID = deploy.GetUID()
			}).WithContext(ctx).Should(Succeed())

			// Delete the Deployment
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorNamespace,
					Name:      dnsOperatorDeployment,
				},
			}
			Expect(testClient().Delete(ctx, deploy)).To(Succeed())

			// Should be recreated with a new UID
			Eventually(func(g Gomega) {
				recreated := &appsv1.Deployment{}
				g.Expect(testClient().Get(ctx, deployKey, recreated)).To(Succeed())
				g.Expect(recreated.GetUID()).ToNot(Equal(originalUID), "expected a new Deployment, not the old one")
			}).WithContext(ctx).Should(Succeed())
		}, testTimeOut)
	})
})
