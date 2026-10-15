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

// OnPremAIGateway is the Schema for the on-prem AI Gateway control planes API.
// It acts as the non-Konnect control plane for AIGatewayDataPlane: it does not
// own any Deployment or Pod itself, it aggregates configuration targeting it
// and pushes it to the data planes that reference it.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=onpremaigw,categories=kong
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type OnPremAIGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of OnPremAIGateway.
	//
	// +optional
	Spec OnPremAIGatewaySpec `json:"spec,omitempty"`

	// Status defines the observed state of OnPremAIGateway.
	//
	// +optional
	Status OnPremAIGatewayStatus `json:"status,omitempty"`
}

// OnPremAIGatewayList contains a list of OnPremAIGateway.
//
// +kubebuilder:object:root=true
type OnPremAIGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OnPremAIGateway `json:"items"`
}

// OnPremAIGatewaySpec defines the desired state of OnPremAIGateway.
type OnPremAIGatewaySpec struct {
	// DataPlaneSync defines the configuration for syncing configuration
	// to the data planes referencing this OnPremAIGateway.
	//
	// +optional
	DataPlaneSync *OnPremAIGatewayDataPlaneSync `json:"dataplaneSync,omitempty"`
}

// OnPremAIGatewayDataPlaneSync defines the configuration for syncing
// configuration to the data planes referencing this OnPremAIGateway.
type OnPremAIGatewayDataPlaneSync struct {
	// IntervalSeconds is the interval between two rounds of syncing configuration
	// with data planes, in seconds.
	// If not specified, the default interval as defined by the operator will be used.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	IntervalSeconds *int32 `json:"intervalSeconds,omitempty"`

	// TimeoutSeconds is the timeout of a single run of syncing configuration with
	// data planes, in seconds.
	// If not specified, no timeout is applied to a sync run.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

// OnPremAIGatewayStatus defines the observed state of OnPremAIGateway.
type OnPremAIGatewayStatus struct {
	// Conditions describe the status of the OnPremAIGateway.
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Ready", status: "Unknown", reason: "Pending", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ConfigHash is the hash of the configuration that was last pushed to the
	// data planes referencing this OnPremAIGateway.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=64
	ConfigHash string `json:"configHash,omitempty"`
}
