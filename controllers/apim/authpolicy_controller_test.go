//go:build integration

package apim

import (
	"context"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/kuadrant/authorino/api/v1beta1"
	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	secv1beta1resources "istio.io/client-go/pkg/apis/security/v1beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const GatewayName = "kuadrant-gwapi-gateway"

var _ = Describe("AuthPolicy controller", func() {
	var testNamespace string

	BeforeEach(func() {
		var generatedTestNamespace = "test-namespace-" + uuid.New().String()

		namespace := &v1.Namespace{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{Name: generatedTestNamespace},
		}

		err := k8sClient.Create(context.Background(), namespace)
		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: generatedTestNamespace}, existingNamespace)
			if err != nil {
				return false
			}
			return true
		}, time.Minute, 5*time.Second).Should(BeTrue())

		testNamespace = existingNamespace.Name
	})

	AfterEach(func() {
		desiredTestNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		err := k8sClient.Delete(context.Background(), desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))

		Expect(err).ToNot(HaveOccurred())

		existingNamespace := &v1.Namespace{}
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNamespace}, existingNamespace)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}, time.Minute, 5*time.Second).Should(BeTrue())
	})

	Context("Attach to HTTPRoute and Gateway", func() {
		It("Should create and delete everything successfully", func() {
			err := ApplyResources(filepath.Join("..", "..", "examples", "toystore", "toystore.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			err = ApplyResources(filepath.Join("..", "..", "examples", "toystore", "httproute.yaml"), k8sClient, testNamespace)
			Expect(err).ToNot(HaveOccurred())

			authpolicies := authPolicies(testNamespace)

			// creating authpolicies
			for idx := range authpolicies {
				err = k8sClient.Create(context.Background(), authpolicies[idx])
				logf.Log.V(1).Info("Creating AuthPolicy", "key", client.ObjectKeyFromObject(authpolicies[idx]).String(), "error", err)
				Expect(err).ToNot(HaveOccurred())

				// check Istio's AuthorizationPolicy existence
				iap := &secv1beta1resources.AuthorizationPolicy{}
				iapKey := types.NamespacedName{
					Name:      getAuthPolicyName(GatewayName, string(authpolicies[idx].Spec.TargetRef.Name), string(authpolicies[idx].Spec.TargetRef.Kind)),
					Namespace: common.KuadrantNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), iapKey, iap)
					logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
					if err != nil && !apierrors.IsAlreadyExists(err) {
						return false
					}
					return true
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())

				// check Authorino's AuthConfig existence
				ac := &authorinov1beta1.AuthConfig{}
				acKey := types.NamespacedName{
					Name:      authConfigName(client.ObjectKeyFromObject(authpolicies[idx])),
					Namespace: common.KuadrantNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), acKey, ac)
					logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", acKey.String(), "error", err)
					if err != nil && !apierrors.IsAlreadyExists(err) {
						return false
					}
					return true
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			}

			// deleting authpolicies
			for idx := range authpolicies {
				err = k8sClient.Delete(context.Background(), authpolicies[idx])
				logf.Log.V(1).Info("Deleting AuthPolicy", "key", client.ObjectKeyFromObject(authpolicies[idx]).String(), "error", err)
				Expect(err).ToNot(HaveOccurred())

				// check Istio's AuthorizationPolicy existence
				iap := &secv1beta1resources.AuthorizationPolicy{}
				iapKey := types.NamespacedName{
					Name:      getAuthPolicyName(GatewayName, string(authpolicies[idx].Spec.TargetRef.Name), string(authpolicies[idx].Spec.TargetRef.Kind)),
					Namespace: common.KuadrantNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), iapKey, iap)
					logf.Log.V(1).Info("Fetching Istio's AuthorizationPolicy", "key", iapKey.String(), "error", err)
					if err != nil && apierrors.IsNotFound(err) {
						return true
					}
					return false
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())

				// check Authorino's AuthConfig existence
				ac := &authorinov1beta1.AuthConfig{}
				acKey := types.NamespacedName{
					Name:      authConfigName(client.ObjectKeyFromObject(authpolicies[idx])),
					Namespace: common.KuadrantNamespace,
				}
				Eventually(func() bool {
					err := k8sClient.Get(context.Background(), acKey, ac)
					logf.Log.V(1).Info("Fetching Authorino's AuthConfig", "key", acKey.String(), "error", err)
					if err != nil && apierrors.IsNotFound(err) {
						return true
					}
					return false
				}, 2*time.Minute, 5*time.Second).Should(BeTrue())
			}
		})

	})
})

func authPolicies(namespace string) []*apimv1alpha1.AuthPolicy {
	routePolicy := &apimv1alpha1.AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "target-route",
			Namespace: namespace,
		},
		Spec: apimv1alpha1.AuthPolicySpec{
			TargetRef: v1alpha2.PolicyTargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  "toystore",
			},
			AuthRules: []*apimv1alpha1.AuthRule{
				{
					Hosts:   []string{"*.toystore.com"},
					Methods: []string{"DELETE", "POST"},
					Paths:   []string{"/admin*"},
				},
			},
			AuthScheme: &v1beta1.AuthConfigSpec{
				Hosts: []string{"api.toystore.com"},
				Identity: []*v1beta1.Identity{
					{
						Name: "apiKey",
						APIKey: &v1beta1.Identity_APIKey{
							LabelSelectors: map[string]string{
								"app": "toystore",
							},
						},
						Credentials: v1beta1.Credentials{
							In: v1beta1.Credentials_In(
								"authorization_header",
							),
							KeySelector: "APIKEY",
						},
					},
				},
			},
		},
	}

	gatewayPolicy := routePolicy.DeepCopy()
	gatewayPolicy.SetName("target-gateway")
	gatewayPolicy.SetNamespace(common.KuadrantNamespace)
	gatewayPolicy.Spec.TargetRef.Kind = "Gateway"
	gatewayPolicy.Spec.TargetRef.Name = GatewayName
	gatewayPolicy.Spec.AuthRules = []*apimv1alpha1.AuthRule{
		{Hosts: []string{"*.toystore.com"}},
	}
	gatewayPolicy.Spec.AuthScheme.Identity[0].APIKey.LabelSelectors["admin"] = "yes"

	return []*apimv1alpha1.AuthPolicy{routePolicy, gatewayPolicy}
}
