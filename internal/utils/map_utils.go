package utils

func MergeMapStringString(existing *map[string]string, desired map[string]string) bool {
	if existing == nil {
		return false
	}
	if *existing == nil {
		*existing = map[string]string{}
	}

	// for each desired key value set, e.g. labels
	// check if it's present in existing. if not add it to existing.
	// e.g. preserving existing labels while adding those that are in the desired set.
	modified := false
	for desiredKey, desiredValue := range desired {
		if existingValue, exists := (*existing)[desiredKey]; !exists || existingValue != desiredValue {
			(*existing)[desiredKey] = desiredValue
			modified = true
		}
	}
	return modified
}
