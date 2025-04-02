package utils

// GetEmptySliceIfNil returns a provided slice, or an empty slice of the same type if the input slice is nil.
func GetEmptySliceIfNil[T any](val []T) []T {
	if val == nil {
		return make([]T, 0)
	}
	return val
}

func Index[T any](slice []T, match func(T) bool) int {
	for i, item := range slice {
		if match(item) {
			return i
		}
	}
	return -1
}

func Find[T any](slice []T, match func(T) bool) (*T, bool) {
	if i := Index(slice, match); i >= 0 {
		return &slice[i], true
	}
	return nil, false
}

// Map applies the given mapper function to each element in the input slice and returns a new slice with the results.
func Map[T, U any](slice []T, f func(T) U) []U {
	if slice == nil {
		return nil
	}

	arr := make([]U, len(slice))
	for i, e := range slice {
		arr[i] = f(e)
	}
	return arr
}

// Filter filters the input slice using the given predicate function and returns a new slice with the results.
func Filter[T any](slice []T, f func(T) bool) []T {
	arr := make([]T, 0)
	for _, e := range slice {
		if f(e) {
			arr = append(arr, e)
		}
	}
	return arr
}
