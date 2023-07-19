/*
Copyright 2022 Kong Inc.

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

	"github.com/kong/gateway-operator/apis/v1beta1"
)

func init() {
	SchemeBuilder.Register(&GatewayConfiguration{}, &GatewayConfigurationList{})
}

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kgc,categories=kong;all

// GatewayConfiguration is the Schema for the gatewayconfigurations API
type GatewayConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayConfigurationSpec   `json:"spec,omitempty"`
	Status GatewayConfigurationStatus `json:"status,omitempty"`
}

// GatewayConfigurationSpec defines the desired state of GatewayConfiguration
type GatewayConfigurationSpec struct {
	// DataPlaneOptions is the specification for configuration
	// overrides for DataPlane resources that will be created for the Gateway.
	//
	// +optional
	DataPlaneOptions *v1beta1.DataPlaneOptions `json:"dataPlaneOptions,omitempty"`

	// ControlPlaneOptions is the specification for configuration
	// overrides for ControlPlane resources that will be created for the Gateway.
	//
	// +optional
	ControlPlaneOptions *ControlPlaneOptions `json:"controlPlaneOptions,omitempty"`
}

// GatewayConfigurationStatus defines the observed state of GatewayConfiguration
type GatewayConfigurationStatus struct {
	// Conditions describe the current conditions of the GatewayConfigurationStatus.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true

// GatewayConfigurationList contains a list of GatewayConfiguration
type GatewayConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayConfiguration `json:"items"`
}

// GetConditions retrieves the GatewayConfiguration Status Condition
func (g *GatewayConfiguration) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

// SetConditions sets the GatewayConfiguration Status Condition
func (g *GatewayConfiguration) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}
