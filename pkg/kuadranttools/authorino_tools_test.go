package kuadranttools

import (
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"k8s.io/utils/ptr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func Test_authorinoSpecSubSet(t *testing.T) {
	type args struct {
		spec authorinov1beta1.AuthorinoSpec
	}
	tests := []struct {
		name string
		args args
		want authorinov1beta1.AuthorinoSpec
	}{
		{
			name: "Empty spec passed",
			args: args{spec: authorinov1beta1.AuthorinoSpec{}},
			want: authorinov1beta1.AuthorinoSpec{},
		},
		{
			name: "Full spec passed",
			args: args{spec: authorinov1beta1.AuthorinoSpec{
				EvaluatorCacheSize: ptr.To(9000),
				Listener:           authorinov1beta1.Listener{},
				Metrics: authorinov1beta1.Metrics{
					Port:               ptr.To(int32(9000)),
					DeepMetricsEnabled: ptr.To(true),
				},
				OIDCServer: authorinov1beta1.OIDCServer{},
				Replicas:   ptr.To(int32(3)),
				Tracing:    authorinov1beta1.Tracing{},
				Volumes:    authorinov1beta1.VolumesSpec{},
			},
			},
			want: authorinov1beta1.AuthorinoSpec{
				EvaluatorCacheSize: ptr.To(9000),
				Listener:           authorinov1beta1.Listener{},
				Metrics: authorinov1beta1.Metrics{
					Port:               ptr.To(int32(9000)),
					DeepMetricsEnabled: ptr.To(true),
				},
				OIDCServer: authorinov1beta1.OIDCServer{},
				Replicas:   ptr.To(int32(3)),
				Tracing:    authorinov1beta1.Tracing{},
				Volumes:    authorinov1beta1.VolumesSpec{},
			},
		},
		{
			name: "Partial spec passed",
			args: args{spec: authorinov1beta1.AuthorinoSpec{
				Replicas: ptr.To(int32(3)),
				Metrics: authorinov1beta1.Metrics{
					Port:               ptr.To(int32(9000)),
					DeepMetricsEnabled: ptr.To(true),
				},
			},
			},
			want: authorinov1beta1.AuthorinoSpec{
				Replicas: ptr.To(int32(3)),
				Metrics: authorinov1beta1.Metrics{
					Port:               ptr.To(int32(9000)),
					DeepMetricsEnabled: ptr.To(true),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authorinoSpecSubSet(tt.args.spec); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("authorinoSpecSubSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthorinoMutator(t *testing.T) {
	type args struct {
		existingObj client.Object
		desiredObj  client.Object
	}
	tests := []struct {
		name          string
		args          args
		want          bool
		wantErr       bool
		errorContains string
	}{
		{
			name:          "existingObj is not a authorino type",
			wantErr:       true,
			errorContains: "existingObj",
		},
		{
			name: "desiredObj is not a authorino type",
			args: args{
				existingObj: &authorinov1beta1.Authorino{},
			},
			wantErr:       true,
			errorContains: "desireObj",
		},
		{
			name: "No update required",
			args: args{
				existingObj: &authorinov1beta1.Authorino{},
				desiredObj:  &authorinov1beta1.Authorino{},
			},
			want: false,
		},
		{
			name: "Update required",
			args: args{
				existingObj: &authorinov1beta1.Authorino{
					Spec: authorinov1beta1.AuthorinoSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
				desiredObj: &authorinov1beta1.Authorino{
					Spec: authorinov1beta1.AuthorinoSpec{
						Replicas: ptr.To(int32(1)),
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AuthorinoMutator(tt.args.existingObj, tt.args.desiredObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthorinoMutator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("AuthorinoMutator() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapListenerSpec(t *testing.T) {
	type args struct {
		listener *authorinov1beta1.Listener
		spec     v1beta1.AuthorinoListener
	}
	tests := []struct {
		name string
		args args
		want authorinov1beta1.Listener
	}{
		{
			name: "Authorino Listener is nil",
			args: args{
				listener: nil,
			},
			want: authorinov1beta1.Listener{},
		},
		{
			name: "Authorino listener has deprecated port set",
			args: args{
				listener: &authorinov1beta1.Listener{Port: ptr.To(int32(2))},
				spec:     v1beta1.AuthorinoListener{Timeout: ptr.To(5000)},
			},
			want: authorinov1beta1.Listener{
				Port:    ptr.To(int32(2)),
				Timeout: ptr.To(5000),
			},
		},
		{
			name: "Past in spec copied to Authorino listener",
			args: args{
				listener: nil,
				spec: v1beta1.AuthorinoListener{
					Ports:                  &authorinov1beta1.Ports{},
					Tls:                    &authorinov1beta1.Tls{},
					Timeout:                ptr.To(5000),
					MaxHttpRequestBodySize: ptr.To(5000),
				},
			},
			want: authorinov1beta1.Listener{
				Timeout:                ptr.To(5000),
				Ports:                  authorinov1beta1.Ports{},
				Tls:                    authorinov1beta1.Tls{},
				MaxHttpRequestBodySize: ptr.To(5000),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapListenerSpec(tt.args.listener, tt.args.spec); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapListenerSpec() = %v, want %v", got, tt.want)
			}
		})
	}
}
