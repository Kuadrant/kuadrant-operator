package slice

// RemoveString returns a newly created []string that contains all items from slice that
// are not equal to s.
func RemoveString(slice []string, s string) []string {
	newSlice := make([]string, 0)
	for _, item := range slice {
		if item == s {
			continue
		}
		newSlice = append(newSlice, item)
	}
	if len(newSlice) == 0 {
		// Sanitize for unit tests so we don't need to distinguish empty array
		// and nil.
		newSlice = nil
	}
	return newSlice
}

// ContainsString checks if a given slice of strings contains the provided string.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func Contains[T any](slice []T, predicate func(T) bool) bool {
	_, ok := Find(slice, predicate)
	return ok
}

// Find checks if an element in slice satisfies the given predicate, and returns
// it. If no element is found returns false
func Find[T any](slice []T, predicate func(T) bool) (element T, ok bool) {
	for _, elem := range slice {
		if predicate(elem) {
			element = elem
			ok = true
			return
		}
	}

	return
}

func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := []T{}
	for _, elem := range slice {
		if predicate(elem) {
			result = append(result, elem)
		}
	}

	return result
}

func Map[T, R any](slice []T, f func(T) R) []R {
	result := make([]R, len(slice))

	for i, elem := range slice {
		result[i] = f(elem)
	}

	return result
}

func MapErr[T, R any](slice []T, f func(T) (R, error)) ([]R, error) {
	result := make([]R, len(slice))

	for i, elem := range slice {
		mapped, err := f(elem)
		if err != nil {
			return nil, err
		}

		result[i] = mapped
	}

	return result, nil
}
