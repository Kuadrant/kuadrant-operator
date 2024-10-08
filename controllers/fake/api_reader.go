//go:build unit

package fake

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type apireader struct {
}

func (a *apireader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	panic("Not Implemented")
}

func (a *apireader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	panic("Not Implemented")
}
