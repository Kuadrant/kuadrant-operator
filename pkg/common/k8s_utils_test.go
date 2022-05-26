//go:build unit
// +build unit

package common

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestObjectKeyListDifference(t *testing.T) {

	key1 := client.ObjectKey{Namespace: "ns1", Name: "obj1"}
	key2 := client.ObjectKey{Namespace: "ns2", Name: "obj2"}
	key3 := client.ObjectKey{Namespace: "ns3", Name: "obj3"}

	testCases := []struct {
		name     string
		a        []client.ObjectKey
		b        []client.ObjectKey
		expected []client.ObjectKey
	}{
		{
			"empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{},
			[]client.ObjectKey{},
		},
		{
			"a empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{key1},
			[]client.ObjectKey{},
		},
		{
			"b empty",
			[]client.ObjectKey{key1, key2},
			[]client.ObjectKey{},
			[]client.ObjectKey{key1, key2},
		},
		{
			"equal",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{},
		},
		{
			"missing key2",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key3},
			[]client.ObjectKey{key2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := ObjectKeyListDifference(tc.a, tc.b)
			if len(res) != len(tc.expected) {
				subT.Errorf("expected len (%d), got (%d)", len(tc.expected), len(res))
			}

			for idx := range res {
				if res[idx] != tc.expected[idx] {
					subT.Errorf("expected object (index %d) does not match. Expected (%s), got (%s)", idx, tc.expected[idx], res[idx])
				}
			}
		})
	}
}
