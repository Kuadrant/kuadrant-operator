package utils

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetServiceWorkloadSelector(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey) (map[string]string, error) {
	service, err := GetService(ctx, k8sClient, serviceKey)
	if err != nil {
		return nil, err
	}
	return service.Spec.Selector, nil
}

func GetService(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey) (*corev1.Service, error) {
	service := &corev1.Service{}
	if err := k8sClient.Get(ctx, serviceKey, service); err != nil {
		return nil, err
	}
	return service, nil
}

// GetServicePortNumber returns the port number from the referenced key and port info
// the port info can be named port or already a number.
func GetServicePortNumber(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey, servicePort string) (int32, error) {
	// check if the port is a number already.
	if num, err := strconv.ParseInt(servicePort, 10, 32); err == nil {
		return int32(num), nil
	}

	// As the port is name, resolv the port from the service
	service, err := GetService(ctx, k8sClient, serviceKey)
	if err != nil {
		// the service must exist
		return 0, err
	}

	for _, p := range service.Spec.Ports {
		if p.Name == servicePort {
			return int32(p.TargetPort.IntValue()), nil
		}
	}

	return 0, fmt.Errorf("service port %s was not found in %s", servicePort, serviceKey.String())
}
