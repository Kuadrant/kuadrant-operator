//go:build unit

package metadata

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_addFinalizer(t *testing.T) {
	testCases := []struct {
		name      string //for name of test
		obj       metav1.Object
		finalizer string
		verify    func(obj metav1.Object, t *testing.T) //what we want to verify
	}{
		{
			name: "adding a finalizer when finalizers are nil",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Finalizers: nil,
				},
			},
			finalizer: "test-finalizer",
			verify: assertFinalizers([]string{
				"test-finalizer",
			}),
		},
		{
			name: "adding a finalizer when finalizers are empty",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Finalizers: []string{}, //this is an empty map
				},
			},
			finalizer: "test-finalizer",
			verify: assertFinalizers([]string{
				"test-finalizer",
			}),
		},
		{
			name: "adding a finalizer when that finalizer already exists",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"test-finalizer", //finalizer that's stored in the map
					},
				},
			},
			finalizer: "test-finalizer",
			verify: assertFinalizers([]string{
				"test-finalizer",
			}),
		},
		{
			name: "adding a finalizer when that finalizer already exists",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"test-finalizer",
						"second-test-finalizer",
						"third-test-finalizer",
					},
				},
			},
			finalizer: "test-finalizer",
			verify: assertFinalizers([]string{
				"test-finalizer",
				"second-test-finalizer",
				"third-test-finalizer",
			}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			AddFinalizer(testCase.obj, testCase.finalizer)
			testCase.verify(testCase.obj, t)
		})
	}
}

func Test_removeFinalizer(t *testing.T) {

	testCases := []struct {
		name      string
		obj       metav1.Object
		finalizer string
		verify    func(obj metav1.Object, t *testing.T)
	}{
		{
			name: "removing a finalizer when finalizers are nil",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Finalizers: nil,
				},
			},
			finalizer: "test-finalizer", //We are trying to remove this key, even though it doesn't exist
			verify:    assertFinalizers(nil),
		},
		{
			name: "removing a finalizer when finalizers are empty",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Finalizers: []string{}, //this is an empty map
				},
			},
			finalizer: "test-finalizer",
			verify:    assertFinalizers(nil),
		},

		{
			name: "removing an existing finalizer",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"test-finalizer", //finalizer that's stored in the map
					},
				},
			},
			finalizer: "test-finalizer", //this is what we are passing to the function
			verify:    assertFinalizers(nil),
		},
		{
			name: "remove an existing finalizer",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"first-test-finalizer",
						"second-test-finalizer",
						"test-finalizer",
					},
				},
			},
			finalizer: "test-finalizer",
			verify: assertFinalizers([]string{
				"first-test-finalizer",
				"second-test-finalizer",
			}),
		},
		{
			name: "remove a finalizer that does not exist in the map",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"first-test-finalizer",
						"second-test-finalizer",
						"third-test-finalizer",
					},
				},
			},
			finalizer: "fourth-key",
			verify: assertFinalizers([]string{
				"first-test-finalizer",
				"second-test-finalizer",
				"third-test-finalizer",
			}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			RemoveFinalizer(testCase.obj, testCase.finalizer)
			testCase.verify(testCase.obj, t)
		})
	}
}

func Test_hasFinalizer(t *testing.T) {
	testCases := []struct {
		name      string
		obj       metav1.Object
		finalizer string
		expect    bool
	}{
		{
			name: "existing finalizer found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"test-finalizer",
					},
				},
			},
			finalizer: "test-finalizer",
			expect:    true,
		},
		{
			name: "existing finalizer not found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Finalizers: []string{
						"value",
					},
				},
			},
			finalizer: "test-key",
			expect:    false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := HasFinalizer(testCase.obj, testCase.finalizer)
			if !got == testCase.expect {
				t.Errorf("expected '%v' got '%v'", testCase.expect, got)
			}
		})
	}
}

func assertFinalizers(expectedFinalizers []string) func(obj metav1.Object, t *testing.T) {
	return func(obj metav1.Object, t *testing.T) {
		finalizers := obj.GetFinalizers()
		if len(finalizers) != len(expectedFinalizers) {
			t.Fatalf("expected %d finalizer(s), but got %d", len(expectedFinalizers), len(finalizers))
		}

		for _, v := range finalizers {
			found := false
			for _, expectedValue := range expectedFinalizers {
				if v == expectedValue {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected only finalizer value to be '%s' but found '%v'", expectedFinalizers[0], v)
			}
		}
	}
}
