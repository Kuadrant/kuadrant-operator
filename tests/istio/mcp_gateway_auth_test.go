//go:build integration && mcp_auth_e2e

package istio_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mcpAuthGatewayURLDefault          = "https://mcp.mcp-gateway.local:8009/mcp"
	mcpAuthKeycloakTokenURLDefault    = "https://keycloak.127-0-0-1.sslip.io:8002/realms/mcp/protocol/openid-connect/token"
	mcpAuthNamespaceDefault           = "kuadrant-system"
	mcpAuthTestServerNamespaceDefault = "mcp-test"
	mcpAuthExtensionName              = "mcp-gateway-extension"
	mcpAuthReadyTimeout               = 5 * time.Minute
	mcpAuthHTTPTimeout                = 30 * time.Second
)

var (
	mcpAuthGatewayURL       = mcpAuthEnv("MCP_AUTH_GATEWAY_URL", mcpAuthGatewayURLDefault)
	mcpAuthKeycloakTokenURL = mcpAuthEnv("MCP_AUTH_KEYCLOAK_TOKEN_URL", mcpAuthKeycloakTokenURLDefault)
	mcpAuthNamespace        = mcpAuthEnv("MCP_AUTH_NAMESPACE", mcpAuthNamespaceDefault)
	mcpAuthTestServerNS     = mcpAuthEnv("MCP_AUTH_TEST_SERVER_NAMESPACE", mcpAuthTestServerNamespaceDefault)
)

var _ = Describe("MCP Gateway AuthPolicy integration", Ordered, func() {
	var (
		createdResources  []*unstructured.Unstructured
		originalExtension *unstructured.Unstructured
	)

	BeforeAll(func(ctx SpecContext) {
		if os.Getenv("MCP_AUTH_E2E") != "true" {
			Skip("MCP_AUTH_E2E is not true")
		}

		By("patching MCPGatewayExtension to use the trusted header public key")
		extension := mcpAuthResource("MCPGatewayExtension", mcpAuthExtensionName, mcpAuthNamespace, nil)
		Expect(testClient().Get(ctx, client.ObjectKey{Name: mcpAuthExtensionName, Namespace: mcpAuthNamespace}, extension)).To(Succeed())
		originalExtension = extension.DeepCopy()
		deployment := &appsv1.Deployment{}
		deploymentKey := client.ObjectKey{Name: "mcp-gateway", Namespace: mcpAuthNamespace}
		Expect(testClient().Get(ctx, deploymentKey, deployment)).To(Succeed())
		previousDeploymentGeneration := deployment.Generation
		currentSecretName, _, err := unstructured.NestedString(extension.Object, "spec", "trustedHeadersKey", "secretName")
		Expect(err).NotTo(HaveOccurred())
		currentGeneration, _, err := unstructured.NestedString(extension.Object, "spec", "trustedHeadersKey", "generate")
		Expect(err).NotTo(HaveOccurred())
		needsDeploymentGeneration := currentSecretName != "trusted-headers-public-key"
		needsTrustedHeaderPatch := needsDeploymentGeneration || currentGeneration != "Disabled"
		if needsTrustedHeaderPatch {
			patch := client.MergeFrom(extension.DeepCopy())
			Expect(unstructured.SetNestedField(extension.Object, map[string]any{
				"secretName": "trusted-headers-public-key",
				"generate":   "Disabled",
			}, "spec", "trustedHeadersKey")).To(Succeed())
			Expect(testClient().Patch(ctx, extension, patch)).To(Succeed())
		}
		Expect(mcpAuthWaitDeploymentRollout(ctx, testClient(), deploymentKey, previousDeploymentGeneration, needsDeploymentGeneration)).To(Succeed())

		By("creating MCPServerRegistrations for the authenticated test servers")
		for _, registration := range []*unstructured.Unstructured{
			mcpAuthRegistration("test-server1", mcpAuthTestServerNS, "mcp-server1-route", "test1_"),
			mcpAuthRegistration("test-server2", mcpAuthTestServerNS, "mcp-server2-route", "test2_"),
		} {
			Expect(testClient().Create(ctx, registration)).To(Succeed())
			createdResources = append(createdResources, registration)
		}
		for _, registration := range createdResources {
			Expect(mcpAuthWaitReady(ctx, testClient(), registration.GroupVersionKind(), client.ObjectKey{
				Name: registration.GetName(), Namespace: registration.GetNamespace(),
			})).To(Succeed())
		}

		By("waiting for AuthPolicy to reject unauthenticated initialization")
		Eventually(func(g Gomega) {
			status, _, _, err := mcpAuthRawPost(ctx, mcpAuthGatewayURL, "", mcpAuthInitializeBody(), nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(http.StatusUnauthorized))
		}).WithContext(ctx).Should(Succeed())
	})

	AfterAll(func(ctx SpecContext) {
		for i := len(createdResources) - 1; i >= 0; i-- {
			if err := testClient().Delete(ctx, createdResources[i]); err != nil && !apierrors.IsNotFound(err) {
				Fail(fmt.Sprintf("delete %s/%s: %v", createdResources[i].GetNamespace(), createdResources[i].GetName(), err))
			}
		}
		if originalExtension == nil {
			return
		}

		current := mcpAuthResource("MCPGatewayExtension", mcpAuthExtensionName, mcpAuthNamespace, nil)
		if err := testClient().Get(ctx, client.ObjectKey{Name: mcpAuthExtensionName, Namespace: mcpAuthNamespace}, current); err != nil {
			if !apierrors.IsNotFound(err) {
				Fail(fmt.Sprintf("get MCPGatewayExtension for restore: %v", err))
			}
			return
		}
		restored := current.DeepCopy()
		if value, found, err := unstructured.NestedFieldCopy(originalExtension.Object, "spec", "trustedHeadersKey"); err != nil {
			Fail(fmt.Sprintf("read original trustedHeadersKey: %v", err))
		} else if found {
			Expect(unstructured.SetNestedField(restored.Object, value, "spec", "trustedHeadersKey")).To(Succeed())
		} else {
			unstructured.RemoveNestedField(restored.Object, "spec", "trustedHeadersKey")
		}
		Expect(testClient().Patch(ctx, restored, client.MergeFrom(current))).To(Succeed())
	})

	It("returns 401 for unauthenticated requests", func(ctx SpecContext) {
		status, body, headers, err := mcpAuthRawPost(ctx, mcpAuthGatewayURL, "", mcpAuthInitializeBody(), nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(http.StatusUnauthorized))
		Expect(body).To(ContainSubstring("Authentication required"))
		Expect(headers.Get("WWW-Authenticate")).To(ContainSubstring("Bearer"))
	})

	It("returns 401 for malformed JWT", func(ctx SpecContext) {
		status, _, _, err := mcpAuthRawPost(ctx, mcpAuthGatewayURL, "", mcpAuthInitializeBody(), map[string]string{
			"Authorization": "Bearer not-a-real-jwt",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("filters tools/list by roles in a valid JWT", func(ctx SpecContext) {
		headers := mcpAuthAuthorizationHeaders(ctx)
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		var tools []string
		Eventually(func(g Gomega) {
			var err error
			_, tools, err = mcpAuthListTools(ctx, mcpAuthGatewayURL, sessionID, headers)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(tools).To(ContainElement("test1_greet"))
			g.Expect(tools).To(ContainElement("test2_headers"))
		}).WithContext(ctx).Should(Succeed())
		Expect(tools).NotTo(ContainElement("test1_time"))
		Expect(tools).NotTo(ContainElement("test2_hello_world"))
	})

	It("allows an authorized tool call", func(ctx SpecContext) {
		headers := mcpAuthAuthorizationHeaders(ctx)
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		status, content, err := mcpAuthCallTool(ctx, mcpAuthGatewayURL, sessionID, "test1_greet", map[string]any{"name": "e2e"}, headers)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(http.StatusOK))
		Expect(content).NotTo(BeEmpty())
	})

	It("rejects an unauthorized tool call", func(ctx SpecContext) {
		headers := mcpAuthAuthorizationHeaders(ctx)
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		status, _, err := mcpAuthCallTool(ctx, mcpAuthGatewayURL, sessionID, "test1_time", nil, headers)
		Expect(err).To(HaveOccurred())
		Expect(status).To(Equal(http.StatusUnauthorized))
		Expect(err).To(MatchError(ContainSubstring("Forbidden")))
	})

	It("filters prompts/list by roles in a valid JWT", func(ctx SpecContext) {
		headers := mcpAuthAuthorizationHeaders(ctx)
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		var prompts []string
		Eventually(func(g Gomega) {
			var err error
			_, prompts, err = mcpAuthListPrompts(ctx, mcpAuthGatewayURL, sessionID, headers)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(prompts).NotTo(BeEmpty())
		}).WithContext(ctx).Should(Succeed())
		Expect(prompts).To(ContainElement("test1_greet"))
	})

	It("allows prompts/get when it is the first request to a server", func(ctx SpecContext) {
		headers := mcpAuthAuthorizationHeaders(ctx)
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		status, body, err := mcpAuthGetPrompt(ctx, mcpAuthGatewayURL, sessionID, "test1_greet", map[string]string{"name": "reviewer"}, headers)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(ContainSubstring("Say hi to reviewer"))
	})

	It("returns an empty prompt list for a JWT and MCPVirtualServer with no intersection", func(ctx SpecContext) {
		By("registering the everything server")
		everything := mcpAuthRegistration("everything-server", mcpAuthTestServerNS, "everything-server-route", "everything_")
		Expect(testClient().Create(ctx, everything)).To(Succeed())
		createdResources = append(createdResources, everything)
		Expect(mcpAuthWaitReady(ctx, testClient(), everything.GroupVersionKind(), client.ObjectKey{
			Name: everything.GetName(), Namespace: everything.GetNamespace(),
		})).To(Succeed())

		virtualServer := mcpAuthVirtualServer("auth-prompt-combined-vs", mcpAuthTestServerNS, []string{"test1_greet"}, []string{"everything_simple_prompt"})
		Expect(testClient().Create(ctx, virtualServer)).To(Succeed())
		createdResources = append(createdResources, virtualServer)

		headers := mcpAuthAuthorizationHeaders(ctx)
		headers["X-Mcp-Virtualserver"] = fmt.Sprintf("%s/%s", virtualServer.GetNamespace(), virtualServer.GetName())
		sessionID := mcpAuthInitializeSession(ctx, headers)
		Expect(mcpAuthNotifyInitialized(ctx, mcpAuthGatewayURL, sessionID, headers)).To(Succeed())

		Eventually(func(g Gomega) {
			_, prompts, err := mcpAuthListPrompts(ctx, mcpAuthGatewayURL, sessionID, headers)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(prompts).To(BeEmpty())
		}).WithContext(ctx).Should(Succeed())
	})
})

func mcpAuthEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func mcpAuthResource(kind, name, namespace string, spec map[string]any) *unstructured.Unstructured {
	object := map[string]any{
		"apiVersion": "mcp.kuadrant.io/v1",
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	if spec != nil {
		object["spec"] = spec
	}
	resource := &unstructured.Unstructured{Object: object}
	resource.SetGroupVersionKind(schema.GroupVersionKind{Group: "mcp.kuadrant.io", Version: "v1", Kind: kind})
	return resource
}

func mcpAuthRegistration(name, namespace, routeName, prefix string) *unstructured.Unstructured {
	return mcpAuthResource("MCPServerRegistration", name, namespace, map[string]any{
		"prefix": prefix,
		"targetRef": map[string]any{
			"group": "gateway.networking.k8s.io",
			"kind":  "HTTPRoute",
			"name":  routeName,
		},
	})
}

func mcpAuthVirtualServer(name, namespace string, tools, prompts []string) *unstructured.Unstructured {
	return mcpAuthResource("MCPVirtualServer", name, namespace, map[string]any{
		"tools":   stringSliceToAny(tools),
		"prompts": stringSliceToAny(prompts),
	})
}

func stringSliceToAny(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func mcpAuthWaitReady(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, key client.ObjectKey) error {
	waitCtx, cancel := context.WithTimeout(ctx, mcpAuthReadyTimeout)
	defer cancel()

	var lastErr error
	for {
		resource := &unstructured.Unstructured{}
		resource.SetGroupVersionKind(gvk)
		if err := c.Get(waitCtx, key, resource); err != nil {
			lastErr = fmt.Errorf("get %s %s/%s: %w", gvk.Kind, key.Namespace, key.Name, err)
		} else if ready, found, err := unstructured.NestedSlice(resource.Object, "status", "conditions"); err != nil {
			lastErr = fmt.Errorf("read %s %s/%s conditions: %w", gvk.Kind, key.Namespace, key.Name, err)
		} else if found {
			for _, item := range ready {
				condition, ok := item.(map[string]any)
				if !ok || condition["type"] != "Ready" {
					continue
				}
				if condition["status"] == "True" {
					return nil
				}
				lastErr = fmt.Errorf("%s %s/%s Ready condition is %v: %v", gvk.Kind, key.Namespace, key.Name, condition["status"], condition["message"])
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("%s %s/%s has no Ready condition", gvk.Kind, key.Namespace, key.Name)
			}
		} else {
			lastErr = fmt.Errorf("%s %s/%s has no status conditions", gvk.Kind, key.Namespace, key.Name)
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for %s %s/%s to become Ready: %w (last error: %v)", gvk.Kind, key.Namespace, key.Name, waitCtx.Err(), lastErr)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
func mcpAuthWaitDeploymentRollout(ctx context.Context, c client.Client, key client.ObjectKey, previousGeneration int64, requireGeneration bool) error {
	waitCtx, cancel := context.WithTimeout(ctx, mcpAuthReadyTimeout)
	defer cancel()

	var lastErr error
	for {
		deployment := &appsv1.Deployment{}
		if err := c.Get(waitCtx, key, deployment); err != nil {
			lastErr = fmt.Errorf("get Deployment %s/%s: %w", key.Namespace, key.Name, err)
		} else {
			desiredReplicas := int32(1)
			if deployment.Spec.Replicas != nil {
				desiredReplicas = *deployment.Spec.Replicas
			}
			switch {
			case requireGeneration && deployment.Generation <= previousGeneration:
				lastErr = fmt.Errorf("Deployment %s/%s has not observed a new generation (old %d, current %d)", key.Namespace, key.Name, previousGeneration, deployment.Generation)
			case deployment.Status.ObservedGeneration < deployment.Generation:
				lastErr = fmt.Errorf("Deployment %s/%s has observed generation %d, want %d", key.Namespace, key.Name, deployment.Status.ObservedGeneration, deployment.Generation)
			case desiredReplicas > 0 && deployment.Status.Replicas != desiredReplicas:
				lastErr = fmt.Errorf("Deployment %s/%s has %d total replicas, want %d", key.Namespace, key.Name, deployment.Status.Replicas, desiredReplicas)
			case deployment.Status.UpdatedReplicas < desiredReplicas:
				lastErr = fmt.Errorf("Deployment %s/%s has %d updated replicas, want %d", key.Namespace, key.Name, deployment.Status.UpdatedReplicas, desiredReplicas)
			case deployment.Status.ReadyReplicas < desiredReplicas:
				lastErr = fmt.Errorf("Deployment %s/%s has %d ready replicas, want %d", key.Namespace, key.Name, deployment.Status.ReadyReplicas, desiredReplicas)
			case deployment.Status.AvailableReplicas < desiredReplicas:
				lastErr = fmt.Errorf("Deployment %s/%s has %d available replicas, want %d", key.Namespace, key.Name, deployment.Status.AvailableReplicas, desiredReplicas)
			default:
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for Deployment %s/%s rollout: %w (last error: %v)", key.Namespace, key.Name, waitCtx.Err(), lastErr)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func mcpAuthDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err == nil && (host == "mcp.mcp-gateway.local" || host == "keycloak.127-0-0-1.sslip.io") {
		address = net.JoinHostPort("127.0.0.1", port)
	}
	return (&net.Dialer{Timeout: mcpAuthHTTPTimeout}).DialContext(ctx, network, address)
}

func mcpAuthHTTPClient() *http.Client {
	return &http.Client{
		Timeout: mcpAuthHTTPTimeout,
		Transport: &http.Transport{
			// Keep the URL host for TLS SNI and HTTP Host while dialing the
			// loopback address used by the Kind ingress ports.
			DialContext:       mcpAuthDialContext,
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // self-signed Kind gateway and Keycloak endpoints
			ForceAttemptHTTP2: false,
		},
	}
}

func mcpAuthPost(ctx context.Context, endpoint, sessionID string, body []byte, headers map[string]string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create MCP POST request for %s: %w", endpoint, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := mcpAuthHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("send MCP POST to %s: %w", endpoint, err)
	}
	return response, nil
}

func mcpAuthInitializeBody() []byte {
	return []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"operator-auth-e2e","version":"0.0.1"}}}`)
}

func mcpAuthInitializeSession(ctx context.Context, headers map[string]string) string {
	sessionID, err := mcpAuthInitialize(ctx, mcpAuthGatewayURL, headers)
	Expect(err).NotTo(HaveOccurred())
	Expect(sessionID).NotTo(BeEmpty())
	return sessionID
}

func mcpAuthInitialize(ctx context.Context, endpoint string, headers map[string]string) (string, error) {
	response, err := mcpAuthPost(ctx, endpoint, "", mcpAuthInitializeBody(), headers)
	if err != nil {
		return "", fmt.Errorf("initialize request failed: %w", err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return "", fmt.Errorf("read initialize response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("initialize returned status %d", response.StatusCode)
	}
	if sessionID := response.Header.Get("Mcp-Session-Id"); sessionID != "" {
		return sessionID, nil
	}
	return "", fmt.Errorf("initialize response has no Mcp-Session-Id header")
}

func mcpAuthNotifyInitialized(ctx context.Context, endpoint, sessionID string, headers map[string]string) error {
	response, err := mcpAuthPost(ctx, endpoint, sessionID, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), headers)
	if err != nil {
		return fmt.Errorf("notifications/initialized failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read notifications/initialized response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notifications/initialized returned status %d: %s", response.StatusCode, string(body))
	}
	return nil
}

func mcpAuthListTools(ctx context.Context, endpoint, sessionID string, headers map[string]string) (int, []string, error) {
	return mcpAuthListNames(ctx, endpoint, sessionID, "tools/list", "tools", headers)
}

func mcpAuthListPrompts(ctx context.Context, endpoint, sessionID string, headers map[string]string) (int, []string, error) {
	return mcpAuthListNames(ctx, endpoint, sessionID, "prompts/list", "prompts", headers)
}

func mcpAuthListNames(ctx context.Context, endpoint, sessionID, method, resultKey string, headers map[string]string) (int, []string, error) {
	body := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"%s"}`, time.Now().UnixNano(), method))
	response, err := mcpAuthPost(ctx, endpoint, sessionID, body, headers)
	if err != nil {
		return 0, nil, fmt.Errorf("%s request failed: %w", method, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		responseBody, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return response.StatusCode, nil, fmt.Errorf("%s returned status %d and body could not be read: %w", method, response.StatusCode, readErr)
		}
		return response.StatusCode, nil, fmt.Errorf("%s returned status %d: %s", method, response.StatusCode, string(responseBody))
	}
	result, err := mcpAuthReadJSONRPCResult(response)
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("parse %s response: %w", method, err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(result, &envelope); err != nil {
		return response.StatusCode, nil, fmt.Errorf("parse %s result: %w", method, err)
	}
	itemsJSON, found := envelope[resultKey]
	if !found {
		return response.StatusCode, nil, fmt.Errorf("%s result has no %q field", method, resultKey)
	}
	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(itemsJSON, &items); err != nil {
		return response.StatusCode, nil, fmt.Errorf("parse %s %s: %w", method, resultKey, err)
	}
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return response.StatusCode, names, nil
}

func mcpAuthCallTool(ctx context.Context, endpoint, sessionID, toolName string, args map[string]any, headers map[string]string) (int, []map[string]any, error) {
	params := map[string]any{"name": toolName}
	if len(args) > 0 {
		params["arguments"] = args
	}
	requestBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params":  params,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("marshal tools/call request: %w", err)
	}
	response, err := mcpAuthPost(ctx, endpoint, sessionID, requestBody, headers)
	if err != nil {
		return 0, nil, fmt.Errorf("tools/call request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		responseBody, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return response.StatusCode, nil, fmt.Errorf("tools/call returned status %d and body could not be read: %w", response.StatusCode, readErr)
		}
		return response.StatusCode, nil, fmt.Errorf("tools/call returned status %d: %s", response.StatusCode, string(responseBody))
	}
	result, err := mcpAuthReadJSONRPCResult(response)
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("parse tools/call response: %w", err)
	}
	var callResult struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(result, &callResult); err != nil {
		return response.StatusCode, nil, fmt.Errorf("parse tools/call result: %w", err)
	}
	return response.StatusCode, callResult.Content, nil
}

func mcpAuthGetPrompt(ctx context.Context, endpoint, sessionID, promptName string, args map[string]string, headers map[string]string) (int, string, error) {
	params := map[string]any{"name": promptName}
	if len(args) > 0 {
		params["arguments"] = args
	}
	requestBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "prompts/get",
		"params":  params,
	})
	if err != nil {
		return 0, "", fmt.Errorf("marshal prompts/get request: %w", err)
	}
	status, body, _, err := mcpAuthRawPost(ctx, endpoint, sessionID, requestBody, headers)
	if err != nil {
		return status, "", fmt.Errorf("prompts/get request failed: %w", err)
	}
	return status, body, nil
}

func mcpAuthRawPost(ctx context.Context, endpoint, sessionID string, body []byte, headers map[string]string) (int, string, http.Header, error) {
	response, err := mcpAuthPost(ctx, endpoint, sessionID, body, headers)
	if err != nil {
		return 0, "", nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, "", response.Header, fmt.Errorf("read response from %s: %w", endpoint, err)
	}
	return response.StatusCode, string(responseBody), response.Header, nil
}

func mcpAuthReadJSONRPCResult(response *http.Response) (json.RawMessage, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read JSON-RPC response: %w", err)
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") || mcpAuthLooksLikeSSE(body) {
		return mcpAuthParseSSEResult(body)
	}
	var message struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC response: %w: %s", err, string(body))
	}
	if message.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error: %s", string(message.Error))
	}
	if message.Result == nil {
		return nil, fmt.Errorf("JSON-RPC response has no result: %s", string(body))
	}
	return message.Result, nil
}

func mcpAuthLooksLikeSSE(body []byte) bool {
	body = bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(body, []byte("event:")) || bytes.HasPrefix(body, []byte("data:"))
}

func mcpAuthParseSSEResult(body []byte) (json.RawMessage, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var message struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error: %s", string(message.Error))
		}
		if message.Result != nil {
			return message.Result, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan SSE response: %w", err)
	}
	return nil, fmt.Errorf("SSE response has no JSON-RPC result: %s", string(body))
}

func mcpAuthAuthorizationHeaders(ctx context.Context) map[string]string {
	token, err := mcpAuthKeycloakToken(ctx, "mcp", "mcp")
	Expect(err).NotTo(HaveOccurred())
	return map[string]string{"Authorization": "Bearer " + token}
}

func mcpAuthKeycloakToken(ctx context.Context, username, password string) (string, error) {
	values := url.Values{
		"grant_type":    {"password"},
		"client_id":     {"mcp-gateway"},
		"client_secret": {"secret"},
		"username":      {username},
		"password":      {password},
		"scope":         {"openid groups roles"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpAuthKeycloakTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("create Keycloak token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := mcpAuthHTTPClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("send Keycloak token request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read Keycloak token response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Keycloak token request returned status %d: %s", response.StatusCode, string(body))
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("decode Keycloak token response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("Keycloak token response has no access_token")
	}
	return tokenResponse.AccessToken, nil
}
