package controllers

import (
	"fmt"
	"sync"

	"github.com/samber/lo"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	DNSRecordKind             = "DNSRecord"
	StateDNSPolicyAcceptedKey = "DNSPolicyValid"
)

var (
	DNSRecordResource  = kuadrantdnsv1alpha1.GroupVersion.WithResource("dnsrecords")
	DNSRecordGroupKind = schema.GroupKind{Group: kuadrantdnsv1alpha1.GroupVersion.Group, Kind: "DNSRecord"}
)

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/finalizers,verbs=update

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/status,verbs=get

func NewDNSWorkflow(client *dynamic.DynamicClient, scheme *runtime.Scheme) *controller.Workflow {
	return &controller.Workflow{
		Precondition: NewDNSPoliciesValidator().Subscription().Reconcile,
		Tasks: []controller.ReconcileFunc{
			NewEffectiveDNSPoliciesReconciler(client, scheme).Subscription().Reconcile,
		},
		Postcondition: NewDNSPolicyStatusUpdater(client).Subscription().Reconcile,
	}
}

func LinkListenerToDNSRecord(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gwapiv1.Gateway])
	listeners := lo.FlatMap(lo.Map(gateways, func(g *gwapiv1.Gateway, _ int) *machinery.Gateway {
		return &machinery.Gateway{Gateway: g}
	}), machinery.ListenersFromGatewayFunc)

	return machinery.LinkFunc{
		From: machinery.ListenerGroupKind,
		To:   DNSRecordGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.FilterMap(listeners, func(l *machinery.Listener, _ int) (machinery.Object, bool) {
				if dnsRecord, ok := child.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord); ok {
					return l, l.GetNamespace() == dnsRecord.GetNamespace() &&
						dnsRecord.GetName() == dnsRecordName(l.Gateway.Name, string(l.Name))
				}
				return nil, false
			})
		},
	}
}

func LinkDNSPolicyToDNSRecord(objs controller.Store) machinery.LinkFunc {
	policies := lo.Map(objs.FilterByGroupKind(v1alpha1.DNSPolicyGroupKind), controller.ObjectAs[*v1alpha1.DNSPolicy])

	return machinery.LinkFunc{
		From: v1alpha1.DNSPolicyGroupKind,
		To:   DNSRecordGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			if dnsRecord, ok := child.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord); ok {
				return lo.FilterMap(policies, func(dnsPolicy *v1alpha1.DNSPolicy, _ int) (machinery.Object, bool) {
					return dnsPolicy, utils.IsOwnedBy(dnsRecord, dnsPolicy)
				})
			}
			return nil
		},
	}
}

func dnsPolicyAcceptedStatusFunc(state *sync.Map) func(policy machinery.Policy) (bool, error) {
	validatedPolicies, validated := state.Load(StateDNSPolicyAcceptedKey)
	if !validated {
		return dnsPolicyAcceptedStatus
	}
	validatedPoliciesMap := validatedPolicies.(map[string]error)
	return func(policy machinery.Policy) (bool, error) {
		err, pValidated := validatedPoliciesMap[policy.GetLocator()]
		if pValidated {
			return err == nil, err
		}
		return dnsPolicyAcceptedStatus(policy)
	}
}

func dnsPolicyAcceptedStatus(policy machinery.Policy) (accepted bool, err error) {
	p, ok := policy.(*v1alpha1.DNSPolicy)
	if !ok {
		return
	}
	if condition := meta.FindStatusCondition(p.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted)); condition != nil {
		accepted = condition.Status == metav1.ConditionTrue
		if !accepted {
			err = fmt.Errorf(condition.Message)
		}
		return
	}
	return
}
