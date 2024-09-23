package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclientgosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

var (
	ConfigMapGroupKind = schema.GroupKind{Group: corev1.GroupName, Kind: "ConfigMap"}
	operatorNamespace  = env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
)

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=list;watch

func NewPolicyMachineryController(manager ctrlruntime.Manager, client *dynamic.DynamicClient, logger logr.Logger) *controller.Controller {
	controllerOpts := []controller.ControllerOption{
		controller.ManagedBy(manager),
		controller.WithLogger(logger),
		controller.WithClient(client),
		controller.WithRunnable("kuadrant watcher", controller.Watch(&kuadrantv1beta1.Kuadrant{}, kuadrantv1beta1.KuadrantResource, metav1.NamespaceAll)),
		controller.WithRunnable("dnspolicy watcher", controller.Watch(&kuadrantv1alpha1.DNSPolicy{}, kuadrantv1alpha1.DNSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("tlspolicy watcher", controller.Watch(&kuadrantv1alpha1.TLSPolicy{}, kuadrantv1alpha1.TLSPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("authpolicy watcher", controller.Watch(&kuadrantv1beta2.AuthPolicy{}, kuadrantv1beta2.AuthPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("ratelimitpolicy watcher", controller.Watch(&kuadrantv1beta2.RateLimitPolicy{}, kuadrantv1beta2.RateLimitPoliciesResource, metav1.NamespaceAll)),
		controller.WithRunnable("topology configmap watcher", controller.Watch(&corev1.ConfigMap{}, controller.ConfigMapsResource, operatorNamespace, controller.FilterResourcesByLabel[*corev1.ConfigMap](fmt.Sprintf("%s=true", kuadrant.TopologyLabel)))),
		controller.WithRunnable("limitador watcher", controller.Watch(&limitadorv1alpha1.Limitador{}, kuadrantv1beta1.LimitadorResource, metav1.NamespaceAll)),
		controller.WithRunnable("authorino watcher", controller.Watch(&authorinov1beta1.Authorino{}, kuadrantv1beta1.AuthorinoResource, metav1.NamespaceAll)),
		controller.WithPolicyKinds(
			kuadrantv1alpha1.DNSPolicyKind,
			kuadrantv1alpha1.TLSPolicyKind,
			kuadrantv1beta2.AuthPolicyKind,
			kuadrantv1beta2.RateLimitPolicyKind,
		),
		controller.WithObjectKinds(
			kuadrantv1beta1.KuadrantKind,
			ConfigMapGroupKind,
			kuadrantv1beta1.LimitadorKind,
			kuadrantv1beta1.AuthorinoKind,
		),
		controller.WithObjectLinks(
			kuadrantv1beta1.LinkKuadrantToGatewayClasses,
			kuadrantv1beta1.LinkKuadrantToLimitador,
			kuadrantv1beta1.LinkKuadrantToAuthorino,
		),
		controller.WithReconcile(buildReconciler(client)),
	}

	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("gateway api is not installed, skipping watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("gatewayclass watcher", controller.Watch(&gwapiv1.GatewayClass{}, controller.GatewayClassesResource, metav1.NamespaceAll)),
			controller.WithRunnable("gateway watcher", controller.Watch(&gwapiv1.Gateway{}, controller.GatewaysResource, metav1.NamespaceAll)),
			controller.WithRunnable("httproute watcher", controller.Watch(&gwapiv1.HTTPRoute{}, controller.HTTPRoutesResource, metav1.NamespaceAll)),
		)
	}

	ok, err = envoygateway.IsEnvoyGatewayInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("envoygateway is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoypatchpolicy watcher", controller.Watch(&egv1alpha1.EnvoyPatchPolicy{}, envoygateway.EnvoyPatchPoliciesResource, metav1.NamespaceAll)),
			controller.WithRunnable("envoyextensionpolicy watcher", controller.Watch(&egv1alpha1.EnvoyExtensionPolicy{}, envoygateway.EnvoyExtensionPoliciesResource, metav1.NamespaceAll)),
			controller.WithRunnable("envoysecuritypolicy watcher", controller.Watch(&egv1alpha1.SecurityPolicy{}, envoygateway.SecurityPoliciesResource, metav1.NamespaceAll)),
			controller.WithObjectKinds(
				envoygateway.EnvoyPatchPolicyGroupKind,
				envoygateway.EnvoyExtensionPolicyGroupKind,
				envoygateway.SecurityPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add specific tasks to workflow
	}

	ok, err = istio.IsIstioInstalled(manager.GetRESTMapper())
	if err != nil || !ok {
		logger.Info("istio is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("envoyfilter watcher", controller.Watch(&istioclientnetworkingv1alpha3.EnvoyFilter{}, istio.EnvoyFiltersResource, metav1.NamespaceAll)),
			controller.WithRunnable("wasmplugin watcher", controller.Watch(&istioclientgoextensionv1alpha1.WasmPlugin{}, istio.WasmPluginsResource, metav1.NamespaceAll)),
			controller.WithRunnable("authorizationpolicy watcher", controller.Watch(&istioclientgosecurityv1beta1.AuthorizationPolicy{}, istio.AuthorizationPoliciesResource, metav1.NamespaceAll)),
			controller.WithObjectKinds(
				istio.EnvoyFilterGroupKind,
				istio.WasmPluginGroupKind,
				istio.AuthorizationPolicyGroupKind,
			),
			// TODO: add object links
		)
		// TODO: add istio specific tasks to workflow
	}

	ok, err = kuadrantgatewayapi.IsCertManagerInstalled(manager.GetRESTMapper(), logger)
	if err != nil || !ok {
		logger.Info("cert manager is not installed, skipping related watches and reconcilers", "err", err)
	} else {
		controllerOpts = append(controllerOpts,
			controller.WithRunnable("certificate watcher", controller.Watch(&certmanagerv1.Certificate{}, CertManagerCertificatesResource, metav1.NamespaceAll)),
			controller.WithRunnable("issuers watcher", controller.Watch(&certmanagerv1.Issuer{}, CertManagerIssuersResource, metav1.NamespaceAll)),
			controller.WithRunnable("clusterissuers watcher", controller.Watch(&certmanagerv1.Certificate{}, CertMangerClusterIssuersResource, metav1.NamespaceAll)),
			controller.WithObjectKinds(
				CertManagerCertificateKind,
				CertManagerIssuerKind,
				CertManagerClusterIssuerKind,
			),
			// TODO: add object links
		)
		// TODO: add tls policy specific tasks to workflow
	}

	return controller.NewController(controllerOpts...)
}

func buildReconciler(client *dynamic.DynamicClient) controller.ReconcileFunc {
	reconciler := &controller.Workflow{
		Precondition: (&controller.Workflow{
			Precondition: NewEventLogger().Log,
			Tasks: []controller.ReconcileFunc{
				NewTopologyFileReconciler(client, operatorNamespace).Reconcile,
			},
		}).Run,
		Tasks: []controller.ReconcileFunc{
			NewAuthorinoCrReconciler(client).Subscription().Reconcile,
		},
	}
	return reconciler.Run
}

type AuthorinoCrReconciler struct {
	Client *dynamic.DynamicClient
}

func NewAuthorinoCrReconciler(client *dynamic.DynamicClient) *AuthorinoCrReconciler {
	return &AuthorinoCrReconciler{Client: client}
}

func (r *AuthorinoCrReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: ptr.To(kuadrantv1beta1.KuadrantKind), EventType: ptr.To(controller.CreateEvent)},
			{Kind: ptr.To(kuadrantv1beta1.AuthorinoKind), EventType: ptr.To(controller.DeleteEvent)},
		},
	}
}

func (r *AuthorinoCrReconciler) Reconcile(ctx context.Context, events []controller.ResourceEvent, topology *machinery.Topology, _ error) {
	logger := controller.LoggerFromContext(ctx).WithName("AuthorinoCrReconciler")
	logger.Info("Reconciling Authorino Cr")

	kobj := &kuadrantv1beta1.Kuadrant{}
	for _, event := range events {
		if event.Kind == kuadrantv1beta1.KuadrantKind && event.EventType == controller.CreateEvent {
			kobjs := lo.FilterMap(topology.Objects().Roots(), func(item machinery.Object, _ int) (*kuadrantv1beta1.Kuadrant, bool) {
				if item.GetName() == event.NewObject.GetName() && item.GetNamespace() == event.NewObject.GetNamespace() && item.GroupVersionKind().Kind == event.NewObject.GetObjectKind().GroupVersionKind().Kind {
					return item.(*kuadrantv1beta1.Kuadrant), true
				}
				return nil, false
			})
			if len(kobjs) > 1 {
				logger.Error(fmt.Errorf("multiple Kuadrant resources found"), "cannot select root Kuadrant resource")
			}
			if len(kobjs) == 0 {
				logger.Info("no kuadrant resources found")
				return
			}
			kobj = kobjs[0]
			break
		} else if event.Kind == kuadrantv1beta1.AuthorinoKind && event.EventType == controller.DeleteEvent {
			kobjs := lo.FilterMap(topology.Objects().Roots(), func(item machinery.Object, _ int) (*kuadrantv1beta1.Kuadrant, bool) {
				if item.GetNamespace() == event.OldObject.GetNamespace() && item.GroupVersionKind().Kind == kuadrantv1beta1.KuadrantKind.Kind {
					return item.(*kuadrantv1beta1.Kuadrant), true
				}
				return nil, false
			})

			if len(kobjs) == 0 {
				logger.Info("no possible kuadrant parent, wont create Authorino CR.")
				return
			}
			if len(kobjs) != 1 {
				logger.Error(fmt.Errorf("muiltply kuadrant CRs found"), "unexpected behaviour may happen")
			}
			kobj = kobjs[0]
			if kobj.GetDeletionTimestamp() != nil {
				logger.Info("kuadrant CR marked for deletion, wont create Authorino CR.")
				return
			}
			break
		}
	}

	authorino := &authorinov1beta1.Authorino{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Authorino",
			APIVersion: "operator.authorino.kuadrant.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authorino",
			Namespace: kobj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         kobj.GroupVersionKind().GroupVersion().String(),
					Kind:               kobj.GroupVersionKind().Kind,
					Name:               kobj.Name,
					UID:                kobj.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: authorinov1beta1.AuthorinoSpec{
			ClusterWide:            true,
			SupersedingHostSubsets: true,
			Listener: authorinov1beta1.Listener{
				Tls: authorinov1beta1.Tls{
					Enabled: ptr.To(false),
				},
			},
			OIDCServer: authorinov1beta1.OIDCServer{
				Tls: authorinov1beta1.Tls{
					Enabled: ptr.To(false),
				},
			},
		},
	}

	authorinos := lo.Filter(topology.Objects().Items(), func(item machinery.Object, _ int) bool {
		return item.GetNamespace() == authorino.GetNamespace() && item.GetName() == authorino.GetName() && item.GroupVersionKind().Kind == authorino.Kind
	})

	if len(authorinos) > 0 {
		logger.V(1).Info("authorino CR already in topology, exiting reconcile")
		return
	}

	unstructuredAuthorino, err := controller.Destruct(authorino)
	if err != nil {
		logger.Error(err, "failed to destruct authorino")
	}
	_, err = r.Client.Resource(kuadrantv1beta1.AuthorinoResource).Namespace(authorino.Namespace).Create(ctx, unstructuredAuthorino, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("already created authorino Cr")
		} else {
			logger.Error(err, "failed to create authorino Cr")
		}
	}
}

type TopologyFileReconciler struct {
	Client    *dynamic.DynamicClient
	Namespace string
}

func NewTopologyFileReconciler(client *dynamic.DynamicClient, namespace string) *TopologyFileReconciler {
	if namespace == "" {
		panic("namespace must be specified and can not be a blank string")
	}
	return &TopologyFileReconciler{Client: client, Namespace: namespace}
}

func (r *TopologyFileReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("topology file")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "topology",
			Namespace: r.Namespace,
			Labels:    map[string]string{kuadrant.TopologyLabel: "true"},
		},
		Data: map[string]string{
			"topology": topology.ToDot(),
		},
	}
	unstructuredCM, err := controller.Destruct(cm)
	if err != nil {
		logger.Error(err, "failed to destruct topology configmap")
		return err
	}

	existingTopologyConfigMaps := topology.Objects().Items(func(object machinery.Object) bool {
		return object.GetName() == cm.GetName() && object.GetNamespace() == cm.GetNamespace() && object.GroupVersionKind().Kind == ConfigMapGroupKind.Kind
	})

	if len(existingTopologyConfigMaps) == 0 {
		_, err = r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Create(ctx, unstructuredCM, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// This error can happen when the operator is starting, and the create event for the topology has not being processed.
				logger.Info("already created topology configmap, must not be in topology yet")
				return
			}
			logger.Error(err, "failed to write topology configmap")
		}
		return err
	}

	if len(existingTopologyConfigMaps) > 1 {
		logger.Info("multiple topology configmaps found, continuing but unexpected behaviour may occur")
	}
	existingTopologyConfigMap := existingTopologyConfigMaps[0].(controller.Object).(*controller.RuntimeObject)
	cmTopology := existingTopologyConfigMap.Object.(*corev1.ConfigMap)

	if d, found := cmTopology.Data["topology"]; !found || strings.Compare(d, cm.Data["topology"]) != 0 {
		_, err = r.Client.Resource(controller.ConfigMapsResource).Namespace(cm.Namespace).Update(ctx, unstructuredCM, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "failed to update topology configmap")
		}
		return err
	}

	return nil
}

type EventLogger struct{}

func NewEventLogger() *EventLogger {
	return &EventLogger{}
}

func (e *EventLogger) Log(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology, err error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("event logger")
	for _, event := range resourceEvents {
		// log the event
		obj := event.OldObject
		if obj == nil {
			obj = event.NewObject
		}
		values := []any{
			"type", event.EventType.String(),
			"kind", obj.GetObjectKind().GroupVersionKind().Kind,
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
		if event.EventType == controller.UpdateEvent && logger.V(1).Enabled() {
			values = append(values, "diff", cmp.Diff(event.OldObject, event.NewObject))
		}
		logger.Info("new event", values...)
		if err != nil {
			logger.Error(err, "error passed to reconcile")
		}
	}

	return nil
}
