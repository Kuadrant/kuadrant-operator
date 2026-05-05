//go:build unit

package openshift

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = configv1.Install(s)
	return s
}

func TestResolveImageURL(t *testing.T) {
	tests := []struct {
		name            string
		imageURL        string
		idmsObjects     []runtime.Object
		itmsObjects     []runtime.Object
		icpObjects      []runtime.Object
		isIDMS          bool
		isITMS          bool
		isICP           bool
		expectedResult  string
	}{
		{
			name:           "no CRDs installed returns original URL",
			imageURL:       "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			expectedResult: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS resolves digest reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "mirror.internal.com/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS does not resolve tag reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "registry.redhat.io/kuadrant/wasm-shim:latest",
		},
		{
			name:     "ITMS resolves tag reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			isITMS:   true,
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"tag-mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "tag-mirror.internal.com/kuadrant/wasm-shim:latest",
		},
		{
			name:     "ITMS does not resolve digest reference",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isITMS:   true,
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"tag-mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "ICP resolves digest reference (legacy ICSP equivalent)",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isICP:    true,
			icpObjects: []runtime.Object{
				&configv1.ImageContentPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageContentPolicy", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-icp"},
					Spec: configv1.ImageContentPolicySpec{
						RepositoryDigestMirrors: []configv1.RepositoryDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.Mirror{"icp-mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "icp-mirror.internal.com/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS and ITMS coexist - digest ref picks IDMS mirror",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:   true,
			isITMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"idms-mirror.internal.com"},
							},
						},
					},
				},
			},
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"itms-mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "idms-mirror.internal.com/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "IDMS and ITMS coexist - tag ref picks ITMS mirror",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim:latest",
			isIDMS:   true,
			isITMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"idms-mirror.internal.com"},
							},
						},
					},
				},
			},
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"itms-mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "itms-mirror.internal.com/kuadrant/wasm-shim:latest",
		},
		{
			name:     "most specific IDMS source wins across multiple IDMS objects",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "broad-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"broad-mirror.internal.com"},
							},
						},
					},
				},
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "specific-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io/kuadrant",
								Mirrors: []configv1.ImageMirror{"specific-mirror.internal.com/kuadrant"},
							},
						},
					},
				},
			},
			expectedResult: "specific-mirror.internal.com/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "no matching IDMS source returns original URL",
			imageURL: "quay.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"mirror.internal.com"},
							},
						},
					},
				},
			},
			expectedResult: "quay.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "empty IDMS mirrors list returns original URL",
			imageURL: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "empty-mirrors"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{},
							},
						},
					},
				},
			},
			expectedResult: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:           "IDMS CRD installed but no objects exist returns original URL",
			imageURL:       "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
			isIDMS:         true,
			idmsObjects:    []runtime.Object{},
			expectedResult: "registry.redhat.io/kuadrant/wasm-shim@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScheme()

			var allObjects []runtime.Object
			allObjects = append(allObjects, tt.idmsObjects...)
			allObjects = append(allObjects, tt.itmsObjects...)
			allObjects = append(allObjects, tt.icpObjects...)

			fakeClient := dfake.NewSimpleDynamicClient(s, allObjects...)

			result := ResolveImageURL(
				context.Background(),
				fakeClient,
				tt.imageURL,
				tt.isIDMS, tt.isITMS, tt.isICP,
				logr.Discard(),
			)

			if result != tt.expectedResult {
				t.Errorf("expected %q, got %q", tt.expectedResult, result)
			}
		})
	}
}
