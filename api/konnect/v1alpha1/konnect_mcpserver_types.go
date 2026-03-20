/*
Copyright 2026 Kong Inc.
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

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func init() {
	SchemeBuilder.Register(&MCPServer{}, &MCPServerList{})
}

// MCPServer is the Schema for the MCPServer API.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kong:channels=kong-operator
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec MCPServerSpec `json:"spec,omitempty"`

	// +optional
	Status MCPServerStatus `json:"status,omitempty"`
}

// MCPServerSpec is the specification of the MCPServer resource.
type MCPServerSpec struct {
	// Mirror is the Konnect Mirror configuration.
	// It is only applicable for ControlPlanes that are created as Mirrors.
	//
	// +required
	Mirror MirrorSpec `json:"mirror"`

	// Source represents the source type of the Konnect entity.
	//
	// +kubebuilder:validation:Enum=Mirror
	// +optional
	// +kubebuilder:default=Mirror
	Source *commonv1alpha1.EntitySource `json:"source,omitempty"`

	// KonnectConfiguration contains the Konnect configuration for the MCP server.
	//
	// +required
	KonnectConfiguration konnectv1alpha2.KonnectConfiguration `json:"konnect"`
}

// MCPServerStatus defines the observed state of MCPServer.
type MCPServerStatus struct {
	// Conditions describe the current conditions of the MCPServer.
	//
	// Known condition types are:
	//
	// * "Programmed"
	// * "Mirrored"
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	konnectv1alpha2.KonnectEntityStatus `json:",inline"` //nolint:embeddedstructfieldcheck
}

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (s *MCPServer) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.KonnectAPIAuthConfigurationRef {
	return s.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// MCPServerList contains a list of MCPServer resources.
//
// +kubebuilder:object:root=true
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []MCPServer `json:"items"`
}
