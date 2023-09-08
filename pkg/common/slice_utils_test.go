// TODO: move to https://github.com/Kuadrant/gateway-api-machinery

package common

import (
	"reflect"
	"testing"
)

func TestGetEmptySliceIfNil(t *testing.T) {
	t.Run("when a non-nil slice is provided then return same slice", func(t *testing.T) {
		value := []int{1, 2, 3}
		expected := value

		result := GetEmptySliceIfNil(value)

		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when a nil slice is provided then return an empty slice of the same type", func(t *testing.T) {
		var value []int
		expected := []int{}

		result := GetEmptySliceIfNil(value)

		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})
}

func TestFind(t *testing.T) {
	s := []string{"a", "ab", "abc"}

	if r, found := Find(s, func(el string) bool { return el == "ab" }); !found || r == nil || *r != "ab" {
		t.Error("should have found 'ab' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) <= 3 }); !found || r == nil || *r != "a" {
		t.Error("should have found 'a' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) >= 3 }); !found || r == nil || *r != "abc" {
		t.Error("should have found 'abc' in the slice")
	}

	if r, found := Find(s, func(el string) bool { return len(el) == 4 }); found || r != nil {
		t.Error("should not have found anything in the slice")
	}

	i := []int{1, 2, 3}

	if r, found := Find(i, func(el int) bool { return el/3 == 1 }); !found || r == nil || *r != 3 {
		t.Error("should have found 3 in the slice")
	}

	if r, found := Find(i, func(el int) bool { return el == 75 }); found || r != nil {
		t.Error("should not have found anything in the slice")
	}
}

func TestSameElements(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected bool
	}{
		{
			name:     "when slice1 and slice2 contain the same elements then return true",
			slice1:   []string{"test-gw1", "test-gw2", "test-gw3"},
			slice2:   []string{"test-gw1", "test-gw2", "test-gw3"},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 contain unique elements then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3"},
			expected: false,
		},
		{
			name:     "when both slices are empty then return true",
			slice1:   []string{},
			slice2:   []string{},
			expected: true,
		},
		{
			name:     "when both slices are nil then return true",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if SameElements(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected bool
	}{
		{
			name:     "when slice1 and slice2 have one common item then return true",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3", "test-gw4"},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 have no common item then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice1 is empty then return false",
			slice1:   []string{},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice2 is empty then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{},
			expected: false,
		},
		{
			name:     "when both slices are empty then return false",
			slice1:   []string{},
			slice2:   []string{},
			expected: false,
		},
		{
			name:     "when slice1 is nil then return false",
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: false,
		},
		{
			name:     "when slice2 is nil then return false",
			slice1:   []string{"test-gw1", "test-gw2"},
			expected: false,
		},
		{
			name:     "when both slices are nil then return false",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Intersect(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersectWithInts(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []int
		slice2   []int
		expected bool
	}{
		{
			name:     "when slice1 and slice2 have one common item then return true",
			slice1:   []int{1, 2},
			slice2:   []int{1, 3, 4},
			expected: true,
		},
		{
			name:     "when slice1 and slice2 have no common item then return false",
			slice1:   []int{1, 2},
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice1 is empty then return false",
			slice1:   []int{},
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice2 is empty then return false",
			slice1:   []int{1, 2},
			slice2:   []int{},
			expected: false,
		},
		{
			name:     "when both slices are empty then return false",
			slice1:   []int{},
			slice2:   []int{},
			expected: false,
		},
		{
			name:     "when slice1 is nil then return false",
			slice2:   []int{3, 4},
			expected: false,
		},
		{
			name:     "when slice2 is nil then return false",
			slice1:   []int{1, 2},
			expected: false,
		},
		{
			name:     "when both slices are nil then return false",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Intersect(tc.slice1, tc.slice2) != tc.expected {
				t.Errorf("when slice1=%v and slice2=%v, expected=%v, but got=%v", tc.slice1, tc.slice2, tc.expected, !tc.expected)
			}
		})
	}
}

func TestIntersection(t *testing.T) {
	testCases := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected []string
	}{
		{
			name:     "when slice1 and slice2 have one common item then return that item",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw1", "test-gw3", "test-gw4"},
			expected: []string{"test-gw1"},
		},
		{
			name:     "when slice1 and slice2 have no common item then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice1 is empty then return nil",
			slice1:   []string{},
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice2 is empty then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "when both slices are empty then return nil",
			slice1:   []string{},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "when slice1 is nil then return nil",
			slice2:   []string{"test-gw3", "test-gw4"},
			expected: nil,
		},
		{
			name:     "when slice2 is nil then return nil",
			slice1:   []string{"test-gw1", "test-gw2"},
			expected: nil,
		},
		{
			name:     "when both slices are nil then return nil",
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if r := Intersection(tc.slice1, tc.slice2); !reflect.DeepEqual(r, tc.expected) {
				t.Errorf("expected=%v; got=%v", tc.expected, r)
			}
		})
	}
}

func TestMap(t *testing.T) {
	slice1 := []int{1, 2, 3, 4}
	f1 := func(x int) int { return x + 1 }
	expected1 := []int{2, 3, 4, 5}
	result1 := Map(slice1, f1)
	t.Run("when mapping an int slice with an increment function then return new slice with the incremented values", func(t *testing.T) {
		if !reflect.DeepEqual(result1, expected1) {
			t.Errorf("result1 = %v; expected %v", result1, expected1)
		}
	})

	slice2 := []string{"hello", "world", "buz", "a"}
	f2 := func(s string) int { return len(s) }
	expected2 := []int{5, 5, 3, 1}
	result2 := Map(slice2, f2)
	t.Run("when mapping a string slice with string->int mapping then return new slice with the mapped values", func(t *testing.T) {
		if !reflect.DeepEqual(result2, expected2) {
			t.Errorf("result2 = %v; expected %v", result2, expected2)
		}
	})

	slice3 := []int{}
	f3 := func(x int) float32 { return float32(x) / 2 }
	expected3 := []float32{}
	result3 := Map(slice3, f3)
	t.Run("when mapping an empty int slice then return an empty slice", func(t *testing.T) {
		if !reflect.DeepEqual(result3, expected3) {
			t.Errorf("result3 = %v; expected %v", result3, expected3)
		}
	})
}
