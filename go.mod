module github.com/kuadrant/kuadrant-operator

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.6
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.16.0
	go.uber.org/zap v1.19.1
	gotest.tools v2.2.0+incompatible
	// go get istio.io/api@1.12.6
	istio.io/api v0.0.0-20220304035241-8c47cbbea144
	// go get istio.io/istio/operator/pkg/apis/istio/v1alpha1@1.12.6
	istio.io/istio v0.0.0-20220328194112-a0c7a3355331
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.10.0
	sigs.k8s.io/controller-runtime v0.10.2
)
