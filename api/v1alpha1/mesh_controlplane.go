/*
Copyright 2024 Kong Inc.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&MeshControlPlane{}, &MeshControlPlaneList{})
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kocp,categories=kong;all
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Provisioned",description="The Resource is provisioned",type=string,JSONPath=`.status.conditions[?(@.type=='Provisioned')].status`

// MeshControlPlane is the Schema for the controlplanes API
// +apireference:kgo:include
type MeshControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshControlPlaneSpec `json:"spec,omitempty"`
	Status ControlPlaneStatus   `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MeshControlPlaneList contains a list of ControlPlane
// +apireference:kgo:include
type MeshControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MeshControlPlane `json:"items"`
}

// MeshControlPlaneSpec defines the desired state of ControlPlane
// +apireference:kgo:include
type MeshControlPlaneSpec struct {
	MeshControlPlaneOptions `json:",inline"`
}

// MeshControlPlaneOptions indicates the specific information needed to
// deploy and connect a ControlPlane to a DataPlane object.
// +apireference:kgo:include
type MeshControlPlaneOptions struct {
	// +optional
	Deployment MeshControlPlaneDeploymentOptions `json:"deployment"`
}

// MeshControlPlaneDeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the the number of replicas
// or pod options like container image and resource requirements.
// version, as well as Env variable overrides.
// +apireference:kgo:include
type MeshControlPlaneDeploymentOptions struct {
	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// ControlPlaneStatus defines the observed state of ControlPlane
// +apireference:kgo:include
type ControlPlaneStatus struct {
	// Conditions describe the current conditions of the Gateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Scheduled", status: "Unknown", reason:"NotReconciled", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the ControlPlane Status Conditions
func (c *MeshControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the ControlPlane Status Conditions
func (c *MeshControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}
