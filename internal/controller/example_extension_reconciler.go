package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
	policymachinery "github.com/kuadrant/policy-machinery/controller"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ExampleExtensionReconciler struct {
}

func (e *ExampleExtensionReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	logger := utils.LoggerFromContext(ctx).WithName("ExampleExtensionReconciler")
	logger.Info("Reconciling ExampleExtension")
	dynamicClient, err := utils.DynamicClientFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	unstructuredPolicy, err := dynamicClient.Resource(kuadrantv1.AuthPoliciesResource).Namespace(request.Namespace).Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(nil, "Could not find my policy")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get my policy")
		return reconcile.Result{}, err
	}

	result, err := policymachinery.Restructure[kuadrantv1.AuthPolicy](unstructuredPolicy)
	if err != nil {
		logger.Error(err, "Failed to restructure OIDCPolicy")
		return reconcile.Result{}, err
	}

	//map it to something that implements the interface
	myPolicy := newExamplePolicy(result.(kuadrantv1.AuthPolicy))

	out, err := kuadrantCtx.Resolve(ctx, myPolicy, "self.findGateways()[0].metadata.name", true)
	if err != nil {
		logger.Error(err, "Failed to resolve")
		return reconcile.Result{}, err
	}
	logger.Info("Resolved", "out", out)

	return reconcile.Result{}, nil
}

type ExamplePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ExamplePolicySpec `json:"spec,omitempty"`
}

func (e *ExamplePolicy) DeepCopyObject() runtime.Object {
	//TODO implement me
	panic("implement me")
}

type ExamplePolicySpec struct {
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
}

func (e *ExamplePolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		e.Spec.TargetRef,
	}
}

func newExamplePolicy(authPolicy kuadrantv1.AuthPolicy) *ExamplePolicy {
	return &ExamplePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authPolicy.Name,
			Namespace: authPolicy.Namespace,
		},
		Spec: ExamplePolicySpec{
			TargetRef: authPolicy.Spec.TargetRef,
		},
	}
}
