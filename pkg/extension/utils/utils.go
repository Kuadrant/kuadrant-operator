package utils

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/policy-machinery/controller"
)

func LoggerFromContext(ctx context.Context) logr.Logger {
	return controller.LoggerFromContext(ctx)
}

func DynamicClientFromContext(ctx context.Context) (*dynamic.DynamicClient, error) {
	dynamicClient, ok := ctx.Value((*dynamic.DynamicClient)(nil)).(*dynamic.DynamicClient)
	if !ok {
		return nil, errors.New("failed to retrieve dynamic client from context")
	}
	return dynamicClient, nil
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
