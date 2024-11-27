package controllers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

func NewDNSPolicyStatusUpdater(client *dynamic.DynamicClient) *DNSPolicyStatusUpdater {
	return &DNSPolicyStatusUpdater{client: client}
}

type DNSPolicyStatusUpdater struct {
	client *dynamic.DynamicClient
}

func (r *DNSPolicyStatusUpdater) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.updateStatus,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
			{Kind: &DNSRecordGroupKind},
		},
	}
}

func (r *DNSPolicyStatusUpdater) updateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("DNSPolicyStatusUpdater").WithValues("context", ctx)
	tracer := controller.TracerFromContext(ctx)

	policyTypeFilterFunc := dnsPolicyTypeFilterFunc()
	policyAcceptedFunc := dnsPolicyAcceptedStatusFunc(state)
	policyErrorFunc := dnsPolicyErrorFunc(state)

	policies := lo.FilterMap(topology.Policies().Items(), policyTypeFilterFunc)

	logger.V(1).Info("updating dns policy statuses", "policies", len(policies))

	for _, policy := range policies {
		policyCtx, span := tracer.Start(ctx, "policy.DNSPolicy")
		span.SetAttributes(
			attribute.String("policy.name", policy.GetName()),
			attribute.String("policy.namespace", policy.GetNamespace()),
			attribute.String("policy.kind", kuadrantv1.DNSPolicyGroupKind.Kind),
			attribute.String("policy.uid", string(policy.GetUID())),
		)

		pLogger := logger.WithValues("policy", policy.GetLocator())

		pLogger.V(1).Info("updating dns policy status")

		if policy.GetDeletionTimestamp() != nil {
			pLogger.V(1).Info("policy marked for deletion, skipping")
			span.AddEvent("policy marked for deletion, skipping")
			span.SetStatus(codes.Ok, "")
			span.End()
			continue
		}

		// copy initial conditions, otherwise status will always be updated
		newStatus := &kuadrantv1.DNSPolicyStatus{
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		accepted, err := policyAcceptedFunc(policy)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(policy, err))

		// do not set enforced condition if Accepted condition is false
		if !accepted {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			policyRecords := lo.FilterMap(topology.Objects().Children(policy), func(item machinery.Object, _ int) (*kuadrantdnsv1alpha1.DNSRecord, bool) {
				if rObj, isObj := item.(*controller.RuntimeObject); isObj {
					if record, isRec := rObj.Object.(*kuadrantdnsv1alpha1.DNSRecord); isRec {
						return record, true
					}
				}
				return nil, false
			})

			enforcedCond := enforcedCondition(policyRecords, policy)
			if pErr := policyErrorFunc(policy); pErr != nil {
				pLogger.V(1).Info("adding contextual error to policy enforced status", "err", pErr)
				enforcedCond.Message = fmt.Sprintf("%s : %s", enforcedCond.Message, pErr.Error())
			}
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

			if policy.Spec.HealthCheck != nil {
				healthyCond := healthyCondition(policyRecords, policy)
				meta.SetStatusCondition(&newStatus.Conditions, *healthyCond)
			} else {
				meta.RemoveStatusCondition(&newStatus.Conditions, string(PolicyConditionSubResourcesHealthy))
			}

			propagateRecordConditions(policyRecords, newStatus)

			if len(policyRecords) > math.MaxInt32 {
				pLogger.Error(fmt.Errorf("too many records: %d exceeds int32 limits", len(policyRecords)), "error setting total dns total records")
				newStatus.TotalRecords = math.MaxInt32
			} else {
				newStatus.TotalRecords = int32(len(policyRecords)) // #nosec G115 - false positive - operation is safe now with the check
			}
		}

		equalStatus := equality.Semantic.DeepEqual(newStatus, policy.Status)
		if equalStatus && policy.Generation == policy.Status.ObservedGeneration {
			pLogger.V(1).Info("policy status unchanged, skipping update")
			span.AddEvent("policy status unchanged, skipping update")
			span.SetStatus(codes.Ok, "")
			span.End()
			continue
		}
		newStatus.ObservedGeneration = policy.Generation
		policy.Status = *newStatus

		// TODO: Managed field cannot be set when applying
		policy.ManagedFields = nil
		obj, err := controller.Destruct(policy)
		if err != nil {
			pLogger.Error(err, "unable to destruct policy") // should never happen
			span.RecordError(err)
			span.SetStatus(codes.Error, "unable to destruct policy")
			span.End()
			continue
		}

		_, err = r.client.Resource(kuadrantv1.DNSPoliciesResource).Namespace(policy.GetNamespace()).ApplyStatus(policyCtx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: FieldManagerName})
		if err != nil {
			pLogger.Error(err, "unable to update status for policy")
			span.RecordError(err)
			span.SetStatus(codes.Error, "unable to update status")
			span.End()
			continue
		}

		emitConditionMetrics(policy)

		span.AddEvent("policy status updated successfully")
		span.SetStatus(codes.Ok, "")
		span.End()
	}

	return nil
}

func enforcedCondition(records []*kuadrantdnsv1alpha1.DNSRecord, dnsPolicy *kuadrantv1.DNSPolicy) *metav1.Condition {
	// there are no controlled DNS records present
	if len(records) == 0 {
		cond := kuadrant.EnforcedCondition(dnsPolicy, nil, true)
		cond.Message = "DNSPolicy has been successfully enforced : no DNSRecords created based on policy and gateway configuration"
		return cond
	}

	// filter not ready records
	notReadyRecords := utils.Filter(records, func(record *kuadrantdnsv1alpha1.DNSRecord) bool {
		return meta.IsStatusConditionFalse(record.Status.Conditions, string(kuadrantdnsv1alpha1.ConditionTypeReady))
	})

	// if there are records and none of the records are ready
	if len(records) > 0 && len(notReadyRecords) == len(records) {
		return kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(kuadrantv1.DNSPolicyGroupKind.Kind, errors.New("policy is not enforced on any DNSRecord: not a single DNSRecord is ready")), false)
	}

	// some of the records are not ready
	if len(notReadyRecords) > 0 {
		additionalMessage := ". Not ready DNSRecords are: "
		for _, record := range notReadyRecords {
			additionalMessage += fmt.Sprintf("%s ", record.Name)
		}
		cond := kuadrant.EnforcedCondition(dnsPolicy, nil, false)
		cond.Message += additionalMessage
		return cond
	}
	// all records are ready
	return kuadrant.EnforcedCondition(dnsPolicy, nil, true)
}

func healthyCondition(records []*kuadrantdnsv1alpha1.DNSRecord, dnsPolicy *kuadrantv1.DNSPolicy) *metav1.Condition {
	// if we don't have records - consider healthy
	if len(records) == 0 {
		cond := dnsPolicyHealthyCondition(dnsPolicy, nil)
		cond.Message = "No sub-resources present"
		return cond
	}

	// filter not healthy records
	notHealthyRecords := utils.Filter(records, func(record *kuadrantdnsv1alpha1.DNSRecord) bool {
		return meta.IsStatusConditionFalse(record.Status.Conditions, string(kuadrantdnsv1alpha1.ConditionTypeHealthy))
	})

	// all records are healthy
	if len(notHealthyRecords) == 0 {
		cond := dnsPolicyHealthyCondition(dnsPolicy, nil)
		cond.Message = "All sub-resources are healthy"
		return cond
	}

	cond := dnsPolicyHealthyCondition(dnsPolicy, kuadrant.NewErrUnknown(kuadrantv1.DNSPolicyGroupKind.Kind, errors.New("not all sub-resources of policy are passing the policy defined health check")))
	additionalMessage := ". Not healthy DNSRecords are: "
	for _, record := range notHealthyRecords {
		additionalMessage += fmt.Sprintf("%s ", record.Name)
	}
	cond.Message += additionalMessage

	return cond
}

var NegativePolarityConditions []string

func propagateRecordConditions(records []*kuadrantdnsv1alpha1.DNSRecord, policyStatus *kuadrantv1.DNSPolicyStatus) {
	//reset conditions
	policyStatus.RecordConditions = map[string][]metav1.Condition{}

	for _, record := range records {
		var allConditions []metav1.Condition
		allConditions = append(allConditions, record.Status.Conditions...)
		if record.Status.HealthCheck != nil {
			allConditions = append(allConditions, record.Status.HealthCheck.Conditions...)

			if record.Status.HealthCheck.Probes != nil {
				for _, probeStatus := range record.Status.HealthCheck.Probes {
					allConditions = append(allConditions, probeStatus.Conditions...)
				}
			}
		}

		for _, condition := range allConditions {
			//skip healthy negative polarity conditions
			if slices.Contains(NegativePolarityConditions, condition.Type) &&
				strings.ToLower(string(condition.Status)) == "false" {
				continue
			}
			//skip healthy positive polarity conditions
			if !slices.Contains(NegativePolarityConditions, condition.Type) &&
				strings.ToLower(string(condition.Status)) == "true" {
				continue
			}

			policyStatus.RecordConditions[record.Spec.RootHost] = append(
				policyStatus.RecordConditions[record.Spec.RootHost],
				condition)
		}
	}
}
