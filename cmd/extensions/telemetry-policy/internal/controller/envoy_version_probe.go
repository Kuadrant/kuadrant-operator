package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	gatewayNamePodLabel = "gateway.networking.k8s.io/gateway-name"
	envoyProxyContainer = "istio-proxy"
	envoyVersionCommand = "pilot-agent"
)

const envoyVersionRecheckInterval = 30 * time.Second

var envoyVersionPattern = regexp.MustCompile(`(?:^|/)(v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)(?:/|$)`)

// EnvoyVersionProbe reports the lowest runtime Envoy version among a Gateway's
// proxy Pods. A missing Pod or an unreadable version is an error so callers can
// fail closed.
type EnvoyVersionProbe interface {
	MinimumVersion(ctx context.Context, namespace, gatewayName string) (string, error)
}

type kubernetesEnvoyVersionProbe struct {
	client kubernetes.Interface
	config *rest.Config
}

// NewKubernetesEnvoyVersionProbe creates a probe that reads the running Envoy
// process through Istio's pilot-agent admin request.
func NewKubernetesEnvoyVersionProbe(config *rest.Config) (EnvoyVersionProbe, error) {
	if config == nil {
		return nil, fmt.Errorf("Kubernetes REST config is required")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client for Envoy version probe: %w", err)
	}
	return &kubernetesEnvoyVersionProbe{client: client, config: rest.CopyConfig(config)}, nil
}

func (p *kubernetesEnvoyVersionProbe) MinimumVersion(ctx context.Context, namespace, gatewayName string) (string, error) {
	gatewayRef := fmt.Sprintf("%s/%s", namespace, gatewayName)
	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: gatewayNamePodLabel + "=" + gatewayName,
	})
	if err != nil {
		return "", fmt.Errorf("list proxy Pods for Gateway %s: %w", gatewayRef, err)
	}

	return minimumEnvoyVersionFromPods(ctx, gatewayRef, pods.Items, func(ctx context.Context, pod corev1.Pod) (string, error) {
		return p.readEnvoyVersion(ctx, namespace, pod.Name)
	})
}

func (p *kubernetesEnvoyVersionProbe) readEnvoyVersion(ctx context.Context, namespace, podName string) (string, error) {
	request := p.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: envoyProxyContainer,
			Command:   []string{envoyVersionCommand, "request", "GET", "/server_info"},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(p.config, "POST", request.URL())
	if err != nil {
		return "", fmt.Errorf("create Envoy server_info request: %w", err)
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", fmt.Errorf("request Envoy server_info: %w", err)
	}

	var serverInfo struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &serverInfo); err != nil {
		return "", fmt.Errorf("decode Envoy server_info: %w", err)
	}
	return serverInfo.Version, nil
}

func minimumEnvoyVersionFromPods(
	ctx context.Context,
	gatewayRef string,
	pods []corev1.Pod,
	readVersion func(context.Context, corev1.Pod) (string, error),
) (string, error) {
	var minimum *utilversion.Version
	proxyCount := 0
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		proxyCount++
		if pod.Status.Phase != corev1.PodRunning {
			return "", fmt.Errorf("proxy Pod %s/%s for Gateway %s is not running (phase %q)", pod.Namespace, pod.Name, gatewayRef, pod.Status.Phase)
		}

		versionText, err := readVersion(ctx, pod)
		if err != nil {
			return "", fmt.Errorf("read Envoy version from proxy Pod %s/%s for Gateway %s: %w", pod.Namespace, pod.Name, gatewayRef, err)
		}
		version, err := parseEnvoyRuntimeVersion(versionText)
		if err != nil {
			return "", fmt.Errorf("parse Envoy version from proxy Pod %s/%s for Gateway %s: %w", pod.Namespace, pod.Name, gatewayRef, err)
		}
		if minimum == nil || version.LessThan(minimum) {
			minimum = version
		}
	}
	if proxyCount == 0 || minimum == nil {
		return "", fmt.Errorf("no non-terminated proxy Pods were found for Gateway %s", gatewayRef)
	}
	return minimum.String(), nil
}

func parseEnvoyRuntimeVersion(versionText string) (*utilversion.Version, error) {
	versionText = strings.TrimSpace(versionText)
	match := envoyVersionPattern.FindStringSubmatch(versionText)
	if len(match) != 2 {
		return nil, fmt.Errorf("server_info version %q is not recognizable", versionText)
	}
	version, err := utilversion.ParseSemantic(match[1])
	if err != nil {
		return nil, fmt.Errorf("server_info version %q is invalid: %w", versionText, err)
	}
	return version, nil
}
