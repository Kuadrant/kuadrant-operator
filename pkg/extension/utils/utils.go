package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/policy-machinery/controller"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type clientKeyType struct{}
type schemeKeyType struct{}

var ClientKey = clientKeyType{}
var SchemeKey = schemeKeyType{}

func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

func ClientFromContext(ctx context.Context) (client.Client, error) {
	client, ok := ctx.Value(ClientKey).(client.Client)
	if !ok {
		return nil, errors.New("failed to retrieve the client from context")
	}
	return client, nil
}

func SchemeFromContext(ctx context.Context) (*runtime.Scheme, error) {
	scheme, ok := ctx.Value(SchemeKey).(*runtime.Scheme)
	if !ok {
		return nil, errors.New("failed to retrieve scheme from context")
	}
	return scheme, nil
}

func MapToExtPolicy(p exttypes.Policy) *extpb.Policy {
	targetRefs := lo.Map(p.GetTargetRefs(), func(targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName, _ int) *extpb.TargetRef {
		return &extpb.TargetRef{
			Group:       string(targetRef.Group),
			Kind:        string(targetRef.Kind),
			Name:        string(targetRef.Name),
			SectionName: string(ptr.Deref(targetRef.SectionName, "")),
		}
	})
	return &extpb.Policy{
		Metadata: &extpb.Metadata{
			Group:     p.GetObjectKind().GroupVersionKind().Group,
			Kind:      p.GetObjectKind().GroupVersionKind().Kind,
			Namespace: p.GetNamespace(),
			Name:      p.GetName(),
		},
		TargetRefs: targetRefs,
	}
}
