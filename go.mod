module github.com/kuadrant/kuadrant-controller

go 1.16

require (
	github.com/Azure/go-autorest/autorest v0.11.19 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/gogo/protobuf v1.3.2
	github.com/google/go-cmp v0.5.6
	github.com/kuadrant/limitador-operator v0.2.0
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.15.0
	go.uber.org/zap v1.19.0
	gotest.tools v2.2.0+incompatible
	istio.io/api v0.0.0-20210219142745-68975986cccb
	istio.io/client-go v1.9.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/klog/v2 v2.10.0
	sigs.k8s.io/controller-runtime v0.10.0
	sigs.k8s.io/gateway-api v0.4.1
)
