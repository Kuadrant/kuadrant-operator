//go:build unit

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("Rate limit type safety", func() {

	Describe("LimitNamespace", func() {
		Context("when converting to ActionScope", func() {
			It("should return an ActionScope with the same underlying value", func() {
				ns := LimitNamespace("default/my-route")
				Expect(string(ActionScope(ns))).To(Equal("default/my-route"))
			})

			It("should be assignable to LimitNamespace type", func() {
				ns := LimitNamespace("mynamespace/myroute")
				Expect(ns).To(BeAssignableToTypeOf(LimitNamespace("")))
			})
		})
	})

	Describe("ActionScope", func() {
		Context("when created from a LimitNamespace", func() {
			It("should be assignable to ActionScope type", func() {
				scope := ActionScope(LimitNamespace("default/my-route"))
				Expect(scope).To(BeAssignableToTypeOf(ActionScope("")))
			})
		})
	})

	Describe("LimitsNamespaceFromRoute", func() {
		var (
			route *gatewayv1.HTTPRoute
		)

		BeforeEach(func() {
			route = &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-route",
					Namespace: "default",
				},
			}
		})

		Context("when given a valid HTTPRoute", func() {
			It("should return a LimitNamespace in namespace/name format", func() {
				ns := LimitsNamespaceFromRoute(route)
				Expect(string(ns)).To(Equal("default/my-route"))
			})

			It("should return a LimitNamespace type", func() {
				ns := LimitsNamespaceFromRoute(route)
				Expect(ns).To(BeAssignableToTypeOf(LimitNamespace("")))
			})
		})

		Context("when chaining directly to ActionScope", func() {
			It("should produce a valid ActionScope in namespace/name format", func() {
				scope := ActionScope(LimitsNamespaceFromRoute(route))
				Expect(string(scope)).To(Equal("default/my-route"))
				Expect(scope).To(BeAssignableToTypeOf(ActionScope("")))
			})
		})
	})
})
