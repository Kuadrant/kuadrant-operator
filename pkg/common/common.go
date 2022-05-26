package common

import (
	"context"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kuadrant/kuadrant-operator/kuadrantcontrollermanifests"
)

func FetchEnv(key string, def string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}

	return val
}

func KuadrantControllerImage(ctx context.Context, scheme *runtime.Scheme) (string, error) {
	image := "unknown"

	parser := func(obj runtime.Object) error {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.GetName() == "kuadrant-controller-manager" {
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "manager" {
						image = container.Image
					}
				}
			}
		}
		return nil
	}

	content, err := kuadrantcontrollermanifests.Content()
	if err != nil {
		return "", err
	}

	err = DecodeFile(ctx, content, scheme, parser)
	if err != nil {
		return "", err
	}

	return image, nil
}
