//go:build unit

package metadata

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_addAnnotation(t *testing.T) {
	testCases := []struct {
		name            string //for name of test
		obj             metav1.Object
		annotationKey   string
		annotationValue string
		verify          func(obj metav1.Object, t *testing.T) //what we want to verify
	}{
		{ //first test starts here and...
			name: "adding an annotation when annotations are nil",
			obj: &v1.ConfigMap{ //here we set Annotations to nil
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Annotations: nil,
				},
			}, //next we provide a key name and value
			annotationKey:   "test-key",
			annotationValue: "test-value",
			verify: assertAnnotations(map[string]string{
				"test-key": "test-value",
			}),
		}, //...ends here
		{
			name: "adding an annotation when annotations are empty",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Annotations: map[string]string{}, //this is an empty map
				},
			},
			annotationKey:   "test-key",
			annotationValue: "test-value",
			verify: assertAnnotations(map[string]string{
				"test-key": "test-value",
			}),
		},
		{
			name: "adding an annotation when that annotation already exists",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"test-key": "different-test-value", //annotation that's stored in the map
					},
				},
			},
			annotationKey:   "test-key",
			annotationValue: "test-value",
			verify: assertAnnotations(map[string]string{
				"test-key": "test-value",
			}),
		},
		{
			name: "adding an annotation when that annotation already exists",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"first-key":  "test-value",
						"second-key": "test-value",
						"test-key":   "",
					},
				},
			},
			annotationKey:   "test-key",
			annotationValue: "test-value",
			verify: assertAnnotations(map[string]string{
				"first-key":  "test-value",
				"second-key": "test-value",
				"test-key":   "test-value",
			}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			AddAnnotation(testCase.obj, testCase.annotationKey, testCase.annotationValue)
			testCase.verify(testCase.obj, t)
		})
	}
}

func Test_removeAnnotation(t *testing.T) {

	testCases := []struct {
		name          string //for name of test
		obj           metav1.Object
		annotationKey string
		verify        func(obj metav1.Object, t *testing.T) //what we want to verify
	}{
		{ //first test starts here and...
			name: "removing an annotation when annotations are nil",
			obj: &v1.ConfigMap{ //here we set Annotations to nil
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Annotations: nil,
				},
			}, //next we provide a key name
			annotationKey: "test-key", //We are trying to remove this key, even though it doesn't exist
			verify:        assertAnnotations(nil),
		}, //...ends here
		{
			name: "removing an annotation when annotations are empty",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Annotations: map[string]string{}, //this is an empty map
				},
			},
			annotationKey: "test-key",
			verify:        assertAnnotations(nil),
		},

		{
			name: "removing an existing annotation",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"test-key": "test-value", //annotation that's stored in the map
					},
				},
			},
			annotationKey: "test-key", //this is what we are passing to the function
			verify:        assertAnnotations(nil),
		},
		{
			name: "remove an existing annotation",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"first-key":  "test-value",
						"second-key": "test-value",
						"test-key":   "",
					},
				},
			},
			annotationKey: "test-key",
			verify: assertAnnotations(map[string]string{
				"first-key":  "test-value",
				"second-key": "test-value",
			}),
		},
		{
			name: "remove an annotation that does not exist in the map",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"first-key":  "test-value",
						"second-key": "test-value",
						"third-key":  "",
					},
				},
			},
			annotationKey: "fourth-key",
			verify: assertAnnotations(map[string]string{
				"first-key":  "test-value",
				"second-key": "test-value",
				"third-key":  "",
			}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			RemoveAnnotation(testCase.obj, testCase.annotationKey)
			testCase.verify(testCase.obj, t)
		})
	}
}

func Test_hasAnnotation(t *testing.T) {
	testCases := []struct {
		name       string
		obj        metav1.Object
		annotation string
		expect     bool
	}{
		{
			name: "existing annotation found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"test-key": "value",
					},
				},
			},
			annotation: "test-key",
			expect:     true,
		},
		{
			name: "existing annotation not found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Annotations: map[string]string{
						"test-fail": "value",
					},
				},
			},
			annotation: "test-key",
			expect:     false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := HasAnnotation(testCase.obj, testCase.annotation)
			if !got == testCase.expect {
				t.Errorf("expected '%v' got '%v'", testCase.expect, got)
			}
		})
	}
}

func assertAnnotations(expectedAnnotations map[string]string) func(obj metav1.Object, t *testing.T) {
	return func(obj metav1.Object, t *testing.T) {
		annotations := obj.GetAnnotations()
		if expectedAnnotations == nil {
			if len(annotations) != 0 {
				t.Fatalf("expected 0 annotations, but got %d", len(annotations))
			}
			return
		}

		if len(annotations) != len(expectedAnnotations) {
			t.Fatalf("expected %d annotations, but got %d", len(expectedAnnotations), len(annotations))
		}

		for key, expectedValue := range expectedAnnotations {
			value, found := annotations[key]
			if !found {
				t.Errorf("expected annotation key '%s' got '%v'", key, expectedValue)
				continue
			}

			if value != expectedValue {
				t.Errorf("expected annotation value for key '%s' to be '%s', but got '%s'", key, expectedValue, value)
			}
		}
	}
}
