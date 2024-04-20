//go:build unit

package controllers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

var _ handler.EventHandler = &testEventHandler{}

type testEventHandler struct {
	lastEventFunc string
}

func (h *testEventHandler) Create(_ context.Context, _ event.CreateEvent, _ workqueue.RateLimitingInterface) {
	h.lastEventFunc = "Create"
}
func (h *testEventHandler) Update(_ context.Context, _ event.UpdateEvent, _ workqueue.RateLimitingInterface) {
	h.lastEventFunc = "Update"
}
func (h *testEventHandler) Delete(_ context.Context, _ event.DeleteEvent, _ workqueue.RateLimitingInterface) {
	h.lastEventFunc = "Delete"
}
func (h *testEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.RateLimitingInterface) {
	h.lastEventFunc = "Generic"
}

// Test policy that implements kuadrantgatewayapi.Policy

var (
	_ kuadrantgatewayapi.Policy       = &TestPolicy{}
	_ kuadrantgatewayapi.PolicyStatus = &FakePolicyStatus{}
)

type TestPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
	Status    FakePolicyStatus                         `json:"status,omitempty"`
}

func (p *TestPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.DirectPolicy
}

func (p *TestPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.TargetRef
}

func (p *TestPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *TestPolicy) DeepCopyObject() runtime.Object {
	if c := p.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (p *TestPolicy) DeepCopy() *TestPolicy {
	if p == nil {
		return nil
	}
	out := new(TestPolicy)
	p.DeepCopyInto(out)
	return out
}

func (p *TestPolicy) DeepCopyInto(out *TestPolicy) {
	*out = *p
	out.TypeMeta = p.TypeMeta
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.TargetRef.DeepCopyInto(&out.TargetRef)
	out.Status = p.Status
}

type FakePolicyStatus struct {
	Conditions []metav1.Condition
}

func (s *FakePolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

func TestPolicyStatusEventHandler(t *testing.T) {
	tests := []struct {
		name          string
		lastEventFunc func() string
		expected      string
	}{
		{
			name: "Create event",
			lastEventFunc: func() string {
				testHandler := &testEventHandler{}
				h := NewPolicyStatusEventHandler(WithHandler(testHandler))
				h.Create(context.Background(), event.CreateEvent{}, nil)
				return testHandler.lastEventFunc
			},
			expected: "Create",
		},
		{
			name: "Update event with different status",
			lastEventFunc: func() string {
				testHandler := &testEventHandler{}
				h := NewPolicyStatusEventHandler(WithHandler(testHandler))
				ev := event.UpdateEvent{
					ObjectOld: &TestPolicy{
						Status: FakePolicyStatus{
							Conditions: []metav1.Condition{},
						},
					},
					ObjectNew: &TestPolicy{
						Status: FakePolicyStatus{
							Conditions: []metav1.Condition{
								{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
									Reason: "ValidPolicy",
								},
							},
						},
					},
				}
				h.Update(context.Background(), ev, nil)
				return testHandler.lastEventFunc
			},
			expected: "Update",
		},
		{
			name: "Update event without different status",
			lastEventFunc: func() string {
				testHandler := &testEventHandler{}
				h := NewPolicyStatusEventHandler(WithHandler(testHandler))
				ev := event.UpdateEvent{
					ObjectOld: &TestPolicy{
						Status: FakePolicyStatus{
							Conditions: []metav1.Condition{
								{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
									Reason: "ValidPolicy",
								},
							},
						},
					},
					ObjectNew: &TestPolicy{
						Status: FakePolicyStatus{
							Conditions: []metav1.Condition{
								{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
									Reason: "ValidPolicy",
								},
							},
						},
					},
				}
				h.Update(context.Background(), ev, nil)
				return testHandler.lastEventFunc
			},
			expected: "",
		},
		{
			name: "Delete event",
			lastEventFunc: func() string {
				testHandler := &testEventHandler{}
				h := NewPolicyStatusEventHandler(WithHandler(testHandler))
				h.Delete(context.Background(), event.DeleteEvent{}, nil)
				return testHandler.lastEventFunc
			},
			expected: "Delete",
		},
		{
			name: "Generic event",
			lastEventFunc: func() string {
				testHandler := &testEventHandler{}
				h := NewPolicyStatusEventHandler(WithHandler(testHandler))
				h.Generic(context.Background(), event.GenericEvent{}, nil)
				return testHandler.lastEventFunc
			},
			expected: "Generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.lastEventFunc()
			if got != tt.expected {
				t.Errorf("%s failed. Expected %s, got %s", tt.name, tt.expected, got)
			}
		})
	}
}
