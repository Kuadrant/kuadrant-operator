package apis

import (
	v2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes,
		v2.SchemeBuilder.AddToScheme,
	)
}
