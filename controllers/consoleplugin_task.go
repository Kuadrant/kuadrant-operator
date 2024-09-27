package controllers

import (
	"context"
	"sync"

	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"github.com/kuadrant/kuadrant-operator/pkg/openshift"
)

type ConsolePluginTask struct {
	*reconcilers.BaseReconciler
}

func NewConsolePluginTaskTask(mgr ctrlruntime.Manager) *ConsolePluginTask {
	return &ConsolePluginTask{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
			log.Log.WithName("consoleplugin"),
			mgr.GetEventRecorderFor("ConsolePlugin"),
		),
	}
}

func (r *ConsolePluginTask) Events() []controller.ResourceEventMatcher {
	return []controller.ResourceEventMatcher{
		{Kind: ptr.To(openshift.ConsolePluginGVK.GroupKind())},
	}
}

func (r *ConsolePluginTask) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	return nil
}
