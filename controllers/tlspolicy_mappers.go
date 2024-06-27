package controllers

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func mapIssuerToPolicy(ctx context.Context, k8sClient client.Client, logger logr.Logger, object client.Object) []reconcile.Request {
	_, ok := object.(*certmanagerv1.Issuer)
	if !ok {
		logger.V(1).Info("cannot map Issuer related event to tls policy", "error", fmt.Sprintf("%T is not a *certmanagerv1.Issuer", object))
		return nil
	}

	policies := &v1alpha1.TLSPolicyList{}
	if err := k8sClient.List(ctx, policies, client.InNamespace(object.GetNamespace())); err != nil {
		logger.V(1).Error(err, "cannot list policies", "namespace", object.GetNamespace())
		return nil
	}

	return policiesToRequests(logger, policies, object, certmanagerv1.IssuerKind)
}

func mapClusterIssuerToPolicy(ctx context.Context, k8sClient client.Client, logger logr.Logger, object client.Object) []reconcile.Request {
	_, ok := object.(*certmanagerv1.ClusterIssuer)
	if !ok {
		logger.V(1).Info("cannot map ClusterIssuer related event to tls policy", "error", fmt.Sprintf("%T is not a *certmanagerv1.ClusterIssuer", object))
		return nil
	}

	policies := &v1alpha1.TLSPolicyList{}
	if err := k8sClient.List(ctx, policies); err != nil {
		logger.V(1).Error(err, "cannot list policies for all namespaces")
		return nil
	}

	return policiesToRequests(logger, policies, object, certmanagerv1.ClusterIssuerKind)
}

func policiesToRequests(logger logr.Logger, policies *v1alpha1.TLSPolicyList, object client.Object, issuerKind string) []reconcile.Request {
	filteredPolicies := utils.Filter(policies.Items, func(policy v1alpha1.TLSPolicy) bool {
		return policy.Spec.IssuerRef.Name == object.GetName() &&
			policy.Spec.IssuerRef.Kind == issuerKind &&
			policy.Spec.IssuerRef.Group == certmanagerv1.SchemeGroupVersion.Group
	})

	return utils.Map(filteredPolicies, func(p v1alpha1.TLSPolicy) reconcile.Request {
		logger.V(1).Info("tls policy possibly affected by related event", "eventKind", issuerKind, "policyName", p.Name, "policyNamespace", p.Namespace)
		return reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&p)}
	})
}
