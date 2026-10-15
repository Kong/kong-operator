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

// OnPremEventGateway is the Schema for the on-prem Event Gateway control planes API.
// It stands for one Event Gateway whose control plane is gw-admin, running in
// the cluster, instead of Konnect: Kong Operator pushes the configuration of
// the entities that reference it to gw-admin, and the data planes pull it
// from there.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=onpremegw,categories=kong
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="Service",description="The gw-admin Service",type=string,JSONPath=`.spec.gwAdmin.service.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type OnPremEventGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of OnPremEventGateway.
	//
	// +required
	Spec OnPremEventGatewaySpec `json:"spec,omitzero"`

	// Status defines the observed state of OnPremEventGateway.
	//
	// +optional
	Status OnPremEventGatewayStatus `json:"status,omitempty"`
}

// OnPremEventGatewayList contains a list of OnPremEventGateway.
//
// +kubebuilder:object:root=true
type OnPremEventGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OnPremEventGateway `json:"items"`
}

// OnPremEventGatewaySpec defines the desired state of OnPremEventGateway.
type OnPremEventGatewaySpec struct {
	// GWAdmin configures how Kong Operator reaches the gw-admin of this gateway.
	//
	// +required
	GWAdmin GWAdmin `json:"gwAdmin,omitzero"`

	// TODO: the spec is not final. Fields, such as the TLS settings of the
	// connection to gw-admin, are added as the implementation of the on-prem
	// Event Gateway goes on.
}

// GWAdmin configures how Kong Operator reaches gw-admin.
type GWAdmin struct {
	// Service is the Service in front of the gw-admin pods, in the namespace
	// of the OnPremEventGateway. Kong Operator pushes the configuration to
	// each pod of this Service.
	//
	// +required
	Service GWAdminService `json:"service,omitzero"`
}

// GWAdminService identifies the gw-admin Service and its port.
type GWAdminService struct {
	// Name is the name of the Service.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$')",message="name must be a valid DNS-1035 label: lowercase alphanumeric characters or '-', starting with a letter and ending with an alphanumeric character"
	Name string `json:"name,omitempty"`

	// PortName is the name of the Service port gw-admin serves its API on.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && self.matches('[a-z]') && !self.contains('--')",message="portName must be a valid port name: lowercase alphanumeric characters or '-', with at least one letter, no consecutive '-', starting and ending with an alphanumeric character"
	PortName string `json:"portName,omitempty"`
}

// OnPremEventGatewayStatus defines the observed state of OnPremEventGateway.
type OnPremEventGatewayStatus struct {
	// Conditions describe the status of the OnPremEventGateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ServerURL is the address the data planes of this gateway use to reach
	// gw-admin.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https' && url(self).getHostname() != ''",message="serverURL must be an https URL with a host"
	ServerURL string `json:"serverURL,omitempty"`

	// ObservedGeneration is the most recent generation observed.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}
