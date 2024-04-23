package utils

import "slices"

// GetEmptySliceIfNil returns a provided slice, or an empty slice of the same type if the input slice is nil.
func GetEmptySliceIfNil[T any](val []T) []T {
	if val == nil {
		return make([]T, 0)
	}
	return val
}

// SameElements checks if the two slices contain the exact same elements. Order does not matter.
func SameElements[T comparable](s1, s2 []T) bool {
	if len(s1) != len(s2) {
		return false
	}
	for _, v := range s1 {
		if !slices.Contains(s2, v) {
			return false
		}
	}
	return true
}

func Intersect[T comparable](slice1, slice2 []T) bool {
	for _, item := range slice1 {
		if slices.Contains(slice2, item) {
			return true
		}
	}
	return false
}

func Intersection[T comparable](slice1, slice2 []T) []T {
	smallerSlice := slice1
	largerSlice := slice2
	if len(slice1) > len(slice2) {
		smallerSlice = slice2
		largerSlice = slice1
	}
	var result []T
	for _, item := range smallerSlice {
		if slices.Contains(largerSlice, item) {
			result = append(result, item)
		}
	}
	return result
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
