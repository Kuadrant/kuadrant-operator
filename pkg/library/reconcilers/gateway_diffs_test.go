package reconcilers

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func TestGatewaysMissingPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1.GatewayList{
		Items: []gatewayapiv1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyKind := &utils.PolicyKindStub{}
	gwName := func(gw utils.GatewayWrapper) string { return gw.Gateway.Name }

	gws = utils.Map(gatewaysMissingPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if !slices.Contains(gws, "gw-3") {
		t.Error("gateway expected to be listed as missing policy ref")
	}

	gws = utils.Map(gatewaysMissingPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}

	gws = utils.Map(gatewaysMissingPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if !slices.Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as missing policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as missing policy ref")
	}
	if !slices.Contains(gws, "gw-3") {
		t.Error("gateway expected to be listed as missing policy ref")
	}
}

func TestGatewaysWithValidPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1.GatewayList{
		Items: []gatewayapiv1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyKind := &utils.PolicyKindStub{}
	gwName := func(gw utils.GatewayWrapper) string { return gw.Gateway.Name }

	gws = utils.Map(gatewaysWithValidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if !slices.Contains(gws, "gw-2") {
		t.Error("gateway expected to be listed as with valid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}

	gws = utils.Map(gatewaysWithValidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyKind), gwName)

	if !slices.Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as with valid policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}

	gws = utils.Map(gatewaysWithValidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with valid policy ref")
	}
}

func TestGatewaysWithInvalidPolicyRef(t *testing.T) {
	gwList := &gatewayapiv1.GatewayList{
		Items: []gatewayapiv1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-1",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "gw-ns",
					Name:        "gw-2",
					Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"}]`},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gw-ns",
					Name:      "gw-3",
				},
			},
		},
	}

	var gws []string
	policyKind := &utils.PolicyKindStub{}
	gwName := func(gw utils.GatewayWrapper) string { return gw.Gateway.Name }

	gws = utils.Map(gatewaysWithInvalidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-1"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-2"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if !slices.Contains(gws, "gw-1") {
		t.Error("gateway expected to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}

	gws = utils.Map(gatewaysWithInvalidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-2"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}

	gws = utils.Map(gatewaysWithInvalidPolicyRef(gwList, client.ObjectKey{Namespace: "app-ns", Name: "policy-3"}, []client.ObjectKey{
		{Namespace: "gw-ns", Name: "gw-1"},
		{Namespace: "gw-ns", Name: "gw-3"},
	}, policyKind), gwName)

	if slices.Contains(gws, "gw-1") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-2") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
	if slices.Contains(gws, "gw-3") {
		t.Error("gateway expected not to be listed as with invalid policy ref")
	}
}

func TestTargetedGatewayKeys(t *testing.T) {
	var (
		namespace = "operator-unittest"
		routeName = "my-route"
	)

	s := scheme.Scheme
	err := appsv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	httpRoute := &gatewayapiv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespace,
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{
					{
						Name: "gwName",
					},
				},
			},
		},
		Status: gatewayapiv1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: []gatewayapiv1.RouteParentStatus{
					{
						ParentRef: gatewayapiv1.ParentReference{
							Name: "gwName",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
	}

	keys := targetedGatewayKeys(httpRoute)

	if len(keys) != 1 {
		t.Fatalf("gateway key slice length is %d and it was expected to be 1", len(keys))
	}

	expectedKey := client.ObjectKey{Name: "gwName", Namespace: namespace}

	if keys[0] != expectedKey {
		t.Fatalf("gwKey value (%+v) does not match expected (%+v)", keys[0], expectedKey)
	}
}
