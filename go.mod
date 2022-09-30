module github.com/kuadrant/kuadrant-operator

go 1.16

require (
	github.com/go-logr/logr v1.2.3
	github.com/google/go-cmp v0.5.9
	github.com/kuadrant/authorino v0.10.0
	github.com/kuadrant/authorino-operator v0.2.0
	github.com/kuadrant/limitador-operator v0.3.1-0.20220830090346-4f6d5794272b
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.20.2
	go.uber.org/zap v1.22.0
	google.golang.org/protobuf v1.28.1
	gotest.tools v2.2.0+incompatible
	istio.io/api v0.0.0-20220929135006-93870da8d382
	istio.io/client-go v1.12.4-0.20220304040955-30b642d5ba34 // indirect
	istio.io/istio v0.0.0-20220929144806-42b01b1beb7d
	k8s.io/api v0.25.1
	k8s.io/apiextensions-apiserver v0.25.0
	k8s.io/apimachinery v0.25.1
	k8s.io/client-go v0.25.1
	k8s.io/klog/v2 v2.80.1
	sigs.k8s.io/controller-runtime v0.13.0
	sigs.k8s.io/gateway-api v0.5.1-0.20220830123301-a7a465ababc8
)
