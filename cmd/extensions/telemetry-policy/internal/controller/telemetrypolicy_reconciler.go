package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
	versioninfo "github.com/kuadrant/kuadrant-operator/internal/version"
	wasmtypes "github.com/kuadrant/kuadrant-operator/internal/wasm"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

const istioGatewayControllerName gatewayapiv1.GatewayController = "istio.io/gateway-controller"

const (
	ocsfEnvoyMinVersionAnnotation = "kuadrant.io/ocsf-envoy-min-version"
	ocsfRequiredEnvoyVersion      = "1.36.0"
)

const (
	ocsfResourcesStorePath = wasmtypes.OCSFResourcesStorePath
	ocsfAIModelStorePath   = wasmtypes.OCSFAIModelStorePath
	ocsfURLPathStorePath   = wasmtypes.OCSFURLPathStorePath
	ocsfEventStorePath     = wasmtypes.OCSFEventStorePath
)

// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies/finalizers,verbs=update

type TelemetryPolicyReconciler struct {
	types.ExtensionBase
}

type ocsfEnvoyVersionError struct {
	reason  gatewayapiv1alpha2.PolicyConditionReason
	message string
}

func (e ocsfEnvoyVersionError) Error() string { return e.message }

func (e ocsfEnvoyVersionError) Reason() gatewayapiv1alpha2.PolicyConditionReason { return e.reason }

func validateOCSFEnvoyVersion(gatewayRef, versionText string) error {
	if strings.TrimSpace(versionText) == "" {
		return ocsfEnvoyVersionError{
			reason: gatewayapiv1alpha2.PolicyConditionReason("EnvoyVersionNotVerified"),
			message: fmt.Sprintf(
				"OCSF access logging requires Envoy >= %s; the minimum version across running proxies for Gateway %s is unverified. Set the %s annotation on its TelemetryPolicy only after checking every proxy replica.",
				ocsfRequiredEnvoyVersion, gatewayRef, ocsfEnvoyMinVersionAnnotation,
			),
		}
	}

	versionText = strings.TrimSpace(versionText)
	minimumVersion, err := utilversion.ParseSemantic(versionText)
	if err != nil {
		return ocsfEnvoyVersionError{
			reason: gatewayapiv1alpha2.PolicyConditionReason("EnvoyVersionNotVerified"),
			message: fmt.Sprintf(
				"OCSF access logging requires Envoy >= %s; Gateway %s has an invalid %s TelemetryPolicy annotation %q. Set it to the lowest attested version across every proxy replica.",
				ocsfRequiredEnvoyVersion, gatewayRef, ocsfEnvoyMinVersionAnnotation, versionText,
			),
		}
	}

	requiredVersion := utilversion.MustParseSemantic(ocsfRequiredEnvoyVersion)
	if !minimumVersion.AtLeast(requiredVersion) {
		return ocsfEnvoyVersionError{
			reason: gatewayapiv1alpha2.PolicyConditionReason("EnvoyVersionUnsupported"),
			message: fmt.Sprintf(
				"OCSF access logging requires Envoy >= %s; the TelemetryPolicy attests a minimum of %s for Gateway %s. Upgrade every proxy replica before enabling OCSF access logging.",
				ocsfRequiredEnvoyVersion, versionText, gatewayRef,
			),
		}
	}

	return nil
}

func ocsfGatewayClassExpression(gateway string) string {
	return fmt.Sprintf("%s.spec.gatewayClassName", gateway)
}

func NewTelemetryPolicyReconciler() *TelemetryPolicyReconciler {
	return &TelemetryPolicyReconciler{}
}

func (r *TelemetryPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}
	r.Logger.Info("reconciling telemetrypolicies started")
	defer r.Logger.Info("reconciling telemetrypolicies completed")

	telemetryPolicy := &v1alpha1.TelemetryPolicy{}
	if err := r.Client.Get(ctx, request.NamespacedName, telemetryPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Error(err, "telemetrypolicy not found")
			return reconcile.Result{}, nil
		}
		r.Logger.Error(err, "failed to retrieve telemetrypolicy")
		return reconcile.Result{}, err
	}

	if telemetryPolicy.GetDeletionTimestamp() != nil {
		r.Logger.Info("telemetrypolicy marked for deletion")
		return reconcile.Result{}, nil
	}

	telemetryPolicyStatus, specErr := r.reconcileSpec(ctx, telemetryPolicy, kuadrantCtx)
	statusResult, statusErr := r.reconcileStatus(ctx, telemetryPolicy, telemetryPolicyStatus)

	if specErr != nil {
		return reconcile.Result{}, specErr
	}
	if statusErr != nil {
		return reconcile.Result{}, statusErr
	}

	if statusResult.RequeueAfter > 0 {
		r.Logger.Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	return reconcile.Result{}, nil
}

func (r *TelemetryPolicyReconciler) reconcileSpec(ctx context.Context, pol *v1alpha1.TelemetryPolicy, kuadrantCtx types.KuadrantCtx) (*v1alpha1.TelemetryPolicyStatus, error) {
	// Validate every expression before registering any binding, so a rejected policy contributes
	// nothing to the data plane. A rejection is terminal until the spec changes, and that change
	// triggers a new reconcile, so it is reported through the status without requeueing.
	if err := validateExpressions(pol); err != nil {
		r.Logger.Info("telemetrypolicy rejected", "reason", err.Error())
		// Clear actions left by an earlier valid reconcile, including OCSF actions.
		pipeline := kuadrantCtx.NewPipeline(pol)
		if commitErr := pipeline.Commit(ctx); commitErr != nil {
			r.Logger.Error(commitErr, "failed to clear rejected telemetry policy pipeline")
			return calculateErrorStatus(pol, commitErr), commitErr
		}
		return calculateErrorStatus(pol, err), nil
	}

	ocsfSupported := false
	var ocsfStatus *v1alpha1.TelemetryPolicyStatus
	var ocsfValidationErr error
	if pol.Spec.Logging != nil && pol.Spec.Logging.Default.OCSF != nil {
		ocsfSupported, ocsfStatus, ocsfValidationErr = r.validateOCSFGatewayProvider(ctx, pol, kuadrantCtx)
	}

	if pol.Spec.Metrics != nil {
		for binding, expression := range pol.Spec.Metrics.Default.Labels {
			if err := kuadrantCtx.AddDataTo(ctx, pol, types.DomainRequest, types.KuadrantMetricBinding(binding), expression); err != nil {
				r.Logger.Error(err, "failed to add data to request domain")
				return calculateErrorStatus(pol, err), err
			}
		}
	}

	if pol.Spec.Logging != nil {
		for binding, expression := range pol.Spec.Logging.Default.Fields {
			if err := kuadrantCtx.AddDataTo(ctx, pol, types.DomainRequest, types.KuadrantLoggingBinding(binding), expression); err != nil {
				r.Logger.Error(err, "failed to add data to request domain")
				return calculateErrorStatus(pol, err), err
			}
		}

	}

	pipeline := kuadrantCtx.NewPipeline(pol)
	if pol.Spec.Logging != nil && pol.Spec.Logging.Default.OCSF != nil && ocsfSupported {
		// Capture request metadata before terminal actions can return early; capture Authorino identity
		// in the response phase, after authorization has produced it.
		requestActions, responseActions := ocsfStoreActions(pol.Spec.Logging.Default.OCSF)
		if err := pipeline.OnHTTPRequestPrelude(requestActions...); err != nil {
			r.Logger.Error(err, "failed to configure OCSF request metadata")
			return calculateErrorStatus(pol, err), err
		}
		if err := pipeline.OnHTTPResponse(responseActions...); err != nil {
			r.Logger.Error(err, "failed to configure OCSF actor metadata")
			return calculateErrorStatus(pol, err), err
		}
	}
	// Commit even when OCSF is absent or unsupported so stale actions from a prior
	// successful reconcile are removed atomically while metrics and custom logging continue.
	if err := pipeline.Commit(ctx); err != nil {
		r.Logger.Error(err, "failed to commit telemetry pipeline")
		return calculateErrorStatus(pol, err), err
	}

	if ocsfValidationErr != nil {
		r.Logger.Error(ocsfValidationErr, "cannot validate Gateway provider for OCSF logging")
		return calculateErrorStatus(pol, ocsfValidationErr), ocsfValidationErr
	}
	if ocsfStatus != nil {
		return ocsfStatus, nil
	}
	return calculateEnforcedStatus(pol, nil), nil
}

func (r *TelemetryPolicyReconciler) validateOCSFGatewayProvider(ctx context.Context, pol *v1alpha1.TelemetryPolicy, kuadrantCtx types.KuadrantCtx) (bool, *v1alpha1.TelemetryPolicyStatus, error) {
	if r.Client == nil {
		return false, nil, fmt.Errorf("cannot validate OCSF Gateway provider: Kubernetes client is not configured")
	}

	gatewayClassNames, err := extcontroller.Resolve[[]string](
		ctx,
		kuadrantCtx,
		pol,
		fmt.Sprintf(`self.findGateways().map(gateway, %s)`, ocsfGatewayClassExpression("gateway")),
		true,
	)
	if err != nil {
		return false, nil, fmt.Errorf("failed to resolve target Gateway from Kuadrant topology: %w", err)
	}
	if len(gatewayClassNames) == 0 {
		statusErr := fmt.Errorf("target Gateway %s/%s was not found in Kuadrant topology", pol.Namespace, pol.Spec.TargetRef.Name)
		return false, calculateErrorStatus(pol, statusErr), nil
	}
	if len(gatewayClassNames) != 1 {
		statusErr := fmt.Errorf("expected one target Gateway for OCSF logging, resolved %d", len(gatewayClassNames))
		return false, calculateErrorStatus(pol, statusErr), nil
	}

	// The subscribed topology resolve provides the GatewayClass name. The current
	// Kuadrant CEL surface does not expose GatewayClass objects, so fetch the
	// cluster-scoped class to validate its controller.
	gatewayClassName := gatewayClassNames[0]
	gatewayClass := &gatewayapiv1.GatewayClass{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: gatewayClassName}, gatewayClass); err != nil {
		if errors.IsNotFound(err) {
			statusErr := fmt.Errorf("GatewayClass %q for OCSF logging was not found: %w", gatewayClassName, err)
			return false, calculateEnforcedStatus(pol, statusErr), nil
		}
		return false, nil, fmt.Errorf("failed to get GatewayClass %q for OCSF provider validation: %w", gatewayClassName, err)
	}
	if gatewayClass.Spec.ControllerName != istioGatewayControllerName {
		statusErr := fmt.Errorf("OCSF logging is only supported by Istio GatewayClasses; GatewayClass %q uses controller %q", gatewayClass.Name, gatewayClass.Spec.ControllerName)
		return false, calculateEnforcedStatus(pol, statusErr), nil
	}

	// Envoy versions are not exposed by the Gateway API. Require the administrator
	// to attest the lowest version across every running proxy for this policy's
	// single Gateway target. The policy annotation is watched, so changing the
	// attestation reconciles upgrades and downgrades.
	gatewayRef := fmt.Sprintf("%s/%s", pol.Namespace, pol.Spec.TargetRef.Name)
	if err := validateOCSFEnvoyVersion(gatewayRef, pol.Annotations[ocsfEnvoyMinVersionAnnotation]); err != nil {
		return false, calculateEnforcedStatus(pol, err), nil
	}

	return true, nil, nil
}

func ocsfStoreActions(config *v1alpha1.OCSFLoggingConfig) (requestActions, responseActions []types.Action) {
	namespaceExpression, namespacePredicate := pathSegmentExpression(config.Target.RequestPath.NamespaceSegment)
	nameExpression, namePredicate := pathSegmentExpression(config.Target.RequestPath.NameSegment)
	segmentsPresent := fmt.Sprintf("(%s) && (%s)", namespacePredicate, namePredicate)
	resourceValue := fmt.Sprintf(
		`%s ? [{"name": %s, "namespace": %s, "type": %s, "role_id": 1, "role": "Target"}] : []`,
		segmentsPresent, nameExpression, namespaceExpression, strconv.Quote(config.Target.ResourceType),
	)
	provider := strconv.Quote(config.Target.AIProvider)
	aiModelValue := fmt.Sprintf(
		`(%s) ? {"name": %s, "ai_provider": %s} : {"ai_provider": %s}`,
		namePredicate, nameExpression, provider, provider,
	)
	requestPathValue := "request.url_path"
	requestActions = []types.Action{
		types.StoreAction{
			Path:  ocsfResourcesStorePath,
			Value: resourceValue,
		},
		types.StoreAction{
			Path:  ocsfAIModelStorePath,
			Value: aiModelValue,
		},
		types.StoreAction{
			Path:  ocsfURLPathStorePath,
			Value: requestPathValue,
		},
	}

	// Store a fallback event before terminal authorization actions so denied requests
	// can still be logged. Authorino's response phase overwrites this export with the
	// authenticated actor before Envoy emits successful requests.
	eventAction := func(actorUser string) types.StoreAction {
		return types.StoreAction{
			Path:         ocsfEventStorePath,
			Value:        ocsfEventValueWithActor(config, actorUser),
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		}
	}
	requestActions = append(requestActions, eventAction(`{"type_id": 0}`))
	responseActions = []types.Action{eventAction(ocsfActorUserValueOrUnknown(config.Actor))}
	return requestActions, responseActions
}

func ocsfEventValue(config *v1alpha1.OCSFLoggingConfig) string {
	return ocsfEventValueWithActor(config, ocsfActorUserValueOrUnknown(config.Actor))
}

func ocsfEventValueWithActor(config *v1alpha1.OCSFLoggingConfig, actorUser string) string {
	// Keep the shim export to values available before Envoy completes the stream.
	// Envoy adds final timestamps, response fields, and status in its access-log formatter.
	requestMethod := `(request.method != null ? request.method : "")`
	httpVersion := `(request.protocol != null ? (request.protocol == "HTTP/1.0" ? "1.0" : request.protocol == "HTTP/1.1" ? "1.1" : request.protocol == "HTTP/2" ? "2.0" : request.protocol == "HTTP/3" ? "3.0" : request.protocol) : "")`
	requestPath := fmt.Sprintf(`(%s != null ? %s : "")`, ocsfURLPathStorePath, ocsfURLPathStorePath)
	fields := []string{
		`"activity_id": 99`,
		`"activity_name": "Inference"`,
		`"category_uid": 6`,
		`"category_name": "Application Activity"`,
		`"class_uid": 6003`,
		`"class_name": "API Activity"`,
		`"type_uid": 600399`,
		`"severity_id": 1`,
		`"severity": "Informational"`,
		fmt.Sprintf(`"metadata": {"version": "1.9.0", "profiles": ["ai_operation"], "product": {"name": "kuadrant-gateway", "version": %s}}`, strconv.Quote(versioninfo.String())),
		fmt.Sprintf(`"actor": {"user": %s}`, actorUser),
		fmt.Sprintf(`"api": {"operation": %s + " " + %s}`, requestMethod, requestPath),
		fmt.Sprintf(`"http_request": {"http_method": %s, "version": %s, "url": {"path": %s}}`, requestMethod, httpVersion, requestPath),
		fmt.Sprintf(`"resources": (%s != null ? %s : [])`, ocsfResourcesStorePath, ocsfResourcesStorePath),
		fmt.Sprintf(`"ai_model": (%s != null ? %s : {"ai_provider": %q})`, ocsfAIModelStorePath, ocsfAIModelStorePath, config.Target.AIProvider),
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

func ocsfActorUserValueOrUnknown(actor v1alpha1.OCSFActorConfig) string {
	return fmt.Sprintf("(%s) ? (%s) : {\"type_id\": 0}", ocsfSelectorPresentPredicate(actor.Identity), ocsfActorUserValue(actor))
}

func ocsfActorUserValue(actor v1alpha1.OCSFActorConfig) string {
	typeID := fmt.Sprintf(`%s.startsWith("system:serviceaccount:") ? 4 : 1`, actor.Identity)
	userMap := func(includeUID, includeGroups bool) string {
		fields := []string{fmt.Sprintf(`"name": %s`, actor.Identity)}
		if includeUID {
			fields = append(fields, fmt.Sprintf(`"uid": %s`, actor.UID))
		}
		if includeGroups {
			fields = append(fields, fmt.Sprintf(`"groups": %s.map(group, {"name": group})`, actor.Groups))
		}
		fields = append(fields, fmt.Sprintf(`"type_id": %s`, typeID))
		return "{" + strings.Join(fields, ", ") + "}"
	}

	if actor.UID == "" && actor.Groups == "" {
		return userMap(false, false)
	}
	if actor.UID == "" {
		groupsPresent := ocsfSelectorPresentPredicate(actor.Groups)
		return fmt.Sprintf("(%s) ? %s : %s", groupsPresent, userMap(false, true), userMap(false, false))
	}
	if actor.Groups == "" {
		uidPresent := ocsfSelectorPresentPredicate(actor.UID)
		return fmt.Sprintf("(%s) ? %s : %s", uidPresent, userMap(true, false), userMap(false, false))
	}

	uidPresent := ocsfSelectorPresentPredicate(actor.UID)
	groupsPresent := ocsfSelectorPresentPredicate(actor.Groups)
	return fmt.Sprintf("(%s) ? ((%s) ? %s : %s) : ((%s) ? %s : %s)",
		uidPresent, groupsPresent, userMap(true, true), userMap(true, false),
		groupsPresent, userMap(false, true), userMap(false, false))
}

func pathSegmentExpression(segment int32) (expression, predicate string) {
	// split preserves the empty prefix of an absolute path, hence the one-based CEL index.
	index := segment + 1
	parts := "request.url_path.split('/')"
	return fmt.Sprintf("%s[%d]", parts, index), fmt.Sprintf("%s.size() > %d", parts, index)
}

// ocsfSelectorPresentPredicate uses presence checks only because the WASM CEL runtime does not support type().
func ocsfSelectorPresentPredicate(selector string) string {
	parts := strings.Split(selector, ".")
	predicates := make([]string, 0, len(parts))
	for i := 2; i < len(parts); i++ {
		predicates = append(predicates, fmt.Sprintf("has(%s)", strings.Join(parts[:i], ".")))
	}
	predicates = append(predicates, fmt.Sprintf("has(%s) && %s != null", selector, selector))
	return strings.Join(predicates, " && ")
}

// validateExpressions rejects metric label and logging field expressions that could expose a
// sensitive request header. Keys are visited in sorted order so the reported error is stable
// across reconciles.
func validateExpressions(pol *v1alpha1.TelemetryPolicy) error {
	if pol.Spec.Metrics != nil {
		labels := pol.Spec.Metrics.Default.Labels
		for _, label := range slices.Sorted(maps.Keys(labels)) {
			if err := checkSensitiveHeaders(labels[label]); err != nil {
				return errInvalidExpression{path: fmt.Sprintf("spec.metrics.default.labels[%q]", label), err: err}
			}
		}
	}

	if pol.Spec.Logging != nil {
		fields := pol.Spec.Logging.Default.Fields
		for _, field := range slices.Sorted(maps.Keys(fields)) {
			if err := checkSensitiveHeaders(fields[field]); err != nil {
				return errInvalidExpression{path: fmt.Sprintf("spec.logging.default.fields[%q]", field), err: err}
			}
		}
	}

	return nil
}

func (r *TelemetryPolicyReconciler) reconcileStatus(ctx context.Context, pol *v1alpha1.TelemetryPolicy, newStatus *v1alpha1.TelemetryPolicyStatus) (ctrl.Result, error) {
	equalStatus := pol.Status.Equals(newStatus, r.Logger)
	r.Logger.Info("Status", "status is different", !equalStatus)
	r.Logger.Info("Status", "generation is different", pol.Generation != pol.Status.ObservedGeneration)
	if equalStatus && pol.Generation == pol.Status.ObservedGeneration {
		// Steady state
		r.Logger.Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	r.Logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", pol.Status.ObservedGeneration, newStatus.ObservedGeneration))

	pol.Status = *newStatus
	updateErr := r.Client.Status().Update(ctx, pol)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			r.Logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

// TODO: We could make these kind of func part of the SDK, we'd need to create a `PolicyStatus` interface and common struct for its fields
func calculateErrorStatus(pol *v1alpha1.TelemetryPolicy, specErr error) *v1alpha1.TelemetryPolicyStatus {
	newStatus := &v1alpha1.TelemetryPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))
	meta.RemoveStatusCondition(&newStatus.Conditions, string(types.PolicyConditionEnforced))
	return newStatus
}

func calculateEnforcedStatus(pol *v1alpha1.TelemetryPolicy, enforcedErr error) *v1alpha1.TelemetryPolicyStatus {
	newStatus := &v1alpha1.TelemetryPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}

	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, nil))
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, enforcedErr, true))
	return newStatus
}
