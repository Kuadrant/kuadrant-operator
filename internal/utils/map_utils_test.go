package utils

import "testing"

func TestMergeMapStringString(t *testing.T) {
	m := make(map[string]string)
	var nilMap map[string]string

	type args struct {
		existing *map[string]string
		desired  map[string]string
	}
	tests := []struct {
		name       string
		args       args
		wantUpdate bool
	}{
		{
			name: "nil pointer to map",
			args: args{
				existing: nil,
				desired: map[string]string{
					"foo": "bar",
					"qux": "quux",
				},
			},
			wantUpdate: false,
		},
		{
			name: "nil map",
			args: args{
				existing: &nilMap,
				desired: map[string]string{
					"foo": "bar",
					"qux": "quux",
				},
			},
			wantUpdate: true,
		},
		{
			name: "empty map",
			args: args{
				existing: &m,
				desired: map[string]string{
					"foo": "bar",
					"qux": "quux",
				},
			},
			wantUpdate: true,
		},
		{
			name: "desired keys not in existing",
			args: args{
				existing: &map[string]string{
					"foo": "bar",
				},
				desired: map[string]string{
					"qux": "quux",
				},
			},
			wantUpdate: true,
		},
		{
			name: "same maps",
			args: args{
				existing: &map[string]string{
					"foo": "bar",
					"qux": "quux",
				},
				desired: map[string]string{
					"foo": "bar",
					"qux": "quux",
				},
			},
			wantUpdate: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(subT *testing.T) {
			got := MergeMapStringString(tt.args.existing, tt.args.desired)
			if got != tt.wantUpdate {
				subT.Errorf("MergeMapStringString() got = %v, wantUpdate %v", got, tt.wantUpdate)
			}

			if tt.args.existing == nil {
				return
			}

			for desiredKey, desiredValue := range tt.args.desired {
				existingVal, ok := (*tt.args.existing)[desiredKey]
				if !ok || existingVal != desiredValue {
					t.Errorf("MergeMapStringString() the key does not match:  %v", desiredKey)
				}
			}
		})
	}
}
