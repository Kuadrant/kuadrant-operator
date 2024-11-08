//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestNewGatewayEventMapper(t *testing.T) {
	testScheme := runtime.NewScheme()

	err := appsv1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	err = gatewayapiv1.Install(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	err = kuadrantv1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	clientBuilder := func(objs []runtime.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(testScheme).
			WithRuntimeObjects(objs...).
			WithIndex(&gatewayapiv1.HTTPRoute{}, fieldindexers.HTTPRouteGatewayParentField, func(rawObj client.Object) []string {
				route, assertionOk := rawObj.(*gatewayapiv1.HTTPRoute)
				if !assertionOk {
					return nil
				}

				return utils.Map(kuadrantgatewayapi.GetGatewayParentKeys(route), func(key client.ObjectKey) string {
					return key.String()
				})
			}).
			Build()
	}

	t.Run("not gateway related event", func(subT *testing.T) {
		objs := []runtime.Object{}
		cl := clientBuilder(objs)
		em := NewGatewayEventMapper(&rateLimitPolicyType{}, WithClient(cl), WithLogger(log.NewLogger()))
		requests := em.Map(ctx, &gatewayapiv1.HTTPRoute{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - no policies - no requests", func(subT *testing.T) {
		objs := []runtime.Object{}
		cl := clientBuilder(objs)
		em := NewGatewayEventMapper(&rateLimitPolicyType{}, WithClient(cl), WithLogger(log.NewLogger()))
		requests := em.Map(ctx, &gatewayapiv1.Gateway{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - requests", func(subT *testing.T) {
		gw := gatewayFactory("ns-a", "gw-1")
		route := routeFactory("ns-a", "route-1", gatewayapiv1.ParentReference{Name: "gw-1"})
		pGw := policyFactory("ns-a", "pRoute", gatewayapiv1alpha2.LocalPolicyTargetReference{
			Group: gatewayapiv1.GroupName,
			Kind:  "HTTPRoute",
			Name:  gatewayapiv1.ObjectName("route-1"),
		})
		pRoute := policyFactory("ns-a", "pGw", gatewayapiv1alpha2.LocalPolicyTargetReference{
			Group: gatewayapiv1.GroupName,
			Kind:  "Gateway",
			Name:  gatewayapiv1.ObjectName("gw-1"),
		})
		objs := []runtime.Object{gw, route, pGw, pRoute}
		cl := clientBuilder(objs)
		em := NewGatewayEventMapper(&rateLimitPolicyType{}, WithClient(cl), WithLogger(log.NewLogger()))
		requests := em.Map(ctx, gw)
		assert.Equal(subT, len(requests), 2)
		assert.Assert(subT, utils.Index(requests, func(r reconcile.Request) bool {
			return r.NamespacedName == types.NamespacedName{Namespace: "ns-a", Name: "pGw"}
		}) >= 0)
		assert.Assert(subT, utils.Index(requests, func(r reconcile.Request) bool {
			return r.NamespacedName == types.NamespacedName{Namespace: "ns-a", Name: "pRoute"}
		}) >= 0)
	})
}

const (
	RateLimitPolicyBackReferenceAnnotationName   = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyDirectReferenceAnnotationName = "kuadrant.io/ratelimitpolicy"
)

type rateLimitPolicyType struct{}

func (r rateLimitPolicyType) GetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   kuadrantv1.GroupVersion.Group,
		Version: kuadrantv1.GroupVersion.Version,
		Kind:    "RateLimitPolicy",
	}
}
func (r rateLimitPolicyType) GetInstance() client.Object {
	return &kuadrantv1.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantv1.RateLimitPolicyGroupKind.Kind,
			APIVersion: kuadrantv1.GroupVersion.String(),
		},
	}
}

func (r rateLimitPolicyType) BackReferenceAnnotationName() string {
	return RateLimitPolicyBackReferenceAnnotationName
}

func (r rateLimitPolicyType) DirectReferenceAnnotationName() string {
	return RateLimitPolicyDirectReferenceAnnotationName
}

func (r rateLimitPolicyType) GetList(ctx context.Context, cl client.Client, listOpts ...client.ListOption) ([]kuadrantgatewayapi.Policy, error) {
	rlpList := &kuadrantv1.RateLimitPolicyList{}
	err := cl.List(ctx, rlpList, listOpts...)
	if err != nil {
		return nil, err
	}
	return utils.Map(rlpList.Items, func(p kuadrantv1.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p }), nil
}
