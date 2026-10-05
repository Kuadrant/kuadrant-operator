//go:build unit

package controllers

import (
	"context"
	"testing"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	controllersfake "github.com/kuadrant/kuadrant-operator/internal/controller/fake"
	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
)

func consoleRuntimeTopology(t *testing.T, version string) (*machinery.Topology, *configv1.ClusterVersion) {
	t.Helper()
	topology := buildTopologyWithClusterVersion(t)
	var clusterVersion *configv1.ClusterVersion
	for _, object := range topology.Objects().Items() {
		if cv, ok := object.(*controller.RuntimeObject).Object.(*configv1.ClusterVersion); ok {
			cv.Status.Desired.Version = version
			clusterVersion = cv
		}
	}
	return topology, clusterVersion
}

func assertConsoleRuntime(t *testing.T, c client.Client, image string, nginx bool) {
	t.Helper()
	ctx := context.Background()
	deployment := &appsv1.Deployment{}
	assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: TestNamespace, Name: consoleplugin.DeploymentName()}, deployment))
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, container.Image, image)
	assert.Equal(t, container.Ports[0].ContainerPort, int32(9443))
	var hasNginxMount, hasNginxVolume bool
	for _, mount := range container.VolumeMounts {
		if mount.Name == "nginx-conf" {
			hasNginxMount = true
			assert.Equal(t, mount.MountPath, "/etc/nginx/nginx.conf")
			assert.Equal(t, mount.SubPath, "nginx.conf")
			assert.Assert(t, mount.ReadOnly)
		}
	}
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == "nginx-conf" {
			hasNginxVolume = true
			assert.Assert(t, volume.ConfigMap != nil)
			assert.Equal(t, volume.ConfigMap.Name, "kuadrant-console-nginx-conf")
		}
	}
	assert.Equal(t, hasNginxMount, nginx)
	assert.Equal(t, hasNginxVolume, nginx)
	env := map[string]string{}
	for _, variable := range container.Env {
		env[variable.Name] = variable.Value
	}
	assert.Equal(t, env["TOPOLOGY_CONFIGMAP_NAME"], "topology")
	assert.Equal(t, env["TOPOLOGY_CONFIGMAP_NAMESPACE"], TestNamespace)
	plugin := &consolev1.ConsolePlugin{}
	assert.NilError(t, c.Get(ctx, client.ObjectKey{Name: consoleplugin.Name()}, plugin))
	if nginx {
		_, hasCert := env["TLS_CERTIFICATE_FILE"]
		_, hasKey := env["TLS_KEY_FILE"]
		assert.Assert(t, !hasCert && !hasKey)
		assert.Assert(t, is.Len(plugin.Spec.Proxy, 0))
		config := &corev1.ConfigMap{}
		assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: TestNamespace, Name: "kuadrant-console-nginx-conf"}, config))
		for _, directive := range []string{
			"listen              9443 ssl;", "listen              [::]:9443 ssl;",
			"ssl_certificate     /var/serving-cert/tls.crt;", "ssl_certificate_key /var/serving-cert/tls.key;",
			"root                /usr/share/nginx/html;", "location /config.js", "root /tmp;",
		} {
			assert.Assert(t, is.Contains(config.Data["nginx.conf"], directive))
		}
	} else {
		assert.Equal(t, env["TLS_CERTIFICATE_FILE"], "/var/serving-cert/tls.crt")
		assert.Equal(t, env["TLS_KEY_FILE"], "/var/serving-cert/tls.key")
		assert.Assert(t, is.Len(plugin.Spec.Proxy, 1))
		assert.Equal(t, plugin.Spec.Proxy[0].Alias, "backend")
		assert.Equal(t, plugin.Spec.Proxy[0].Authorization, consolev1.UserToken)
	}
}

func TestConsolePluginRuntimeLifecycle(t *testing.T) {
	const goImage = "quay.io/kuadrant/console-plugin:v0.7.0"
	const pf5Image = "quay.io/kuadrant/console-plugin:v0.1.5-2"
	t.Setenv(openshift.RelatedImageConsolePluginSDK1EnvVar, ConsolePluginImageURL)
	t.Setenv(openshift.RelatedImageConsolePluginLatestEnvVar, goImage)
	t.Setenv(openshift.RelatedImageConsolePluginPF5EnvVar, pf5Image)
	scheme := runtime.NewScheme()
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, appsv1.AddToScheme(scheme))
	assert.NilError(t, networkingv1.AddToScheme(scheme))
	assert.NilError(t, consolev1.AddToScheme(scheme))
	assert.NilError(t, configv1.AddToScheme(scheme))
	ctx := context.Background()
	for _, initialVersion := range []string{"4.19.0", "4.20.0", "4.21.0", "4.22.0-rc.1", "4.22.0"} {
		t.Run(initialVersion, func(t *testing.T) {
			// The live failure has a v0.6.0 image with the Go pod configuration.
			broken := consoleplugin.Deployment(TestNamespace, consoleplugin.Image{URL: ConsolePluginImageURL, Runtime: consoleplugin.RuntimeGo}, TopologyConfigMapName)
			broken.Spec.Template.Spec.Containers[0].Env = append(broken.Spec.Template.Spec.Containers[0].Env,
				corev1.EnvVar{Name: "METRICS_WORKLOAD_SUFFIX", Value: "-openshift-default"})
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broken).WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if d, ok := obj.(*appsv1.Deployment); ok {
						for _, volume := range d.Spec.Template.Spec.Volumes {
							if volume.ConfigMap != nil {
								// A pod template must never reference a ConfigMap that is not created yet.
								assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: d.Namespace, Name: volume.ConfigMap.Name}, &corev1.ConfigMap{}))
							}
						}
					}
					return c.Update(ctx, obj, opts...)
				},
			}).Build()
			manager := controllersfake.NewManagerBuilder().WithClient(c).WithScheme(scheme).Build()
			r := NewConsolePluginReconciler(manager, TestNamespace, consoleplugin.Image{})
			for _, version := range []string{initialVersion, "4.22.0", "4.21.0", "4.22.0"} {
				topology, cv := consoleRuntimeTopology(t, version)
				events := []controller.ResourceEvent{{Kind: openshift.ClusterVersionGroupKind.GroupKind(), EventType: controller.UpdateEvent, NewObject: cv}}
				assert.NilError(t, r.Subscription().Reconcile(ctx, events, topology, nil, nil))
				image, nginx := ConsolePluginImageURL, true
				switch version {
				case "4.19.0":
					image = pf5Image
				case "4.22.0", "4.22.0-rc.1":
					image, nginx = goImage, false
				}
				assertConsoleRuntime(t, c, image, nginx)
				d := &appsv1.Deployment{}
				key := client.ObjectKeyFromObject(broken)
				assert.NilError(t, c.Get(ctx, key, d))
				assert.Assert(t, is.Contains(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "METRICS_WORKLOAD_SUFFIX", Value: "-openshift-default"}))
				before := d.DeepCopy()
				assert.NilError(t, r.Run(ctx, nil, topology, nil, nil))
				assert.NilError(t, c.Get(ctx, key, d))
				assert.DeepEqual(t, d, before)
			}
			// The final Go rollout retains the ConfigMap needed by older pods.
			key := client.ObjectKey{Namespace: TestNamespace, Name: "kuadrant-console-nginx-conf"}
			assert.NilError(t, c.Get(ctx, key, &corev1.ConfigMap{}))
			empty, err := machinery.NewTopology()
			assert.NilError(t, err)
			assert.NilError(t, r.Run(ctx, nil, empty, nil, nil))
			assert.Assert(t, apierrors.IsNotFound(c.Get(ctx, key, &corev1.ConfigMap{})))
		})
	}
}

func TestConsolePluginFreshGoRuntimeDoesNotCreateNginxConfig(t *testing.T) {
	t.Setenv(openshift.RelatedImageConsolePluginLatestEnvVar, "quay.io/kuadrant/console-plugin:v0.7.0")
	scheme := runtime.NewScheme()
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, appsv1.AddToScheme(scheme))
	assert.NilError(t, networkingv1.AddToScheme(scheme))
	assert.NilError(t, consolev1.AddToScheme(scheme))
	manager := controllersfake.NewManagerBuilder().WithScheme(scheme).WithClient(fake.NewClientBuilder().WithScheme(scheme).Build()).Build()
	topology, _ := consoleRuntimeTopology(t, "4.22.0")
	assert.NilError(t, NewConsolePluginReconciler(manager, TestNamespace, consoleplugin.Image{}).Run(context.Background(), nil, topology, nil, nil))
	assertConsoleRuntime(t, manager.GetClient(), "quay.io/kuadrant/console-plugin:v0.7.0", false)
	err := manager.GetClient().Get(context.Background(), client.ObjectKey{Namespace: TestNamespace, Name: "kuadrant-console-nginx-conf"}, &corev1.ConfigMap{})
	assert.Assert(t, apierrors.IsNotFound(err))
}
