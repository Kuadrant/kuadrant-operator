//go:build integration

package tests

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func BuildBasicGateway(gwName, ns string, mutateFns ...func(*gatewayapiv1.Gateway)) *gatewayapiv1.Gateway {
	gateway := &gatewayapiv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gwName,
			Namespace:   ns,
			Labels:      map[string]string{"app": "rlptest"},
			Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
		},
		Spec: gatewayapiv1.GatewaySpec{
			GatewayClassName: "istio",
			Listeners: []gatewayapiv1.Listener{
				{
					Name:     "default",
					Port:     gatewayapiv1.PortNumber(80),
					Protocol: "HTTP",
				},
			},
		},
	}
	for _, mutateFn := range mutateFns {
		mutateFn(gateway)
	}
	return gateway
}

func DeleteNamespace(ctx context.Context, cl client.Client, namespace string) {
	desiredTestNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Eventually(func(g Gomega) {
		err := cl.Delete(ctx, desiredTestNamespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		g.Expect(err).ToNot(BeNil())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())
}

func DeleteNamespaceCallback(ctx context.Context, cl client.Client, namespace string) func() {
	return func() {
		DeleteNamespace(ctx, cl, namespace)
	}
}

func CreateNamespace(ctx context.Context, cl client.Client) string {
	nsObject := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-namespace-"},
	}
	Expect(cl.Create(ctx, nsObject)).To(Succeed())

	return nsObject.Name
}

func ApplyKuadrantCR(ctx context.Context, cl client.Client, namespace string) {
	ApplyKuadrantCRWithName(ctx, cl, namespace, "kuadrant-sample")
}

func ApplyKuadrantCRWithName(ctx context.Context, cl client.Client, namespace, name string) {
	kuadrantCR := &kuadrantv1beta1.Kuadrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kuadrant",
			APIVersion: kuadrantv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := cl.Create(ctx, kuadrantCR)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() bool {
		kuadrant := &kuadrantv1beta1.Kuadrant{}
		err := cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, kuadrant)
		if err != nil {
			return false
		}
		if !meta.IsStatusConditionTrue(kuadrant.Status.Conditions, "Ready") {
			return false
		}
		return true
	}, time.Minute, 5*time.Second).Should(BeTrue())
}

func GatewayIsReady(ctx context.Context, cl client.Client, gateway *gatewayapiv1.Gateway) func() bool {
	return func() bool {
		existingGateway := &gatewayapiv1.Gateway{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(gateway), existingGateway)
		if err != nil {
			logf.Log.V(1).Info("gateway not read", "gateway", client.ObjectKeyFromObject(gateway), "error", err)
			return false
		}

		if !meta.IsStatusConditionTrue(existingGateway.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed)) {
			logf.Log.V(1).Info("gateway not programmed", "gateway", client.ObjectKeyFromObject(gateway))
			return false
		}

		return true
	}
}

func BuildBasicHttpRoute(routeName, gwName, ns string, hostnames []string, mutateFns ...func(*gatewayapiv1.HTTPRoute)) *gatewayapiv1.HTTPRoute {
	route := &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: gatewayapiv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: ns,
			Labels:    map[string]string{"app": "rlptest"},
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name:      gatewayapiv1.ObjectName(gwName),
						Namespace: ptr.To(gatewayapiv1.Namespace(ns)),
					},
				},
			},
			Hostnames: utils.Map(hostnames, func(hostname string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(hostname) }),
			Rules: []gatewayapiv1.HTTPRouteRule{
				{
					Matches: []gatewayapiv1.HTTPRouteMatch{
						{
							Path: &gatewayapiv1.HTTPPathMatch{
								Type:  ptr.To(gatewayapiv1.PathMatchPathPrefix),
								Value: ptr.To("/toy"),
							},
							Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
						},
					},
				},
			},
		},
	}
	for _, mutateFn := range mutateFns {
		mutateFn(route)
	}
	return route
}

func RouteIsAccepted(ctx context.Context, cl client.Client, routeKey client.ObjectKey) func() bool {
	return func() bool {
		route := &gatewayapiv1.HTTPRoute{}
		err := cl.Get(ctx, routeKey, route)

		if err != nil {
			logf.Log.V(1).Info("httpRoute not read", "route", routeKey, "error", err)
			return false
		}

		if !kuadrantgatewayapi.IsHTTPRouteAccepted(route) {
			logf.Log.V(1).Info("httpRoute not accepted", "route", routeKey)
			return false
		}

		return true
	}
}

func BuildMultipleRulesHttpRoute(routeName, gwName, ns string, hostnames []string) *gatewayapiv1.HTTPRoute {
	route := BuildBasicHttpRoute(routeName, gwName, ns, hostnames)
	route.Spec.Rules = []gatewayapiv1.HTTPRouteRule{
		{ // POST|DELETE /admin*
			Matches: []gatewayapiv1.HTTPRouteMatch{
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/admin"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("POST")),
				},
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/admin"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("DELETE")),
				},
			},
		},
		{ // GET /private*
			Matches: []gatewayapiv1.HTTPRouteMatch{
				{
					Path: &gatewayapiv1.HTTPPathMatch{
						Type:  ptr.To(gatewayapiv1.PathMatchType("PathPrefix")),
						Value: ptr.To("/private"),
					},
					Method: ptr.To(gatewayapiv1.HTTPMethod("GET")),
				},
			},
		},
	}
	return route
}

func DeleteKuadrantCR(ctx context.Context, cl client.Client, namespace string) {
	k := &kuadrantv1beta1.Kuadrant{ObjectMeta: metav1.ObjectMeta{Name: "kuadrant-sample", Namespace: namespace}}
	Eventually(func(g Gomega) {
		err := cl.Delete(ctx, k)
		g.Expect(err).To(HaveOccurred())
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithContext(ctx).Should(Succeed())
}

func RLPIsAccepted(ctx context.Context, cl client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, cl, rlpKey, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func RLPIsEnforced(ctx context.Context, cl client.Client, rlpKey client.ObjectKey) func() bool {
	return RLPIsConditionTrue(ctx, cl, rlpKey, string(kuadrant.PolicyConditionEnforced))
}

func RLPIsConditionTrue(ctx context.Context, cl client.Client, rlpKey client.ObjectKey, condition string) func() bool {
	return func() bool {
		existingRLP := &kuadrantv1beta2.RateLimitPolicy{}
		err := cl.Get(ctx, rlpKey, existingRLP)
		if err != nil {
			logf.Log.V(1).Error(err, "ratelimitpolicy not read", "rlp", rlpKey)
			return false
		}

		return meta.IsStatusConditionTrue(existingRLP.Status.Conditions, condition)
	}
}

func RLPEnforcedCondition(ctx context.Context, cl client.Client, rlpKey client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) bool {
	p := &kuadrantv1beta2.RateLimitPolicy{}
	if err := cl.Get(ctx, rlpKey, p); err != nil {
		return false
	}

	cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
	if cond == nil {
		return false
	}

	return cond.Reason == string(reason) && cond.Message == message
}

func WasmPluginIsAvailable(ctx context.Context, cl client.Client, key client.ObjectKey) func() bool {
	return func() bool {
		wp := &istioclientgoextensionv1alpha1.WasmPlugin{}
		err := cl.Get(ctx, key, wp)
		if err != nil {
			logf.Log.V(1).Info("wasmplugin not read", "key", key, "error", err)
			return false
		}

		// Unfortunately, WasmPlugin does not have status yet
		// Leaving this here for future use
		//if !meta.IsStatusConditionTrue(wp.Status.Conditions, "Available") {
		//	return false
		//}

		return true
	}
}

func IsAuthPolicyAcceptedAndEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, cl, policy)() && IsAuthPolicyEnforced(ctx, cl, policy)()
	}
}

func IsAuthPolicyAcceptedAndNotEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return func() bool {
		return IsAuthPolicyAccepted(ctx, cl, policy)() && !IsAuthPolicyEnforced(ctx, cl, policy)()
	}
}

func IsAuthPolicyAccepted(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, cl, policy, string(gatewayapiv1alpha2.PolicyConditionAccepted))
}

func IsAuthPolicyEnforced(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy) func() bool {
	return IsAuthPolicyConditionTrue(ctx, cl, policy, string(kuadrant.PolicyConditionEnforced))
}

func IsAuthPolicyEnforcedCondition(ctx context.Context, cl client.Client, key client.ObjectKey, reason gatewayapiv1alpha2.PolicyConditionReason, message string) func() bool {
	return func() bool {
		p := &kuadrantv1beta2.AuthPolicy{}
		if err := cl.Get(ctx, key, p); err != nil {
			return false
		}

		cond := meta.FindStatusCondition(p.Status.Conditions, string(kuadrant.PolicyConditionEnforced))
		if cond == nil {
			return false
		}

		return cond.Reason == string(reason) && cond.Message == message
	}
}

func IsAuthPolicyConditionTrue(ctx context.Context, cl client.Client, policy *kuadrantv1beta2.AuthPolicy, condition string) func() bool {
	return func() bool {
		existingPolicy := &kuadrantv1beta2.AuthPolicy{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(policy), existingPolicy)
		return err == nil && meta.IsStatusConditionTrue(existingPolicy.Status.Conditions, condition)
	}
}
