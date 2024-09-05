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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/api/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&GatewayConfiguration{}, &GatewayConfigurationList{})
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kogc,categories=kong;all
// +kubebuilder:validation:XValidation:message="Extension not allowed for DataPlane config options",rule="has(self.spec.dataPlaneOptions.extensions) ? self.spec.dataPlaneOptions.extensions.all(e, e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneKonnectExtension') : true"

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
	DataPlaneOptions *GatewayConfigDataPlaneOptions `json:"dataPlaneOptions,omitempty"`

	// ControlPlaneOptions is the specification for configuration
	// overrides for ControlPlane resources that will be created for the Gateway.
	//
	// +optional
	ControlPlaneOptions *ControlPlaneOptions `json:"controlPlaneOptions,omitempty"`
}

// GatewayConfigDataPlaneOptions indicates the specific information needed to
// configure and deploy a DataPlane object.
type GatewayConfigDataPlaneOptions struct {
	// +optional
	Deployment DataPlaneDeploymentOptions `json:"deployment"`

	// +optional
	Network GatewayConfigDataPlaneNetworkOptions `json:"network"`

	// Extensions provide additional or replacement features for the DataPlane
	// resources to influence or enhance functionality.
	// NOTE: since we have one extension only (DataPlaneKonnectExtension), we limit the amount of extensions to 1.
	//
	// +optional
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=1
	Extensions []v1alpha1.ExtensionRef `json:"extensions,omitempty"`
}

// GatewayConfigDataPlaneNetworkOptions defines network related options for a DataPlane.
type GatewayConfigDataPlaneNetworkOptions struct {
	// Services indicates the configuration of Kubernetes Services needed for
	// the topology of various forms of traffic (including ingress, etc.) to
	// and from the DataPlane.
	Services *GatewayConfigDataPlaneServices `json:"services,omitempty"`
}

// GatewayConfigDataPlaneServices contains Services related DataPlane configuration.
type GatewayConfigDataPlaneServices struct {
	// Ingress is the Kubernetes Service that will be used to expose ingress
	// traffic for the DataPlane. Here you can determine whether the DataPlane
	// will be exposed outside the cluster (e.g. using a LoadBalancer type
	// Services) or only internally (e.g. ClusterIP), and inject any additional
	// annotations you need on the service (for instance, if you need to
	// influence a cloud provider LoadBalancer configuration).
	//
	// +optional
	Ingress *GatewayConfigServiceOptions `json:"ingress,omitempty"`
}

// GatewayConfigServiceOptions is used to includes options to customize the ingress service,
// such as the annotations.
type GatewayConfigServiceOptions struct {
	ServiceOptions `json:",inline"`
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
