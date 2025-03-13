package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func IsCRDInstalled(restMapper meta.RESTMapper, group, kind, version string) (bool, error) {
	_, err := restMapper.RESTMapping(
		schema.GroupKind{Group: group, Kind: kind},
		version,
	)
	if err == nil {
		return true, nil
	}

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	return false, err
}
