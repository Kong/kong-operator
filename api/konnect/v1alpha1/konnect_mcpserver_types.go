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
// +kong:channels=kong-operator
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec MCPServerSpec `json:"spec,omitempty"`
}

// MCPServerSpec is the specification of the MCPServer resource.
type MCPServerSpec struct {
	// Mirror is the Konnect Mirror configuration.
	// It is only applicable for ControlPlanes that are created as Mirrors.
	//
	// +optional
	Mirror *MirrorSpec `json:"mirror,omitempty"`

	// Source represents the source type of the Konnect entity.
	//
	// +kubebuilder:validation:Enum=Mirror
	// +optional
	// +kubebuilder:default=Mirror
	Source *commonv1alpha1.EntitySource `json:"source,omitempty"`
}

// MCPServerList contains a list of MCPServer resources.
//
// +kubebuilder:object:root=true
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []MCPServer `json:"items"`
}
