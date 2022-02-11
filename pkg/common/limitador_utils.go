package common

import (
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RateLimitFactory struct {
	Key        client.ObjectKey
	Conditions []string
	MaxValue   int
	Namespace  string
	Seconds    int
	Variables  []string
}

func (r *RateLimitFactory) RateLimit() *limitadorv1alpha1.RateLimit {
	return &limitadorv1alpha1.RateLimit{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimit",
			APIVersion: "limitador.kuadrant.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Key.Name,
			Namespace: r.Key.Namespace,
		},
		Spec: limitadorv1alpha1.RateLimitSpec{
			Conditions: r.Conditions,
			MaxValue:   r.MaxValue,
			Namespace:  r.Namespace,
			Seconds:    r.Seconds,
			Variables:  r.Variables,
		},
	}
}
