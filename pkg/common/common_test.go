//go:build unit

package common

import (
	"os"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestValidSubdomains(t *testing.T) {
	testCases := []struct {
		name             string
		domains          []string
		subdomains       []string
		expected         bool
		expectedHostname string
	}{
		{"nil", nil, nil, true, ""},
		{"nil subdomains", []string{"*.example.com"}, nil, true, ""},
		{"nil domains", nil, []string{"*.example.com"}, false, "*.example.com"},
		{"dot matters", []string{"*.example.com"}, []string{"example.com"}, false, "example.com"},
		{"dot matters2", []string{"example.com"}, []string{"*.example.com"}, false, "*.example.com"},
		{"happy path", []string{"*.example.com", "*.net"}, []string{"*.other.net", "test.example.com"}, true, ""},
		{"not all match", []string{"*.example.com", "*.net"}, []string{"*.other.com", "*.example.com"}, false, "*.other.com"},
		{"wildcard in subdomains does not match", []string{"*.example.com", "*.net"}, []string{"*", "*.example.com"}, false, "*"},
		{"wildcard in domains matches all", []string{"*", "*.net"}, []string{"*.net", "*.example.com"}, true, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			valid, hostname := ValidSubdomains(tc.domains, tc.subdomains)
			if valid != tc.expected {
				subT.Errorf("expected (%t), got (%t)", tc.expected, valid)
			}
			if hostname != tc.expectedHostname {
				subT.Errorf("expected hostname (%s), got (%s)", tc.expectedHostname, hostname)
			}
		})
	}
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

func TestContains(t *testing.T) {
	testCases := []struct {
		name     string
		slice    []string
		target   string
		expected bool
	}{
		{
			name:     "when slice has one target item then return true",
			slice:    []string{"test-gw"},
			target:   "test-gw",
			expected: true,
		},
		{
			name:     "when slice is empty then return false",
			slice:    []string{},
			target:   "test-gw",
			expected: false,
		},
		{
			name:     "when target is in a slice then return true",
			slice:    []string{"test-gw1", "test-gw2", "test-gw3"},
			target:   "test-gw2",
			expected: true,
		},
		{
			name:     "when no target in a slice then return false",
			slice:    []string{"test-gw1", "test-gw2", "test-gw3"},
			target:   "test-gw4",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if Contains(tc.slice, tc.target) != tc.expected {
				t.Errorf("when slice=%v and target=%s, expected=%v, but got=%v", tc.slice, tc.target, tc.expected, !tc.expected)
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

func TestSliceCopy(t *testing.T) {
	input1 := []int{1, 2, 3}
	expected1 := []int{1, 2, 3}
	output1 := SliceCopy(input1)
	t.Run("when given slice of integers then return a copy of the input slice", func(t *testing.T) {
		if !reflect.DeepEqual(output1, expected1) {
			t.Errorf("SliceCopy(%v) = %v; expected %v", input1, output1, expected1)
		}
	})

	input2 := []string{"foo", "bar", "baz"}
	expected2 := []string{"foo", "bar", "baz"}
	output2 := SliceCopy(input2)
	t.Run("when given slice of strings then return a copy of the input slice", func(t *testing.T) {
		if !reflect.DeepEqual(output2, expected2) {
			t.Errorf("SliceCopy(%v) = %v; expected %v", input2, output2, expected2)
		}
	})

	type person struct {
		name string
		age  int
	}
	input3 := []person{{"Artem", 65}, {"DD", 18}, {"Charlie", 23}}
	expected3 := []person{{"Artem", 65}, {"DD", 18}, {"Charlie", 23}}
	output3 := SliceCopy(input3)
	t.Run("when given slice of structs then return a copy of the input slice", func(t *testing.T) {
		if !reflect.DeepEqual(output3, expected3) {
			t.Errorf("SliceCopy(%v) = %v; expected %v", input3, output3, expected3)
		}
	})

	input4 := []int{1, 2, 3}
	expected4 := []int{1, 2, 3}
	output4 := SliceCopy(input4)
	t.Run("when modifying the original input slice then does not affect the returned copy", func(t *testing.T) {
		if !reflect.DeepEqual(output4, expected4) {
			t.Errorf("SliceCopy(%v) = %v; expected %v", input4, output4, expected4)
		}
		input4[0] = 4
		if reflect.DeepEqual(output4, input4) {
			t.Errorf("modifying the original input slice should not change the output slice")
		}
	})
}

func TestReverseSlice(t *testing.T) {
	input1 := []int{1, 2, 3}
	expected1 := []int{3, 2, 1}
	output1 := ReverseSlice(input1)
	t.Run("when given slice of integers then return reversed copy of the input slice", func(t *testing.T) {
		if !reflect.DeepEqual(output1, expected1) {
			t.Errorf("ReverseSlice(%v) = %v; expected %v", input1, output1, expected1)
		}
	})

	input2 := []string{"foo", "bar", "baz"}
	expected2 := []string{"baz", "bar", "foo"}
	output2 := ReverseSlice(input2)
	t.Run("when given slice of strings then return reversed copy of the input slice", func(t *testing.T) {
		if !reflect.DeepEqual(output2, expected2) {
			t.Errorf("ReverseSlice(%v) = %v; expected %v", input2, output2, expected2)
		}
	})

	input3 := []int{}
	expected3 := []int{}
	output3 := ReverseSlice(input3)
	t.Run("when given an empty slice then return empty slice", func(t *testing.T) {
		if !reflect.DeepEqual(output3, expected3) {
			t.Errorf("ReverseSlice(%v) = %v; expected %v", input3, output3, expected3)
		}
	})
}

func TestMergeMapStringString(t *testing.T) {
	testCases := []struct {
		name          string
		existing      map[string]string
		desired       map[string]string
		expected      bool
		expectedState map[string]string
	}{
		{
			name:          "when existing and desired are empty then return false and not modify the existing map",
			existing:      map[string]string{},
			desired:       map[string]string{},
			expected:      false,
			expectedState: map[string]string{},
		},
		{
			name:          "when existing is empty and desired has values then return true and set the values in the existing map",
			existing:      map[string]string{},
			desired:       map[string]string{"a": "1", "b": "2"},
			expected:      true,
			expectedState: map[string]string{"a": "1", "b": "2"},
		},
		{
			name:          "when existing has some values and desired has different/new values then return true and modify the existing map",
			existing:      map[string]string{"a": "1", "b": "2"},
			desired:       map[string]string{"a": "3", "c": "4"},
			expected:      true,
			expectedState: map[string]string{"a": "3", "b": "2", "c": "4"},
		},
		{
			name:          "when existing has all the values from desired then return false and not modify the existing map",
			existing:      map[string]string{"a": "1", "b": "2"},
			desired:       map[string]string{"a": "1", "b": "2"},
			expected:      false,
			expectedState: map[string]string{"a": "1", "b": "2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			existingCopy := make(map[string]string, len(tc.existing))
			for k, v := range tc.existing {
				existingCopy[k] = v
			}
			modified := MergeMapStringString(&existingCopy, tc.desired)

			if modified != tc.expected {
				t.Errorf("MergeMapStringString(%v, %v) returned %v; expected %v", tc.existing, tc.desired, modified, tc.expected)
			}

			if !reflect.DeepEqual(existingCopy, tc.expectedState) {
				t.Errorf("MergeMapStringString(%v, %v) modified the existing map to %v; expected %v", tc.existing, tc.desired, existingCopy, tc.expectedState)
			}
		})
	}
}
