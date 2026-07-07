package controllers

import (
	"context"
	"fmt"
	"sync"

	authorinoopapi "github.com/kuadrant/authorino-operator/api/v1beta1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/helm"
)

// HelmAuthorinoReconciler reconciles Authorino deployment using Helm charts
// instead of creating an Authorino CR
type HelmAuthorinoReconciler struct {
	Client    *dynamic.DynamicClient
	ChartPath string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func NewHelmAuthorinoReconciler(client *dynamic.DynamicClient, chartPath string) *HelmAuthorinoReconciler {
	return &HelmAuthorinoReconciler{
		Client:    client,
		ChartPath: chartPath,
	}
}

func (r *HelmAuthorinoReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(v1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(v1beta1.AuthorinoGroupKind), EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *HelmAuthorinoReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	span := trace.SpanFromContext(ctx)
	logger := controller.LoggerFromContext(ctx).WithName("HelmAuthorinoReconciler")
	logger.V(1).Info("reconciling authorino via helm", "status", "started")
	defer logger.V(1).Info("reconciling authorino via helm", "status", "completed")

	// Get Authorino CR from topology
	authorinoObj := GetAuthorinoFromTopology(topology, state)
	if authorinoObj == nil {
		span.AddEvent("no authorino object found")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger = logger.WithValues("authorino", authorinoObj.Namespace+"/"+authorinoObj.Name)

	// Build Helm values from Authorino spec
	values := r.buildHelmValues(authorinoObj)

	// Render chart
	renderer := helm.NewRenderer(r.ChartPath)
	objects, err := renderer.Render("authorino", authorinoObj.Namespace, values)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to render authorino chart")
		logger.Error(err, "failed to render authorino chart")
		return err
	}

	logger.Info("rendered authorino chart", "resourceCount", len(objects))

	// Apply each rendered resource using Server-Side Apply
	for _, obj := range objects {
		// Set owner reference to Authorino CR
		obj.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: authorinoObj.APIVersion,
				Kind:       authorinoObj.Kind,
				Name:       authorinoObj.Name,
				UID:        authorinoObj.UID,
				Controller: ptr.To(true),
			},
		})

		gvr := obj.GroupVersionKind().GroupVersion().WithResource(kindToResource(obj.GetKind()))

		// ClusterRoleBindings are cluster-scoped, don't apply with namespace
		var resourceClient dynamic.ResourceInterface
		if obj.GetKind() == "ClusterRoleBinding" {
			resourceClient = r.Client.Resource(gvr)
		} else {
			resourceClient = r.Client.Resource(gvr).Namespace(authorinoObj.Namespace)
		}

		_, err := resourceClient.Apply(
			ctx,
			obj.GetName(),
			obj,
			metav1.ApplyOptions{
				FieldManager: FieldManagerName,
				Force:        true,
			},
		)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("failed to apply %s/%s", obj.GetKind(), obj.GetName()))
			logger.Error(err, "failed to apply resource",
				"kind", obj.GetKind(),
				"name", obj.GetName(),
			)
			return err
		}

		logger.V(1).Info("applied resource",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
		)
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("authorino helm deployment reconciled successfully")

	return nil
}

func (r *HelmAuthorinoReconciler) buildHelmValues(authorinoObj *authorinoopapi.Authorino) map[string]interface{} {
	values := map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "quay.io/kuadrant/authorino",
			"tag":        "latest",
			"pullPolicy": "IfNotPresent",
		},
		"rbac": map[string]interface{}{
			"create": true,
		},
		"serviceAccount": map[string]interface{}{
			"create": true,
			"name":   "",
		},
	}

	// Use replicas from Authorino CR if specified
	if authorinoObj.Spec.Replicas != nil {
		values["replicas"] = *authorinoObj.Spec.Replicas
	} else {
		values["replicas"] = 1
	}

	// Use custom image if specified
	if authorinoObj.Spec.Image != "" {
		// Parse image into repository:tag
		imageParts := splitImageString(authorinoObj.Spec.Image)
		values["image"] = imageParts
	}

	// Set clusterWide from Authorino CR
	values["clusterWide"] = authorinoObj.Spec.ClusterWide

	// Set TLS configuration from Authorino CR
	// Both listener and oidcServer TLS must be disabled for tls.enabled: false
	tlsEnabled := authorinoObj.Spec.Listener.Tls.Enabled != nil && *authorinoObj.Spec.Listener.Tls.Enabled
	oidcTlsEnabled := authorinoObj.Spec.OIDCServer.Tls.Enabled != nil && *authorinoObj.Spec.OIDCServer.Tls.Enabled

	values["tls"] = map[string]interface{}{
		"enabled":        tlsEnabled || oidcTlsEnabled,
		"certSecretName": "authorino-oidc-server-cert",
	}

	// Build args based on TLS settings
	args := []string{
		"--auth-config-label-selector=authorino.kuadrant.io/managed-by=authorino",
		"--deep-metrics-enabled=true",
		"--log-level=info",
		"--log-mode=production",
		"--oidc-http-port=8083",
		"--timeout=0",
	}

	// Add TLS args only if enabled
	if tlsEnabled {
		args = append(args,
			"--tls-cert=/etc/ssl/certs/tls.crt",
			"--tls-cert-key=/etc/ssl/private/tls.key",
		)
	}
	if oidcTlsEnabled {
		args = append(args,
			"--oidc-tls-cert=/etc/ssl/certs/oidc.crt",
			"--oidc-tls-cert-key=/etc/ssl/private/oidc.key",
		)
	}

	values["args"] = args

	return values
}

// splitImageString splits "repo:tag" into map[string]interface{}{"repository": "repo", "tag": "tag"}
func splitImageString(image string) map[string]interface{} {
	parts := make(map[string]interface{})

	// Simple split on last ':'
	lastColon := -1
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon > 0 {
		parts["repository"] = image[:lastColon]
		parts["tag"] = image[lastColon+1:]
	} else {
		parts["repository"] = image
		parts["tag"] = "latest"
	}

	parts["pullPolicy"] = "IfNotPresent"
	return parts
}

// kindToResource converts Kind to resource name (simple pluralization)
func kindToResource(kind string) string {
	switch kind {
	case "Service":
		return "services"
	case "ServiceAccount":
		return "serviceaccounts"
	case "Deployment":
		return "deployments"
	case "ConfigMap":
		return "configmaps"
	case "ClusterRoleBinding":
		return "clusterrolebindings"
	default:
		// Simple pluralization
		return kind + "s"
	}
}
