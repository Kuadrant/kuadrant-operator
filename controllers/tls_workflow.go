package controllers

import (
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/client-go/dynamic"
)

func NewTLSWorkflow(client *dynamic.DynamicClient) *controller.Workflow {
	return &controller.Workflow{
		Precondition:  NewValidateTLSPolicyTask().Reconcile,
		Postcondition: NewTLSPolicyStatusTask(client).Reconcile,
	}
}
