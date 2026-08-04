/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPServerDataPlane is the Schema for the MCP Server data planes API.
// It manages a MCP Server Deployment that uses Konnect control plane configuration
// via a referenced MCPServer resource.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.deployment.scaling.horizontal.static.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=mcpdp,categories=kong
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type MCPServerDataPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of MCPServerDataPlane.
	//
	// +required
	Spec MCPServerDataPlaneSpec `json:"spec,omitzero"`

	// Status defines the observed state of MCPServerDataPlane.
	//
	// +optional
	Status MCPServerDataPlaneStatus `json:"status,omitempty"`
}

// MCPServerDataPlaneList contains a list of MCPServerDataPlane.
//
// +kubebuilder:object:root=true
type MCPServerDataPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []MCPServerDataPlane `json:"items"`
}

// MCPServerDataPlaneSpec defines the desired state of MCPServerDataPlane.
type MCPServerDataPlaneSpec struct {
	// MCPServerRef references the control plane this MCPServerDataPlane connects to.
	// The type field identifies which kind of MCPServer is being referenced.
	// Currently only konnectNamespacedRef is supported, which references a
	// MCPServer resource in the same namespace.
	//
	// +required
	MCPServerRef MCPServerRef `json:"mcpServerRef,omitzero"`

	// Deployment configures the Deployment: image, replicas, resources,
	// extra env vars, volume mounts, etc.
	//
	// +optional
	Deployment *DeploymentOptions `json:"deployment,omitempty"`
}

// DeploymentOptions specifies options for the Deployment managed by the MCPServerDataPlane controller.
type DeploymentOptions struct {
	// Scaling defines the scaling options for the deployment.
	//
	// +optional
	Scaling *Scaling `json:"scaling,omitempty"`
}

// Scaling defines the scaling options for the deployment.
type Scaling struct {
	// HorizontalScaling defines horizontal scaling options for the deployment.
	//
	// +optional
	HorizontalScaling *HorizontalScaling `json:"horizontal,omitempty"`
}

// MCPHorizontalScalingType defines the type of horizontal scaling to use for
// the MCP deployment.
type MCPHorizontalScalingType string

const (
	// MCPHorizontalScalingTypeStatic indicates that the deployment should be
	// scaled by setting the number of replicas directly.
	MCPHorizontalScalingTypeStatic MCPHorizontalScalingType = "static"
)

// HorizontalScaling defines horizontal scaling options for the deployment.
// It holds all the options from the HorizontalPodAutoscalerSpec besides the
// ScaleTargetRef which is being controlled by the Operator.
//
// +kubebuilder:validation:XValidation:rule="self.type != 'static' || (has(self.static.replicas) && self.static.replicas >= 0)",message="When type is static: replicas must be set and must be non-negative"
type HorizontalScaling struct {
	// Type indicates the type of horizontal scaling to use.
	// Currently only "static" is supported, which means the deployment will be
	// scaled by setting the number of replicas directly.
	//
	// +optional
	// +kubebuilder:validation:Enum=static
	Type MCPHorizontalScalingType `json:"type,omitempty"`

	// Static defines the static horizontal scaling options for the deployment.
	//
	// +optional
	Static MCPHorizontalScalingStatic `json:"static,omitempty"`
}

// MCPHorizontalScalingStatic defines the static horizontal scaling options for
// the MCP deployment.
type MCPHorizontalScalingStatic struct {
	// Replicas describes the number of desired replicas.
	// This is a pointer to distinguish between explicit zero and not specified.
	//
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`
}

// MCPServerDataPlaneStatus defines the observed state of MCPServerDataPlane.
type MCPServerDataPlaneStatus struct {
	// Conditions describe the status of the MCPServerDataPlane.
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Ready", status: "Unknown", reason: "Pending", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ReadyReplicas indicates how many replicas have reported to be ready.
	//
	// +kubebuilder:default=0
	// +optional
	ReadyReplicas int32 `json:"readyReplicas"`

	// Replicas indicates how many replicas have been set for the MCPServerDataPlane.
	//
	// +kubebuilder:default=0
	// +optional
	Replicas int32 `json:"replicas"`

	// Selector is the label selector used by the scale subresource to match
	// the Pods managed by the owned Deployment.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Selector string `json:"selector,omitempty"`

	// Version indicates the version of the referenced, remote MCPServer.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version,omitempty"`
}

// GetConditions retrieves the MCPServerDataPlane Status Conditions.
func (e *MCPServerDataPlane) GetConditions() []metav1.Condition {
	return e.Status.Conditions
}

// SetConditions sets the MCPServerDataPlane Status Conditions.
func (e *MCPServerDataPlane) SetConditions(conditions []metav1.Condition) {
	e.Status.Conditions = conditions
}
