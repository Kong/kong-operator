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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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
// +kubebuilder:subresource:scale:specpath=.spec.deployment.replicas,statuspath=.status.replicas,selectorpath=.status.selector
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
//
// +kubebuilder:validation:XValidation:message="Using both replicas and scaling fields is not allowed.",rule="!(has(self.scaling) && has(self.replicas))"
type DeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This is effectively shorthand for setting a scaling minimum and maximum
	// to the same value. This field and the scaling field are mutually exclusive:
	// You can only configure one or the other.
	//
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Scaling defines the scaling options for the deployment.
	//
	// +optional
	Scaling *Scaling `json:"scaling,omitempty"`

	// Annotations are custom annotations that are propagated to the keg
	// Deployment metadata by the operator.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are custom labels that are propagated to the keg Deployment
	// metadata by the operator.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Labels map[string]string `json:"labels,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	// It's being applied on top of the generated Deployments using
	// [StrategicMergePatch](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch#StrategicMergePatch).
	//
	// Note: environment variables set here take precedence over strongly-typed
	// fields in Spec.Config. Using raw env vars is discouraged and intended for
	// advanced use cases only.
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// Scaling defines the scaling options for the deployment.
type Scaling struct {
	// HorizontalScaling defines horizontal scaling options for the deployment.
	//
	// +optional
	HorizontalScaling *HorizontalScaling `json:"horizontal,omitempty"`
}

// HorizontalScaling defines horizontal scaling options for the deployment.
// It holds all the options from the HorizontalPodAutoscalerSpec besides the
// ScaleTargetRef which is being controlled by the Operator.
type HorizontalScaling struct {
	// minReplicas is the lower limit for the number of replicas to which the autoscaler
	// can scale down.  It defaults to 1 pod.  minReplicas is allowed to be 0 if the
	// alpha feature gate HPAScaleToZero is enabled and at least one Object or External
	// metric is configured.  Scaling is active as long as at least one metric value is
	// available.
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty" protobuf:"varint,2,opt,name=minReplicas"`

	// maxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	// It cannot be less than minReplicas.
	//
	// +required
	MaxReplicas int32 `json:"maxReplicas" protobuf:"varint,3,opt,name=maxReplicas"`

	// metrics contains the specifications for which to use to calculate the
	// desired replica count (the maximum replica count across all metrics will
	// be used).  The desired replica count is calculated multiplying the
	// ratio between the target value and the current value by the current
	// number of pods.  Ergo, metrics used must decrease as the pod count is
	// increased, and vice-versa.  See the individual metric source types for
	// more information about how each type of metric must respond.
	// If not set, the default metric will be set to 80% average CPU utilization.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Metrics []autoscalingv2.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,4,rep,name=metrics"`

	// behavior configures the scaling behavior of the target
	// in both Up and Down directions (scaleUp and scaleDown fields respectively).
	// If not set, the default HPAScalingRules for scale up and scale down are used.
	// +optional
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty" protobuf:"bytes,5,opt,name=behavior"`
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
	Selector string `json:"selector,omitempty"`

	// Version indicates the version of the referenced, remote MCPServer.
	//
	// +optional
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
