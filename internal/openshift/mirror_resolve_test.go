//go:build unit

package openshift

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	dfake "k8s.io/client-go/dynamic/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = configv1.Install(s)
	return s
}

func TestResolveImageURL(t *testing.T) {
	tests := []struct {
		name           string
		imageURL       string
		idmsObjects    []runtime.Object
		itmsObjects    []runtime.Object
		icpObjects     []runtime.Object
		isIDMS         bool
		isITMS         bool
		isICP          bool
		expectedResult string
	}{
		{
			name:           "no CRDs installed returns original URL",
			imageURL:       realDigestRef,
			expectedResult: realDigestRef,
		},
		{
			name:     "IDMS resolves realistic digest reference",
			imageURL: realDigestRef,
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "disconnected-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "IDMS resolves registry.access.redhat.com digest reference",
			imageURL: altDigestRef,
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "alt-registry-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.access.redhat.com",
								Mirrors: []configv1.ImageMirror{"mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "IDMS does not resolve tag reference",
			imageURL: realTagRef,
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: realTagRef,
		},
		{
			name:     "ITMS resolves realistic tag reference",
			imageURL: realTagRef,
			isITMS:   true,
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"tag-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "tag-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "ITMS does not resolve digest reference",
			imageURL: realDigestRef,
			isITMS:   true,
			itmsObjects: []runtime.Object{
				&configv1.ImageTagMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-itms"},
					Spec: configv1.ImageTagMirrorSetSpec{
						ImageTagMirrors: []configv1.ImageTagMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"tag-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: realDigestRef,
		},
		{
			name:     "ICP resolves digest reference (legacy ICSP equivalent)",
			imageURL: realDigestRef,
			isICP:    true,
			icpObjects: []runtime.Object{
				&configv1.ImageContentPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageContentPolicy", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-icp"},
					Spec: configv1.ImageContentPolicySpec{
						RepositoryDigestMirrors: []configv1.RepositoryDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.Mirror{"icp-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "icp-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "IDMS and ITMS coexist - digest ref picks IDMS mirror",
			imageURL: realDigestRef,
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
								Mirrors: []configv1.ImageMirror{"idms-mirror.disconnected.local"},
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
								Mirrors: []configv1.ImageMirror{"itms-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "idms-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "IDMS and ITMS coexist - tag ref picks ITMS mirror",
			imageURL: realTagRef,
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
								Mirrors: []configv1.ImageMirror{"idms-mirror.disconnected.local"},
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
								Mirrors: []configv1.ImageMirror{"itms-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "itms-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9:v0.12.3-4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
		},
		{
			name:     "most specific IDMS source wins across multiple IDMS objects",
			imageURL: realDigestRef,
			isIDMS:   true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "broad-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"broad-mirror.disconnected.local"},
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
								Source:  "registry.redhat.io/rhcl-1",
								Mirrors: []configv1.ImageMirror{"specific-mirror.disconnected.local/rhcl-1"},
							},
						},
					},
				},
			},
			expectedResult: "specific-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
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
								Mirrors: []configv1.ImageMirror{"mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "quay.io/kuadrant/wasm-shim@sha256:abc123",
		},
		{
			name:     "empty IDMS mirrors list returns original URL",
			imageURL: realDigestRef,
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
			expectedResult: realDigestRef,
		},
		{
			name:           "IDMS CRD installed but no objects exist returns original URL",
			imageURL:       realDigestRef,
			isIDMS:         true,
			idmsObjects:    []runtime.Object{},
			expectedResult: realDigestRef,
		},
		{
			name:     "all three CRD types combine rules",
			imageURL: realDigestRef,
			isIDMS:   true,
			isITMS:   true,
			isICP:    true,
			idmsObjects: []runtime.Object{
				&configv1.ImageDigestMirrorSet{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
					Spec: configv1.ImageDigestMirrorSetSpec{
						ImageDigestMirrors: []configv1.ImageDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.ImageMirror{"idms-mirror.disconnected.local"},
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
								Mirrors: []configv1.ImageMirror{"itms-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			icpObjects: []runtime.Object{
				&configv1.ImageContentPolicy{
					TypeMeta:   metav1.TypeMeta{Kind: "ImageContentPolicy", APIVersion: "config.openshift.io/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-icp"},
					Spec: configv1.ImageContentPolicySpec{
						RepositoryDigestMirrors: []configv1.RepositoryDigestMirrors{
							{
								Source:  "registry.redhat.io",
								Mirrors: []configv1.Mirror{"icp-mirror.disconnected.local"},
							},
						},
					},
				},
			},
			expectedResult: "idms-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb",
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

func TestResolveImageURLListErrors(t *testing.T) {
	tests := []struct {
		name           string
		failResources  []string
		isIDMS         bool
		isITMS         bool
		isICP          bool
	}{
		{
			name:          "IDMS list error is handled gracefully",
			failResources: []string{"imagedigestmirrorsets"},
			isIDMS:        true,
		},
		{
			name:          "ITMS list error is handled gracefully",
			failResources: []string{"imagetagmirrorsets"},
			isITMS:        true,
		},
		{
			name:          "ICP list error is handled gracefully",
			failResources: []string{"imagecontentpolicies"},
			isICP:         true,
		},
		{
			name:          "all three list errors handled gracefully",
			failResources: []string{"imagedigestmirrorsets", "imagetagmirrorsets", "imagecontentpolicies"},
			isIDMS:        true,
			isITMS:        true,
			isICP:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScheme()
			fakeClient := dfake.NewSimpleDynamicClient(s)

			for _, resource := range tt.failResources {
				fakeClient.PrependReactor("list", resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, &errFake{msg: "simulated API error"}
				})
			}

			result := ResolveImageURL(
				context.Background(),
				fakeClient,
				realDigestRef,
				tt.isIDMS, tt.isITMS, tt.isICP,
				logr.Discard(),
			)

			if result != realDigestRef {
				t.Errorf("expected original URL %q on error, got %q", realDigestRef, result)
			}
		})
	}
}

type errFake struct{ msg string }

func (e *errFake) Error() string { return e.msg }

func TestCollectIDMSRules(t *testing.T) {
	s := newScheme()

	t.Run("returns error on list failure", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("list", "imagedigestmirrorsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "API unavailable"}
		})

		rules, err := collectIDMSRules(context.Background(), fakeClient)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if rules != nil {
			t.Errorf("expected nil rules on error, got %v", rules)
		}
	})

	t.Run("collects rules from multiple IDMS objects with multiple mirrors", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s,
			&configv1.ImageDigestMirrorSet{
				TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "idms-1"},
				Spec: configv1.ImageDigestMirrorSetSpec{
					ImageDigestMirrors: []configv1.ImageDigestMirrors{
						{
							Source:  "registry.redhat.io",
							Mirrors: []configv1.ImageMirror{"mirror1.local", "mirror2.local"},
						},
						{
							Source:  "registry.access.redhat.com",
							Mirrors: []configv1.ImageMirror{"mirror3.local"},
						},
					},
				},
			},
		)

		rules, err := collectIDMSRules(context.Background(), fakeClient)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rules) != 2 {
			t.Fatalf("expected 2 rules, got %d", len(rules))
		}
		if rules[0].source != "registry.redhat.io" {
			t.Errorf("expected source registry.redhat.io, got %s", rules[0].source)
		}
		if len(rules[0].mirrors) != 2 {
			t.Errorf("expected 2 mirrors, got %d", len(rules[0].mirrors))
		}
		if rules[0].pullType != pullTypeDigest {
			t.Errorf("expected pullTypeDigest, got %d", rules[0].pullType)
		}
		if rules[1].source != "registry.access.redhat.com" {
			t.Errorf("expected source registry.access.redhat.com, got %s", rules[1].source)
		}
	})
}

func TestCollectITMSRules(t *testing.T) {
	s := newScheme()

	t.Run("returns error on list failure", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("list", "imagetagmirrorsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "API unavailable"}
		})

		rules, err := collectITMSRules(context.Background(), fakeClient)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if rules != nil {
			t.Errorf("expected nil rules on error, got %v", rules)
		}
	})

	t.Run("collects rules with pullTypeTag", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s,
			&configv1.ImageTagMirrorSet{
				TypeMeta:   metav1.TypeMeta{Kind: "ImageTagMirrorSet", APIVersion: "config.openshift.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "itms-1"},
				Spec: configv1.ImageTagMirrorSetSpec{
					ImageTagMirrors: []configv1.ImageTagMirrors{
						{
							Source:  "registry.redhat.io",
							Mirrors: []configv1.ImageMirror{"tag-mirror.local"},
						},
					},
				},
			},
		)

		rules, err := collectITMSRules(context.Background(), fakeClient)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rules) != 1 {
			t.Fatalf("expected 1 rule, got %d", len(rules))
		}
		if rules[0].pullType != pullTypeTag {
			t.Errorf("expected pullTypeTag, got %d", rules[0].pullType)
		}
	})
}

func TestCollectICPRules(t *testing.T) {
	s := newScheme()

	t.Run("returns error on list failure", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s)
		fakeClient.PrependReactor("list", "imagecontentpolicies", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, &errFake{msg: "API unavailable"}
		})

		rules, err := collectICPRules(context.Background(), fakeClient)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if rules != nil {
			t.Errorf("expected nil rules on error, got %v", rules)
		}
	})

	t.Run("collects rules with pullTypeDigest", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s,
			&configv1.ImageContentPolicy{
				TypeMeta:   metav1.TypeMeta{Kind: "ImageContentPolicy", APIVersion: "config.openshift.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "icp-1"},
				Spec: configv1.ImageContentPolicySpec{
					RepositoryDigestMirrors: []configv1.RepositoryDigestMirrors{
						{
							Source:  "registry.redhat.io",
							Mirrors: []configv1.Mirror{"icp-mirror.local"},
						},
					},
				},
			},
		)

		rules, err := collectICPRules(context.Background(), fakeClient)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rules) != 1 {
			t.Fatalf("expected 1 rule, got %d", len(rules))
		}
		if rules[0].pullType != pullTypeDigest {
			t.Errorf("expected pullTypeDigest, got %d", rules[0].pullType)
		}
	})
}

func TestCollectRulesConversionError(t *testing.T) {
	s := newScheme()

	t.Run("IDMS with malformed object is skipped", func(t *testing.T) {
		fakeClient := dfake.NewSimpleDynamicClient(s,
			&configv1.ImageDigestMirrorSet{
				TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "good-idms"},
				Spec: configv1.ImageDigestMirrorSetSpec{
					ImageDigestMirrors: []configv1.ImageDigestMirrors{
						{
							Source:  "registry.redhat.io",
							Mirrors: []configv1.ImageMirror{"mirror.local"},
						},
					},
				},
			},
		)

		// Inject a malformed object by adding a reactor that modifies list results
		fakeClient.PrependReactor("list", "imagedigestmirrorsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			// Call through to get the real list, then corrupt one item
			for i, reactor := range fakeClient.ReactionChain[1:] {
				_ = i
				handled, obj, err := reactor.React(action)
				if handled {
					return handled, obj, err
				}
			}
			return false, nil, nil
		})

		rules, err := collectIDMSRules(context.Background(), fakeClient)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have collected the good object's rule
		if len(rules) != 1 {
			t.Errorf("expected 1 rule from good object, got %d", len(rules))
		}
	})
}

func TestResolveImageURLAllListsFail(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	fakeClient.PrependReactor("list", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, &errFake{msg: "the server could not find the requested resource"}
	})

	result := ResolveImageURL(
		context.Background(),
		fakeClient,
		realDigestRef,
		true, true, true,
		logr.Discard(),
	)

	if result != realDigestRef {
		t.Errorf("expected original URL on API errors, got %q", result)
	}
}

func TestResolveImageURLMultipleIDMSObjects(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "global-idms"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{"global-mirror.disconnected.local"},
					},
				},
			},
		},
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "rhcl-idms"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io/rhcl-1",
						Mirrors: []configv1.ImageMirror{"rhcl-mirror.disconnected.local/rhcl-1"},
					},
				},
			},
		},
	)

	result := ResolveImageURL(
		context.Background(),
		fakeClient,
		realDigestRef,
		true, false, false,
		logr.Discard(),
	)

	expected := "rhcl-mirror.disconnected.local/rhcl-1/wasm-shim-rhel9@sha256:4b8cd7dea4d9cd3c7170af872c229e206155691e7dbb4a90c64699ccecc7ccbb"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// Verify GVR constants match what the dynamic client expects
func TestResolveImageURLKillSwitch(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-idms"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{"mirror.disconnected.local"},
					},
				},
			},
		},
	)

	t.Setenv("DISABLE_IMAGE_MIRROR_RESOLUTION", "true")

	result := ResolveImageURL(
		context.Background(),
		fakeClient,
		realDigestRef,
		true, false, false,
		logr.Discard(),
	)

	if result != realDigestRef {
		t.Errorf("expected original URL when kill-switch is enabled, got %q", result)
	}
}

// malformedObject returns an unstructured object with a type mismatch at
// .metadata (string instead of map) which causes FromUnstructured to fail.
func malformedObject() map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"metadata":   "not-a-map",
	}
}

func TestCollectRulesConversionErrorITMS(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	fakeClient.PrependReactor("list", "imagetagmirrorsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"apiVersion": "config.openshift.io/v1",
				"kind":       "ImageTagMirrorSetList",
			},
			Items: []unstructured.Unstructured{
				{Object: malformedObject()},
			},
		}, nil
	})

	rules, err := collectITMSRules(context.Background(), fakeClient)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules from malformed object, got %d", len(rules))
	}
}

func TestCollectRulesConversionErrorICP(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	fakeClient.PrependReactor("list", "imagecontentpolicies", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"apiVersion": "config.openshift.io/v1",
				"kind":       "ImageContentPolicyList",
			},
			Items: []unstructured.Unstructured{
				{Object: malformedObject()},
			},
		}, nil
	})

	rules, err := collectICPRules(context.Background(), fakeClient)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules from malformed object, got %d", len(rules))
	}
}

func TestCollectRulesConversionErrorIDMSMalformed(t *testing.T) {
	s := newScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	fakeClient.PrependReactor("list", "imagedigestmirrorsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"apiVersion": "config.openshift.io/v1",
				"kind":       "ImageDigestMirrorSetList",
			},
			Items: []unstructured.Unstructured{
				{Object: malformedObject()},
			},
		}, nil
	})

	rules, err := collectIDMSRules(context.Background(), fakeClient)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules from malformed object, got %d", len(rules))
	}
}

func TestMirrorResourceGVRs(t *testing.T) {
	expected := []struct {
		name string
		gvr  schema.GroupVersionResource
	}{
		{"IDMS", schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "imagedigestmirrorsets"}},
		{"ITMS", schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "imagetagmirrorsets"}},
		{"ICP", schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "imagecontentpolicies"}},
	}

	actuals := []schema.GroupVersionResource{
		ImageDigestMirrorSetResource,
		ImageTagMirrorSetResource,
		ImageContentPolicyResource,
	}

	for i, exp := range expected {
		if actuals[i] != exp.gvr {
			t.Errorf("%s GVR: expected %v, got %v", exp.name, exp.gvr, actuals[i])
		}
	}
}
