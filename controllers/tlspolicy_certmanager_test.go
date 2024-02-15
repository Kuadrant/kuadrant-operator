//go:build unit

package controllers

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Helper function tests largely based on cert manager https://github.com/cert-manager/cert-manager/blob/master/pkg/controller/certificate-shim/sync_test.go
func Test_validateGatewayListenerBlock(t *testing.T) {
	tests := []struct {
		name     string
		ingLike  metav1.Object
		listener gatewayapiv1.Listener
		wantErr  string
	}{
		{
			name: "empty TLS block",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
			},
			wantErr: "spec.listeners[0].tls: Required value: the TLS block cannot be empty",
		},
		{
			name: "empty hostname",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group("core"); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:  "example-com",
						},
					},
				},
			},
			wantErr: "spec.listeners[0].hostname: Required value: the hostname cannot be empty",
		},
		{
			name: "empty TLS CertificateRefs",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group(""); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:  "example-com",
						},
					},
				},
			},
			wantErr: "spec.listeners[0].tls.mode: Required value: the mode field is required",
		},
		{
			name: "empty TLS Mode",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode:            ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{},
				},
			},
			wantErr: "spec.listeners[0].tls.certificateRef: Required value: listener has no certificateRefs",
		},
		{
			name: "unsupported TLS Mode",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModePassthrough),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group(""); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:  "example-com",
						},
					},
				},
			},
			wantErr: "spec.listeners[0].tls.mode: Unsupported value: \"Passthrough\": supported values: \"Terminate\"",
		},
		{
			name: "empty group",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group(""); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:  "example-com",
						},
					},
				},
			},
			// no group is now supported
			wantErr: "",
		},
		{
			name: "unsupported group",
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group("invalid"); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:  "example-com-tls",
						},
					},
				},
			},
			wantErr: "spec.listeners[0].tls.certificateRef[0].group: Unsupported value: \"invalid\": supported values: \"core\", \"\"",
		},
		{
			name: "unsupported kind",
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group: func() *gatewayapiv1.Group { g := gatewayapiv1.Group("core"); return &g }(),
							Kind:  func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("SomeOtherKind"); return &k }(),
							Name:  "example-com",
						},
					},
				},
			},
			wantErr: "spec.listeners[0].tls.certificateRef[0].kind: Unsupported value: \"SomeOtherKind\": supported values: \"Secret\", \"\"",
		},
		{
			name: "cross-namespace secret ref",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "default",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group:     func() *gatewayapiv1.Group { g := gatewayapiv1.Group(""); return &g }(),
							Kind:      func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:      "example-com",
							Namespace: func() *gatewayapiv1.Namespace { n := gatewayapiv1.Namespace("another-namespace"); return &n }(),
						},
					},
				},
			},
			wantErr: "spec.listeners[0].tls.certificateRef[0].namespace: Invalid value: \"another-namespace\": cross-namespace secret references are not allowed in listeners",
		},
		{
			name: "same namespace secret ref",
			ingLike: &gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "another-namespace",
				},
			},
			listener: gatewayapiv1.Listener{
				Hostname: ptr.To(gatewayapiv1.Hostname("example.com")),
				Port:     gatewayapiv1.PortNumber(443),
				Protocol: gatewayapiv1.HTTPSProtocolType,
				TLS: &gatewayapiv1.GatewayTLSConfig{
					Mode: ptr.To(gatewayapiv1.TLSModeTerminate),
					CertificateRefs: []gatewayapiv1.SecretObjectReference{
						{
							Group:     func() *gatewayapiv1.Group { g := gatewayapiv1.Group(""); return &g }(),
							Kind:      func() *gatewayapiv1.Kind { k := gatewayapiv1.Kind("Secret"); return &k }(),
							Name:      "example-com",
							Namespace: func() *gatewayapiv1.Namespace { n := gatewayapiv1.Namespace("another-namespace"); return &n }(),
						},
					},
				},
			},
			wantErr: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotErr := validateGatewayListenerBlock(field.NewPath("spec", "listeners").Index(0), test.listener, test.ingLike).ToAggregate()
			if test.wantErr == "" {
				if gotErr != nil {
					t.Errorf("validateGatewayListenerBlock() expected no error, but got %v", gotErr)
				}
			} else {
				if gotErr == nil || !reflect.DeepEqual(gotErr.Error(), test.wantErr) {
					t.Errorf("validateGatewayListenerBlock() error = %v, wantErr %v", gotErr, test.wantErr)
				}
			}
		})
	}
}
