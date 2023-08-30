package common

import (
	"encoding/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildBackRefs(refs []client.ObjectKey) (string, error) {
	if len(refs) == 0 {
		return "", nil
	}

	var uniqueKeys []client.ObjectKey

	for _, v := range refs {
		if !Contains(uniqueKeys, v) {
			uniqueKeys = append(uniqueKeys, v)
		}
	}

	serialized, err := json.Marshal(uniqueKeys)
	if err != nil {
		return "", err
	}
	return string(serialized), nil
}
