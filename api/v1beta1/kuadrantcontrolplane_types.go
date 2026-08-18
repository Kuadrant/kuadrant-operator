package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	KuadrantControlPlaneDefaultName = "default"

	ControlPlaneConditionReady = "Ready"

	ControlPlaneReasonComponentsHealthy   = "ComponentsHealthy"
	ControlPlaneReasonComponentsUnhealthy = "ComponentsUnhealthy"
	ControlPlaneReasonDeployFailed        = "DeployFailed"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName=kcp
//+kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="KuadrantControlPlane name must be default"
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
//+kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KuadrantControlPlane manages the lifecycle of Kuadrant component operators.
// The operator auto-creates a singleton named "default" on startup.
// Deleting this resource is a no-op — the operator re-creates it immediately.
type KuadrantControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status KuadrantControlPlaneStatus `json:"status,omitempty"`
}

// KuadrantControlPlaneStatus defines the observed state of the control plane.
type KuadrantControlPlaneStatus struct {
	// ObservedGeneration is the most recent generation observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Components reports the status of each managed component operator.
	// +listType=map
	// +listMapKey=name
	// +optional
	Components []ComponentStatus `json:"components,omitempty"`
}

// ComponentStatus reports the observed state of a single component operator.
type ComponentStatus struct {
	// Name of the component (e.g., "dns-operator").
	Name string `json:"name"`

	// Ready indicates whether the component Deployment has available replicas.
	Ready bool `json:"ready"`

	// ChartVersion is the version from the synced Helm chart's Chart.yaml.
	// +optional
	ChartVersion string `json:"chartVersion,omitempty"`

	// Images reports the RELATED_IMAGE env var values for this component.
	// +listType=map
	// +listMapKey=name
	// +optional
	Images []ImageStatus `json:"images,omitempty"`

	// CRDs reports establishment status of CRDs managed by this component.
	// +listType=map
	// +listMapKey=name
	// +optional
	CRDs []CRDStatus `json:"crds,omitempty"`
}

// ImageStatus reports a single image reference from a RELATED_IMAGE env var.
type ImageStatus struct {
	// Name identifies this image (e.g., "controller", "broker").
	Name string `json:"name"`

	// Image is the RELATED_IMAGE env var value.
	Image string `json:"image"`
}

// CRDStatus reports the observed state of a single CRD.
type CRDStatus struct {
	// Name of the CRD (e.g., "dnsrecords.kuadrant.io").
	Name string `json:"name"`

	// Established indicates whether the CRD is accepted by the API server.
	Established bool `json:"established"`
}

//+kubebuilder:object:root=true

// KuadrantControlPlaneList contains a list of KuadrantControlPlane.
type KuadrantControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KuadrantControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KuadrantControlPlane{}, &KuadrantControlPlaneList{})
}
