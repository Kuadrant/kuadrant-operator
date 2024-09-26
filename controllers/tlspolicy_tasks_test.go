//go:build unit

package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestTLSPolicyValidKey(t *testing.T) {
	type args struct {
		uid types.UID
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test uid is appended",
			args: args{
				types.UID("unqiueid"),
			},
			want: "TLSPolicyValid:unqiueid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TLSPolicyValidKey(tt.args.uid); got != tt.want {
				t.Errorf("TLSPolicyValidKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
