//go:build integration && ocsf_e2e

package istio_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/tests"
)

const (
	ocsfE2EReadyTimeout     = 5 * time.Minute
	ocsfE2EHTTPTimeout      = 15 * time.Second
	ocsfHTTPBinImage        = "quay.io/kuadrant/httpbin@sha256:2865a9ac6596340135fc3cd82cbc543c4050124afb10361e36f727abe4f71900"
	ocsfAccessLoggerName    = "envoy.access_loggers.stdout"
	ocsfResourceTypeKey     = "wasm.kuadrant.ocsf.resource.type"
	ocsfMarkerDiagnosticKey = "ocsf_marker_diagnostic"
)

var _ = Describe("OCSF access log", Label("ocsf", "long-running"), func() {
	It("emits an OCSF 1.9.0 API Activity event", func(ctx SpecContext) {
		if os.Getenv("OCSF_E2E") != "true" {
			Skip("OCSF_E2E is not true; use make test-ocsf-event-e2e")
		}
		testNamespace := tests.CreateNamespace(ctx, testClient())
		GinkgoWriter.Printf("OCSF E2E test namespace: %s\n", testNamespace)
		DeferCleanup(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), testNamespace)
		})
		By("creating a Kuadrant resource so pipeline updates can trigger reconciliation")
		tests.ApplyKuadrantCR(ctx, testClient(), testNamespace)

		By("deploying an HTTPBin backend")
		backendLabels := map[string]string{"app": "ocsf-event-backend"}
		backend := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ocsf-event-backend", Namespace: testNamespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{MatchLabels: backendLabels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: backendLabels},
					Spec: corev1.PodSpec{
						SecurityContext: &corev1.PodSecurityContext{
							RunAsNonRoot:   ptr.To(true),
							SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						},
						Containers: []corev1.Container{{
							Name:            "httpbin",
							Image:           ocsfHTTPBinImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                ptr.To[int64](1000),
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
							Ports:        []corev1.ContainerPort{{Name: "http", ContainerPort: 80}},
							VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
						}},
						Volumes: []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
					},
				},
			},
		}
		Expect(testClient().Create(ctx, backend)).To(Succeed())
		backendService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ocsf-event-backend", Namespace: testNamespace},
			Spec: corev1.ServiceSpec{
				Selector: backendLabels,
				Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(80)}},
			},
		}
		Expect(testClient().Create(ctx, backendService)).To(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(backend), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(BeNumerically(">=", current.Generation))
			g.Expect(current.Status.ReadyReplicas).To(Equal(int32(1)))
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		By("creating an Istio Gateway and a route that rewrites to HTTPBin /get")
		const hostname = "ocsf-event.e2e.test"
		gateway := tests.NewGatewayBuilder("ocsf-event-gateway", tests.GatewayClassName, testNamespace).
			WithHTTPListener("http", hostname).Gateway
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).
			WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())

		route := tests.BuildBasicHttpRoute("ocsf-event-route", gateway.Name, testNamespace, []string{hostname})
		route.Spec.Rules[0].Matches = []gatewayapiv1.HTTPRouteMatch{{
			Path: &gatewayapiv1.HTTPPathMatch{
				Type:  ptr.To(gatewayapiv1.PathMatchExact),
				Value: ptr.To("/tenant/model/get"),
			},
		}}
		route.Spec.Rules[0].Filters = []gatewayapiv1.HTTPRouteFilter{{
			Type: gatewayapiv1.HTTPRouteFilterType("URLRewrite"),
			URLRewrite: &gatewayapiv1.HTTPURLRewriteFilter{
				Path: &gatewayapiv1.HTTPPathModifier{
					Type:            gatewayapiv1.HTTPPathModifierType("ReplaceFullPath"),
					ReplaceFullPath: ptr.To("/get"),
				},
			},
		}}
		route.Spec.Rules[0].BackendRefs = []gatewayapiv1.HTTPBackendRef{{
			BackendRef: gatewayapiv1.BackendRef{
				BackendObjectReference: gatewayapiv1.BackendObjectReference{
					Name: gatewayapiv1.ObjectName(backendService.Name),
					Port: ptr.To(gatewayapiv1.PortNumber(80)),
				},
			},
		}}
		Expect(testClient().Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).
			WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())

		By("attaching an AuthPolicy that supplies the OCSF actor identity")
		authPolicy := ocsfAuthPolicy(testNamespace, gateway.Name)
		Expect(testClient().Create(ctx, authPolicy)).To(Succeed())
		Eventually(func() error {
			return ocsfAuthPolicyEnforcementStatus(ctx, testClient(), client.ObjectKeyFromObject(authPolicy))
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		By("attaching an OCSF aiOperation TelemetryPolicy to the Gateway")
		policy := ocsfTelemetryPolicy(testNamespace, gateway.Name)
		Expect(testClient().Create(ctx, policy)).To(Succeed())
		Eventually(func() error {
			return ocsfTelemetryPolicyEnforcementStatus(ctx, testClient(), client.ObjectKeyFromObject(policy))
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		By("waiting for the generated EnvoyFilter with the OCSF access logger")
		envoyFilterKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.Name), Namespace: testNamespace}
		envoyFilterGVK := schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}
		Eventually(func(g Gomega) {
			envoyFilter := &unstructured.Unstructured{}
			envoyFilter.SetGroupVersionKind(envoyFilterGVK)
			g.Expect(testClient().Get(ctx, envoyFilterKey, envoyFilter)).To(Succeed())

			configPatches, found, err := unstructured.NestedSlice(envoyFilter.Object, "spec", "configPatches")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			hasAccessLogPatch := false
			hasExportedOCSFResourceTypeAction := false
			for _, configPatch := range configPatches {
				configPatchMap, ok := configPatch.(map[string]any)
				if !ok {
					continue
				}
				patchValue, found, err := unstructured.NestedMap(configPatchMap, "patch", "value")
				if err != nil || !found {
					continue
				}
				typedConfig, found, err := unstructured.NestedMap(patchValue, "typed_config")
				if err != nil || !found {
					continue
				}
				accessLogs, found, err := unstructured.NestedSlice(typedConfig, "access_log")
				if err == nil && found && len(accessLogs) > 0 {
					hasAccessLogPatch = true
				}
				pluginConfigJSON, found, err := unstructured.NestedString(typedConfig, "value", "config", "configuration", "value")
				if err == nil && found && hasExportedOCSFResourceTypeStoreAction(pluginConfigJSON) {
					hasExportedOCSFResourceTypeAction = true
				}
			}
			g.Expect(hasAccessLogPatch).To(BeTrue(), "EnvoyFilter %s does not yet contain an access-log patch", envoyFilterKey)
			g.Expect(hasExportedOCSFResourceTypeAction).To(BeTrue(),
				"EnvoyFilter %s does not yet contain an exported ocsf.resource.type WASM store action", envoyFilterKey)
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		By("port-forwarding to the Gateway's ready Envoy pod")
		serviceName := gateway.Name + "-istio"
		var gatewayPod *corev1.Pod
		Eventually(func(g Gomega) {
			service := &corev1.Service{}
			g.Expect(testClient().Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: serviceName}, service)).To(Succeed())
			g.Expect(service.Spec.Selector).NotTo(BeEmpty())
			pods := &corev1.PodList{}
			g.Expect(testClient().List(ctx, pods, client.InNamespace(testNamespace), client.MatchingLabels(service.Spec.Selector))).To(Succeed())
			var readyPod *corev1.Pod
			for i := range pods.Items {
				if podIsReady(&pods.Items[i]) {
					readyPod = pods.Items[i].DeepCopy()
					break
				}
			}
			g.Expect(readyPod).NotTo(BeNil())
			gatewayPod = readyPod
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())
		GinkgoWriter.Printf("OCSF E2E gateway pod: %s/%s (uid=%s)\n", gatewayPod.Namespace, gatewayPod.Name, gatewayPod.UID)

		By("installing an isolated diagnostic logger for the OCSF resource marker")
		diagnosticEnvoyFilter := ocsfMarkerDiagnosticEnvoyFilter(testNamespace, gatewayPod.Labels)
		Expect(testClient().Create(ctx, diagnosticEnvoyFilter)).To(Succeed())

		By("waiting for the OCSF access logger in Envoy's active config")
		var activeConfigSummary string
		Eventually(func(g Gomega) {
			configDump, err := ocsfEnvoyConfigDump(ctx, testNamespace, gatewayPod.Name)
			activeConfigSummary = summarizeOCSFEnvoyConfigDump(configDump)
			g.Expect(err).NotTo(HaveOccurred(), "Envoy config dump result: %s", activeConfigSummary)
			if err != nil {
				return
			}
			g.Expect(bytes.Contains(configDump, []byte(ocsfAccessLoggerName))).To(BeTrue(),
				"Envoy active config does not include the OCSF access logger: %s", activeConfigSummary)
			g.Expect(bytes.Contains(configDump, []byte(ocsfResourceTypeKey))).To(BeTrue(),
				"Envoy active config does not include the OCSF resource marker: %s", activeConfigSummary)
			g.Expect(hasExportedOCSFResourceTypeStoreActionInEnvoyConfigDump(configDump)).To(BeTrue(),
				"Envoy active config does not include the exported OCSF WASM store action: %s", activeConfigSummary)
			g.Expect(bytes.Contains(configDump, []byte(ocsfMarkerDiagnosticKey))).To(BeTrue(),
				"Envoy active config does not include the test-only OCSF marker diagnostic logger: %s", activeConfigSummary)
		}).WithContext(ctx).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

		localPort, stopPortForward, checkPortForward, err := ocsfPortForward(ctx, testNamespace, "pod/"+gatewayPod.Name, 80)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stopPortForward)

		By("sending one request containing values that must not appear in OCSF")
		const querySentinel = "ocsf-query-secret-sentinel"
		const credentialSentinel = "ocsf-bearer-secret-sentinel"
		httpClient := &http.Client{Timeout: ocsfE2EHTTPTimeout}
		requestStarted := time.Now().Add(-time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/tenant/model/get?sentinel=%s", localPort, querySentinel), nil)
		Expect(err).NotTo(HaveOccurred())
		if err != nil {
			return
		}
		request.Host = hostname
		request.Header.Set("Authorization", "Bearer "+credentialSentinel)
		request.Header.Set("X-OCSF-User", "ocsf-test-user")
		response, err := httpClient.Do(request)
		if err != nil {
			diagnostics := ocsfGatewayDiagnostics(ctx, testNamespace, gatewayPod.Name)
			Fail(fmt.Sprintf("request through the Gateway failed: %v\n%s", err, diagnostics))
			return
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(response.Body.Close()).To(Succeed())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		By("waiting for the OCSF event in the Gateway access log")
		var event map[string]any
		var accessLogs []byte
		Eventually(func(g Gomega) {
			if err := checkPortForward(); err != nil {
				diagnostics := ocsfGatewayDiagnostics(ctx, testNamespace, gatewayPod.Name)
				StopTrying("kubectl port-forward stopped while polling for the OCSF event").
					Wrap(err).
					Attach("gateway pod diagnostics", diagnostics).
					Now()
			}
			logCommand := exec.CommandContext(ctx, "kubectl", "-n", testNamespace, "logs", gatewayPod.Name,
				"-c", "istio-proxy", "--since-time="+requestStarted.UTC().Format(time.RFC3339Nano), "--tail=100")
			accessLogs, err = logCommand.CombinedOutput()
			g.Expect(err).NotTo(HaveOccurred())
			if err != nil {
				return
			}
			event = findOCSFEvent(accessLogs, "GET /tenant/model/get")
			markerDiagnostic := findOCSFMarkerDiagnostic(accessLogs)
			markerSummary := "not observed"
			if markerDiagnostic != nil {
				markerValue, _ := markerDiagnostic["resource_type"].(string)
				markerSummary = fmt.Sprintf("method=%v resource_type=%q", markerDiagnostic["method"], markerValue)
			}
			g.Expect(event).NotTo(BeNil(),
				"recent istio-proxy log summary:\n%s\nOCSF marker diagnostic: %s",
				summarizeOCSFProxyLogs(accessLogs), markerSummary)
		}).WithContext(ctx).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

		Expect(event["class_uid"]).To(Equal(float64(6003)))
		Expect(event["class_name"]).To(Equal("API Activity"))
		Expect(event["type_uid"]).To(Equal(float64(600399)))
		Expect(event["activity_id"]).To(Equal(float64(99)))
		metadata := ocsfEventObject(event, "metadata")
		Expect(metadata["version"]).To(Equal("1.9.0"))
		Expect(metadata["profiles"]).To(Equal([]any{"ai_operation"}))
		Expect(metadata["logged_time"]).To(BeNumerically(">", 1_700_000_000_000))
		Expect(metadata["logged_time"]).To(BeNumerically("<", 2_000_000_000_000))
		Expect(event["time"]).To(BeNumerically(">", 1_700_000_000_000))
		Expect(event["time"]).To(BeNumerically("<", 2_000_000_000_000))
		Expect(metadata["logged_time"]).To(BeNumerically(">=", event["time"]))

		actor := ocsfEventObject(ocsfEventObject(event, "actor"), "user")
		Expect(actor["name"]).To(Equal("ocsf-test-user"))
		Expect(actor["type_id"]).To(Equal(float64(1)))
		Expect(actor["uid"]).To(BeNil(), "an absent optional filter-state value must not break the event")
		Expect(actor["groups"]).To(Equal([]any{map[string]any{"name": "ocsf-test-group"}}),
			"the typed formatter must preserve nested list/map values exported by wasm-shim")
		api := ocsfEventObject(event, "api")
		Expect(api["operation"]).To(Equal("GET /tenant/model/get"))
		httpRequest := ocsfEventObject(event, "http_request")
		Expect(httpRequest["http_method"]).To(Equal("GET"))
		Expect(ocsfEventObject(httpRequest, "url")["path"]).To(Equal("/tenant/model/get"))
		httpResponse := ocsfEventObject(event, "http_response")
		Expect(httpResponse["code"]).To(Equal(float64(http.StatusOK)))
		Expect(event["status"]).To(Equal("Success"))
		Expect(event["status_code"]).To(Equal(float64(http.StatusOK)))

		resources, ok := event["resources"].([]any)
		Expect(ok).To(BeTrue())
		Expect(resources).To(HaveLen(1))
		resource, ok := resources[0].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(resource["type"]).To(Equal("model"))
		Expect(resource["namespace"]).To(Equal("tenant"))
		Expect(resource["name"]).To(Equal("model"))
		aiModel := ocsfEventObject(event, "ai_model")
		Expect(aiModel["name"]).To(Equal("model"))
		Expect(aiModel["ai_provider"]).To(Equal("test-provider"))

		eventJSON, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(eventJSON)).NotTo(ContainSubstring(querySentinel))
		Expect(string(eventJSON)).NotTo(ContainSubstring(credentialSentinel))
		Expect(string(eventJSON)).NotTo(ContainSubstring("Bearer " + credentialSentinel))
		for _, artifact := range []string{
			"Gg5vY3NmLXRlc3QtdXNlcg==",
			"string_value",
			"number_value",
			"bool_value",
			"list_value",
			"struct_value",
			"bytes_value",
		} {
			Expect(string(eventJSON)).NotTo(ContainSubstring(artifact), "OCSF event must not contain base64/protobuf artifacts")
		}
	})
})

var _ = Describe("WASM runtime control", Label("wasm-control", "long-running"), func() {
	It("keeps a non-OCSF TokenRateLimitPolicy WASM filter running", func(ctx SpecContext) {
		if os.Getenv("WASM_CONTROL_E2E") != "true" {
			Skip("WASM_CONTROL_E2E is not true; use make test-wasm-shim-control-e2e")
		}

		testNamespace := tests.CreateNamespace(ctx, testClient())
		GinkgoWriter.Printf("WASM control E2E test namespace: %s\n", testNamespace)
		DeferCleanup(func(ctx SpecContext) {
			tests.DeleteNamespace(ctx, testClient(), testNamespace)
		})

		By("creating a Kuadrant resource so policy updates can trigger reconciliation")
		tests.ApplyKuadrantCR(ctx, testClient(), testNamespace)

		By("deploying an HTTPBin backend")
		backendLabels := map[string]string{"app": "wasm-control-backend"}
		backend := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-control-backend", Namespace: testNamespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{MatchLabels: backendLabels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: backendLabels},
					Spec: corev1.PodSpec{
						SecurityContext: &corev1.PodSecurityContext{
							RunAsNonRoot:   ptr.To(true),
							SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						},
						Containers: []corev1.Container{{
							Name:            "httpbin",
							Image:           ocsfHTTPBinImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                ptr.To[int64](1000),
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
							Ports:        []corev1.ContainerPort{{Name: "http", ContainerPort: 80}},
							VolumeMounts: []corev1.VolumeMount{{Name: "tmp", MountPath: "/tmp"}},
						}},
						Volumes: []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
					},
				},
			},
		}
		Expect(testClient().Create(ctx, backend)).To(Succeed())
		backendService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-control-backend", Namespace: testNamespace},
			Spec: corev1.ServiceSpec{
				Selector: backendLabels,
				Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(80)}},
			},
		}
		Expect(testClient().Create(ctx, backendService)).To(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(backend), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(BeNumerically(">=", current.Generation))
			g.Expect(current.Status.ReadyReplicas).To(Equal(int32(1)))
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		By("creating an Istio Gateway and HTTPRoute")
		const hostname = "wasm-control.e2e.test"
		gateway := tests.NewGatewayBuilder("wasm-control-gateway", tests.GatewayClassName, testNamespace).
			WithHTTPListener("http", hostname).Gateway
		Expect(testClient().Create(ctx, gateway)).To(Succeed())
		Eventually(tests.GatewayIsReady(ctx, testClient(), gateway)).WithContext(ctx).
			WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())

		route := tests.BuildBasicHttpRoute("wasm-control-route", gateway.Name, testNamespace, []string{hostname})
		route.Spec.Rules[0].Matches = []gatewayapiv1.HTTPRouteMatch{{
			Path: &gatewayapiv1.HTTPPathMatch{
				Type:  ptr.To(gatewayapiv1.PathMatchExact),
				Value: ptr.To("/get"),
			},
		}}
		route.Spec.Rules[0].BackendRefs = []gatewayapiv1.HTTPBackendRef{{
			BackendRef: gatewayapiv1.BackendRef{
				BackendObjectReference: gatewayapiv1.BackendObjectReference{
					Name: gatewayapiv1.ObjectName(backendService.Name),
					Port: ptr.To(gatewayapiv1.PortNumber(80)),
				},
			},
		}}
		Expect(testClient().Create(ctx, route)).To(Succeed())
		Eventually(tests.RouteIsAccepted(ctx, testClient(), client.ObjectKeyFromObject(route))).
			WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())

		By("recording the ready Gateway proxy baseline before attaching a WASM policy")
		serviceName := gateway.Name + "-istio"
		var gatewayPod *corev1.Pod
		Eventually(func(g Gomega) {
			service := &corev1.Service{}
			g.Expect(testClient().Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: serviceName}, service)).To(Succeed())
			g.Expect(service.Spec.Selector).NotTo(BeEmpty())
			pods := &corev1.PodList{}
			g.Expect(testClient().List(ctx, pods, client.InNamespace(testNamespace), client.MatchingLabels(service.Spec.Selector))).To(Succeed())
			var readyPod *corev1.Pod
			for i := range pods.Items {
				if podIsReady(&pods.Items[i]) {
					readyPod = pods.Items[i].DeepCopy()
					break
				}
			}
			g.Expect(readyPod).NotTo(BeNil())
			gatewayPod = readyPod
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())
		Expect(gatewayPod).NotTo(BeNil())
		if gatewayPod == nil {
			return
		}

		initialProxyRestarts := int32(-1)
		for _, status := range gatewayPod.Status.ContainerStatuses {
			if status.Name == "istio-proxy" {
				initialProxyRestarts = status.RestartCount
				GinkgoWriter.Printf("WASM control baseline proxy image: %s (%s), restarts=%d\n", status.Image, status.ImageID, initialProxyRestarts)
				break
			}
		}
		Expect(initialProxyRestarts).NotTo(Equal(int32(-1)), "ready Gateway pod did not report the istio-proxy container status")
		initialGatewayPodUID := gatewayPod.UID
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Printf("WASM control Gateway diagnostics:\n%s", ocsfGatewayDiagnostics(ctx, testNamespace, gatewayPod.Name))
			}
		})

		By("attaching a TokenRateLimitPolicy to generate the shared WASM shim without OCSF logging")
		policy := &kuadrantv1alpha1.TokenRateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "TokenRateLimitPolicy", APIVersion: kuadrantv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-control-policy", Namespace: testNamespace},
			Spec: kuadrantv1alpha1.TokenRateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: gatewayapiv1.GroupName,
						Kind:  "HTTPRoute",
						Name:  gatewayapiv1.ObjectName(route.Name),
					},
				},
				TokenRateLimitPolicySpecProper: kuadrantv1alpha1.TokenRateLimitPolicySpecProper{
					Limits: map[string]kuadrantv1alpha1.TokenLimit{
						"control": {
							Rates: []kuadrantv1.Rate{{Limit: 5000, Window: kuadrantv1.Duration("1m")}},
						},
					},
				},
			},
		}
		Expect(testClient().Create(ctx, policy)).To(Succeed())
		policyKey := client.ObjectKeyFromObject(policy)
		Eventually(tests.TokenRateLimitPolicyIsAccepted(ctx, testClient(), policyKey)).
			WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())
		Eventually(tests.TokenRateLimitPolicyIsEnforced(ctx, testClient(), policyKey)).
			WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(BeTrue())

		By("verifying the generated EnvoyFilter uses the WASM shim module with no OCSF config")
		envoyFilterKey := client.ObjectKey{Name: wasm.ExtensionName(gateway.Name), Namespace: testNamespace}
		envoyFilterGVK := schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}
		var envoyFilter *unstructured.Unstructured
		Eventually(func(g Gomega) {
			envoyFilter = &unstructured.Unstructured{}
			envoyFilter.SetGroupVersionKind(envoyFilterGVK)
			g.Expect(testClient().Get(ctx, envoyFilterKey, envoyFilter)).To(Succeed())
		}).WithContext(ctx).WithTimeout(ocsfE2EReadyTimeout).Should(Succeed())

		runtime, moduleURL, moduleSHA, pluginConfig, err := wasmShimIdentityFromEnvoyFilter(envoyFilter)
		Expect(err).NotTo(HaveOccurred())
		Expect(runtime).To(Equal("envoy.wasm.runtime.v8"))
		Expect(moduleURL).To(ContainSubstring("/plugin.wasm"))
		Expect(moduleSHA).To(MatchRegexp("^[a-f0-9]{64}$"))
		Expect(strings.ToLower(pluginConfig)).NotTo(ContainSubstring("ocsf"))
		filterJSON, err := json.Marshal(envoyFilter.Object)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.ToLower(string(filterJSON))).NotTo(ContainSubstring("ocsf"))

		GinkgoWriter.Printf("WASM control module: url=%s sha256=%s runtime=%s\n", moduleURL, moduleSHA, runtime)

		By("waiting for the WASM shim in Envoy's active config")
		Eventually(func(g Gomega) {
			configDump, err := ocsfEnvoyConfigDump(ctx, testNamespace, gatewayPod.Name)
			g.Expect(err).NotTo(HaveOccurred())
			if err != nil {
				return
			}
			g.Expect(bytes.Contains(configDump, []byte("kuadrant-wasm-shim"))).To(BeTrue())
			g.Expect(bytes.Contains(configDump, []byte(moduleSHA))).To(BeTrue())
		}).WithContext(ctx).WithTimeout(90 * time.Second).WithPolling(time.Second).Should(Succeed())

		currentGatewayPod := &corev1.Pod{}
		Expect(testClient().Get(ctx, client.ObjectKeyFromObject(gatewayPod), currentGatewayPod)).To(Succeed())
		Expect(currentGatewayPod.UID).To(Equal(initialGatewayPodUID), "Gateway pod was replaced while applying the non-OCSF WASM filter")
		proxyRestarts := int32(-1)
		for _, status := range currentGatewayPod.Status.ContainerStatuses {
			if status.Name == "istio-proxy" {
				proxyRestarts = status.RestartCount
				break
			}
		}
		Expect(proxyRestarts).NotTo(Equal(int32(-1)), "Gateway pod did not report the istio-proxy container status after applying the filter")
		Expect(proxyRestarts).To(Equal(initialProxyRestarts), "istio-proxy restarted after the non-OCSF WASM filter was attached")

		By("sending a request through the Gateway with the non-OCSF WASM filter active")
		localPort, stopPortForward, checkPortForward, err := ocsfPortForward(ctx, testNamespace, "pod/"+gatewayPod.Name, 80)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stopPortForward)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/get", localPort), nil)
		Expect(err).NotTo(HaveOccurred())
		request.Host = hostname
		response, err := (&http.Client{Timeout: ocsfE2EHTTPTimeout}).Do(request)
		if err != nil {
			diagnostics := ocsfGatewayDiagnostics(ctx, testNamespace, gatewayPod.Name)
			Fail(fmt.Sprintf("request through the non-OCSF WASM control Gateway failed: %v\n%s", err, diagnostics))
			return
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(response.Body.Close()).To(Succeed())
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		Expect(checkPortForward()).To(Succeed())

		By("confirming the proxy remains stable after loading the shared WASM module")
		Consistently(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(testClient().Get(ctx, client.ObjectKeyFromObject(gatewayPod), current)).To(Succeed())
			g.Expect(current.UID).To(Equal(gatewayPod.UID))
			g.Expect(podIsReady(current)).To(BeTrue())
			proxyRestarts := int32(-1)
			for _, status := range current.Status.ContainerStatuses {
				if status.Name == "istio-proxy" {
					proxyRestarts = status.RestartCount
					break
				}
			}
			g.Expect(proxyRestarts).To(Equal(initialProxyRestarts))
		}).WithContext(ctx).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
	})
})

func wasmShimIdentityFromEnvoyFilter(envoyFilter *unstructured.Unstructured) (runtime, moduleURL, moduleSHA, pluginConfig string, err error) {
	configPatches, found, err := unstructured.NestedSlice(envoyFilter.Object, "spec", "configPatches")
	if err != nil {
		return "", "", "", "", err
	}
	if !found {
		return "", "", "", "", fmt.Errorf("EnvoyFilter has no configPatches")
	}

	for _, rawPatch := range configPatches {
		patch, ok := rawPatch.(map[string]any)
		if !ok {
			continue
		}
		patchValue, found, err := unstructured.NestedMap(patch, "patch", "value")
		if err != nil || !found {
			continue
		}
		typedConfig, found, err := unstructured.NestedMap(patchValue, "typed_config")
		if err != nil || !found {
			continue
		}
		wasmConfig, found, err := unstructured.NestedMap(typedConfig, "value", "config")
		if err != nil || !found {
			continue
		}
		name, _, _ := unstructured.NestedString(wasmConfig, "name")
		if name != "kuadrant-wasm-shim" {
			continue
		}
		vmConfig, found, err := unstructured.NestedMap(wasmConfig, "vm_config")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no vm_config")
		}
		runtime, found, err = unstructured.NestedString(vmConfig, "runtime")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no runtime")
		}
		remote, found, err := unstructured.NestedMap(vmConfig, "code", "remote")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no remote code")
		}
		moduleSHA, found, err = unstructured.NestedString(remote, "sha256")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no module sha256")
		}
		httpURI, found, err := unstructured.NestedMap(remote, "http_uri")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no HTTP module URI")
		}
		moduleURL, found, err = unstructured.NestedString(httpURI, "uri")
		if err != nil || !found {
			return "", "", "", "", fmt.Errorf("WASM shim config has no module URL")
		}
		pluginConfig, _, _ = unstructured.NestedString(wasmConfig, "configuration", "value")
		return runtime, moduleURL, moduleSHA, pluginConfig, nil
	}

	return "", "", "", "", fmt.Errorf("EnvoyFilter contains no kuadrant-wasm-shim HTTP filter")
}

func ocsfAuthPolicy(namespace, gatewayName string) *unstructured.Unstructured {
	policy := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kuadrant.io/v1",
		"kind":       "AuthPolicy",
		"metadata": map[string]any{
			"name":      "ocsf-event-auth-policy",
			"namespace": namespace,
		},
		"spec": map[string]any{
			"targetRef": map[string]any{
				"group": "gateway.networking.k8s.io",
				"kind":  "Gateway",
				"name":  gatewayName,
			},
			"rules": map[string]any{
				"authentication": map[string]any{
					"test-identity": map[string]any{
						"plain": map[string]any{
							"expression": "{'username': request.headers['x-ocsf-user'], 'groups': ['ocsf-test-group']}",
						},
					},
				},
				"response": map[string]any{
					"success": map[string]any{
						"filters": map[string]any{
							"identity": map[string]any{
								"json": map[string]any{
									"properties": map[string]any{
										"username": map[string]any{
											"selector": "auth.identity.username",
										},
										"groups": map[string]any{
											"selector": "auth.identity.groups",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}}
	policy.SetGroupVersionKind(schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"})
	return policy
}

func ocsfAuthPolicyEnforcementStatus(ctx context.Context, c client.Client, key client.ObjectKey) error {
	policy := &unstructured.Unstructured{}
	policy.SetGroupVersionKind(schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"})
	if err := c.Get(ctx, key, policy); err != nil {
		return fmt.Errorf("get AuthPolicy %s: %w", key, err)
	}

	generation := policy.GetGeneration()
	observedGeneration, hasObservedGeneration, err := unstructured.NestedInt64(policy.Object, "status", "observedGeneration")
	if err != nil {
		return fmt.Errorf("read AuthPolicy %s observedGeneration: %w", key, err)
	}
	conditions, hasConditions, err := unstructured.NestedSlice(policy.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("read AuthPolicy %s conditions: %w", key, err)
	}
	if hasObservedGeneration && hasConditions && observedGeneration >= generation {
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]any)
			if ok && condition["type"] == "Enforced" && condition["status"] == "True" {
				return nil
			}
		}
	}
	return fmt.Errorf("AuthPolicy %s is not Enforced=True (generation=%d observedGeneration=%v present=%t conditions=%v present=%t)",
		key, generation, observedGeneration, hasObservedGeneration, conditions, hasConditions)
}

func ocsfTelemetryPolicy(namespace, gatewayName string) *unstructured.Unstructured {
	policy := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "extensions.kuadrant.io/v1alpha1",
		"kind":       "TelemetryPolicy",
		"metadata": map[string]any{
			"name":      "ocsf-event-policy",
			"namespace": namespace,
		},
		"spec": map[string]any{
			"targetRef": map[string]any{
				"group": "gateway.networking.k8s.io",
				"kind":  "Gateway",
				"name":  gatewayName,
			},
			"logging": map[string]any{
				"default": map[string]any{
					"ocsf": map[string]any{
						"profile": "aiOperation",
						"actor": map[string]any{
							"identity": "auth.identity.username",
							"groups":   "auth.identity.groups",
						},
						"target": map[string]any{
							"resourceType": "model",
							"requestPath": map[string]any{
								"namespaceSegment": int64(0),
								"nameSegment":      int64(1),
							},
							"aiProvider": "test-provider",
						},
					},
				},
			},
		},
	}}
	policy.SetGroupVersionKind(schema.GroupVersionKind{Group: "extensions.kuadrant.io", Version: "v1alpha1", Kind: "TelemetryPolicy"})
	return policy
}

// ocsfMarkerDiagnosticEnvoyFilter logs only the request method and OCSF resource-type marker; it must not log paths, headers, or bodies.
func ocsfMarkerDiagnosticEnvoyFilter(namespace string, workloadLabels map[string]string) *unstructured.Unstructured {
	labels := make(map[string]any, len(workloadLabels))
	for key, value := range workloadLabels {
		labels[key] = value
	}

	envoyFilter := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.istio.io/v1alpha3",
		"kind":       "EnvoyFilter",
		"metadata": map[string]any{
			"name":      "ocsf-marker-diagnostic",
			"namespace": namespace,
		},
		"spec": map[string]any{
			"workloadSelector": map[string]any{"labels": labels},
			"configPatches": []any{
				map[string]any{
					"applyTo": "NETWORK_FILTER",
					"match": map[string]any{
						"context": "GATEWAY",
						"listener": map[string]any{
							"filterChain": map[string]any{
								"filter": map[string]any{
									"name": "envoy.filters.network.http_connection_manager",
								},
							},
						},
					},
					"patch": map[string]any{
						"operation": "MERGE",
						"value": map[string]any{
							"name": "envoy.filters.network.http_connection_manager",
							"typed_config": map[string]any{
								"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
								"access_log": []any{
									map[string]any{
										"name": ocsfAccessLoggerName,
										"typed_config": map[string]any{
											"@type": "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog",
											"log_format": map[string]any{
												"json_format": map[string]any{
													ocsfMarkerDiagnosticKey: true,
													"method":                "%REQ(:METHOD)%",
													"resource_type":         fmt.Sprintf("%%FILTER_STATE(%s:TYPED)%%", ocsfResourceTypeKey),
												},
												"omit_empty_values": false,
												"formatters": []any{
													map[string]any{
														"name": "envoy.formatter.cel",
														"typed_config": map[string]any{
															"@type": "type.googleapis.com/envoy.extensions.formatter.cel.v3.Cel",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}}
	envoyFilter.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter",
	})
	return envoyFilter
}

func ocsfTelemetryPolicyEnforcementStatus(ctx context.Context, c client.Client, key client.ObjectKey) error {
	policy := &unstructured.Unstructured{}
	policy.SetGroupVersionKind(schema.GroupVersionKind{Group: "extensions.kuadrant.io", Version: "v1alpha1", Kind: "TelemetryPolicy"})
	if err := c.Get(ctx, key, policy); err != nil {
		return fmt.Errorf("get TelemetryPolicy %s: %w", key, err)
	}

	generation := policy.GetGeneration()
	observedGeneration, hasObservedGeneration, err := unstructured.NestedInt64(policy.Object, "status", "observedGeneration")
	if err != nil {
		return fmt.Errorf("read TelemetryPolicy %s observedGeneration: %w", key, err)
	}
	conditions, hasConditions, err := unstructured.NestedSlice(policy.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("read TelemetryPolicy %s conditions: %w", key, err)
	}
	if hasObservedGeneration && hasConditions && observedGeneration >= generation {
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]any)
			if ok && condition["type"] == "Enforced" && condition["status"] == "True" {
				return nil
			}
		}
	}

	return fmt.Errorf(
		"TelemetryPolicy %s is not Enforced=True (generation=%d observedGeneration=%v present=%t conditions=%v present=%t)",
		key, generation, observedGeneration, hasObservedGeneration, conditions, hasConditions,
	)
}

func podIsReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func ocsfPortForward(ctx context.Context, namespace, target string, remotePort int32) (uint16, func(), func() error, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, nil, fmt.Errorf("find an available local port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, nil, nil, fmt.Errorf("release local port: %w", err)
	}

	command := exec.CommandContext(ctx, "kubectl", "-n", namespace, "port-forward", "--address=127.0.0.1", target,
		fmt.Sprintf("%d:%d", localPort, remotePort))
	stdout, err := command.StdoutPipe()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("capture kubectl port-forward output: %w", err)
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("capture kubectl port-forward errors: %w", err)
	}
	var stderr bytes.Buffer
	if err := command.Start(); err != nil {
		return 0, nil, nil, fmt.Errorf("start kubectl port-forward: %w", err)
	}
	forwardReady := make(chan struct{}, 1)
	forwardReadyLine := fmt.Sprintf("Forwarding from 127.0.0.1:%d -> %d", localPort, remotePort)
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), forwardReadyLine) {
				select {
				case forwardReady <- struct{}{}:
				default:
				}
			}
		}
	}()
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderr, stderrPipe)
	}()
	commandDone := make(chan struct{})
	var commandErr error
	go func() {
		<-stdoutDone
		<-stderrDone
		commandErr = command.Wait()
		close(commandDone)
	}()
	checkPortForward := func() error {
		select {
		case <-commandDone:
			return fmt.Errorf("kubectl port-forward exited unexpectedly: %v: %s", commandErr, stderr.String())
		default:
			return nil
		}
	}
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-forwardReady:
			if err := checkPortForward(); err != nil {
				return 0, nil, nil, err
			}
			stop := func() {
				select {
				case <-commandDone:
				default:
					if command.Process != nil {
						_ = command.Process.Kill()
					}
					select {
					case <-commandDone:
					case <-time.After(5 * time.Second):
					}
				}
			}
			return uint16(localPort), stop, checkPortForward, nil
		case <-commandDone:
			return 0, nil, nil, fmt.Errorf("kubectl port-forward exited before becoming ready: %v: %s", commandErr, stderr.String())
		case <-ctx.Done():
			_ = command.Process.Kill()
			select {
			case <-commandDone:
			case <-time.After(5 * time.Second):
			}
			return 0, nil, nil, fmt.Errorf("kubectl port-forward: %w", ctx.Err())
		case <-timeout.C:
			_ = command.Process.Kill()
			select {
			case <-commandDone:
			case <-time.After(5 * time.Second):
			}
			return 0, nil, nil, fmt.Errorf("timed out starting kubectl port-forward: %s", stderr.String())
		}
	}
}

func ocsfGatewayDiagnostics(ctx context.Context, namespace, podName string) string {
	var output strings.Builder
	commands := [][]string{
		{"get", "pod", podName, "-o", `jsonpath={range .status.containerStatuses[*]}{.name}:{.restartCount}:{.state}:{.lastState}{"\n"}{end}`},
		{"get", "events", "--field-selector", "involvedObject.name=" + podName, "--sort-by=.lastTimestamp"},
		{"exec", podName, "-c", "istio-proxy", "--", "pilot-agent", "request", "GET", "/config_dump"},
		{"logs", podName, "-c", "istio-proxy", "--previous", "--tail=100"},
	}
	for _, args := range commands {
		fmt.Fprintf(&output, "\n$ kubectl -n %s %s\n", namespace, strings.Join(args, " "))
		commandArgs := append([]string{"-n", namespace}, args...)
		command := exec.CommandContext(ctx, "kubectl", commandArgs...)
		commandOutput, err := command.CombinedOutput()
		if args[0] == "exec" {
			commandOutput = []byte(summarizeOCSFEnvoyConfigDump(commandOutput))
		} else if args[0] == "logs" {
			commandOutput = []byte(summarizeOCSFProxyLogs(commandOutput))
		}
		output.Write(commandOutput)
		if err != nil {
			fmt.Fprintf(&output, "command error: %v\n", err)
		}
	}
	return output.String()
}

func ocsfEnvoyConfigDump(ctx context.Context, namespace, podName string) ([]byte, error) {
	command := exec.CommandContext(ctx, "kubectl", "-n", namespace, "exec", podName,
		"-c", "istio-proxy", "--", "pilot-agent", "request", "GET", "/config_dump")
	return command.CombinedOutput()
}

func summarizeOCSFEnvoyConfigDump(configDump []byte) string {
	return fmt.Sprintf("config dump bytes=%d; OCSF access logger present=%t; OCSF resource marker present=%t; exported OCSF WASM store action present=%t",
		len(configDump), bytes.Contains(configDump, []byte(ocsfAccessLoggerName)), bytes.Contains(configDump, []byte(ocsfResourceTypeKey)),
		hasExportedOCSFResourceTypeStoreActionInEnvoyConfigDump(configDump))
}

func hasExportedOCSFResourceTypeStoreAction(pluginConfigJSON string) bool {
	var pluginConfig struct {
		ActionSets []struct {
			Actions []struct {
				Type         string `json:"type"`
				Path         string `json:"path"`
				ExportToHost bool   `json:"exportToHost"`
			} `json:"actions"`
		} `json:"actionSets"`
	}
	if err := json.Unmarshal([]byte(pluginConfigJSON), &pluginConfig); err != nil {
		return false
	}
	for _, actionSet := range pluginConfig.ActionSets {
		for _, action := range actionSet.Actions {
			if action.Type == "store" && action.Path == strings.TrimPrefix(ocsfResourceTypeKey, "wasm.kuadrant.") && action.ExportToHost {
				return true
			}
		}
	}
	return false
}

func hasExportedOCSFResourceTypeStoreActionInEnvoyConfigDump(configDump []byte) bool {
	var envoyConfig any
	if err := json.Unmarshal(configDump, &envoyConfig); err != nil {
		return false
	}

	var containsAction func(any) bool
	containsAction = func(value any) bool {
		switch typed := value.(type) {
		case map[string]any:
			if typed["name"] == "kuadrant-wasm-shim" {
				configuration, ok := typed["configuration"].(map[string]any)
				if ok {
					pluginConfigJSON, ok := configuration["value"].(string)
					if ok && hasExportedOCSFResourceTypeStoreAction(pluginConfigJSON) {
						return true
					}
				}
			}
			for _, nested := range typed {
				if containsAction(nested) {
					return true
				}
			}
		case []any:
			for _, nested := range typed {
				if containsAction(nested) {
					return true
				}
			}
		}
		return false
	}
	return containsAction(envoyConfig)
}

func summarizeOCSFProxyLogs(logs []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(logs))
	var backtraceFrames []string
	var failureLines []string
	var recentLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 512 {
			line = line[:512] + "...[truncated]"
		}
		recentLines = append(recentLines, line)
		if len(recentLines) > 6 {
			recentLines = recentLines[1:]
		}

		if strings.Contains(line, "envoy backtrace") {
			for _, field := range strings.Fields(line) {
				if !strings.HasPrefix(field, "#") {
					continue
				}
				var frame int
				if _, err := fmt.Sscanf(field, "#%d:", &frame); err == nil && frame <= 11 {
					backtraceFrames = append(backtraceFrames, line[strings.Index(line, "envoy backtrace"):])
				}
				break
			}
			continue
		}

		lower := strings.ToLower(line)
		if strings.Contains(lower, "envoy exited with error") || strings.Contains(lower, "segmentation fault") ||
			strings.Contains(lower, "\terror\t") || strings.Contains(lower, "\tcritical\t") || strings.Contains(lower, "\tfatal\t") {
			failureLines = append(failureLines, line)
			if len(failureLines) > 4 {
				failureLines = failureLines[1:]
			}
		}
	}

	var relevantLines []string
	if len(backtraceFrames) > 0 {
		relevantLines = append(relevantLines, "Envoy backtrace frames #0-11:")
		relevantLines = append(relevantLines, backtraceFrames...)
	}
	if len(failureLines) > 0 {
		relevantLines = append(relevantLines, "Proxy failure lines:")
		relevantLines = append(relevantLines, failureLines...)
	}
	if len(relevantLines) == 0 {
		relevantLines = append(relevantLines, "Last proxy log lines:")
		relevantLines = append(relevantLines, recentLines...)
	}
	if err := scanner.Err(); err != nil {
		relevantLines = append(relevantLines, fmt.Sprintf("read proxy logs: %v", err))
	}
	return strings.Join(relevantLines, "\n")
}

func findOCSFEvent(logs []byte, expectedOperation string) map[string]any {
	scanner := bufio.NewScanner(bytes.NewReader(logs))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		api, ok := event["api"].(map[string]any)
		if ok && api["operation"] == expectedOperation {
			return event
		}
	}
	return nil
}

func findOCSFMarkerDiagnostic(logs []byte) map[string]any {
	scanner := bufio.NewScanner(bytes.NewReader(logs))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record[ocsfMarkerDiagnosticKey] == true {
			return record
		}
	}
	return nil
}

func ocsfEventObject(object map[string]any, field string) map[string]any {
	value, ok := object[field].(map[string]any)
	Expect(ok).To(BeTrue(), "expected OCSF field %q to be an object, got %T", field, object[field])
	return value
}
