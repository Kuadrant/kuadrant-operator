//go:build unit

package metadata

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_hasLabel(t *testing.T) {
	testCases := []struct {
		name   string
		obj    metav1.Object
		label  string
		expect bool
	}{
		{
			name: "existing label found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-key": "value",
					},
				},
			},
			label:  "test-key",
			expect: true,
		},
		{
			name: "existing label not found",
			obj: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-fail": "value",
					},
				},
			},
			label:  "test-key",
			expect: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := HasLabel(testCase.obj, testCase.label)
			if !got == testCase.expect {
				t.Errorf("expected '%v' got '%v'", testCase.expect, got)
			}
		})
	}
}
