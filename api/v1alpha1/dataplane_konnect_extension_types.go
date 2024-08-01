package v1alpha1

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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&DataPlaneKonnectExtension{}, &DataPlaneKonnectExtensionList{})
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kong;all
// +kubebuilder:subresource:status

// DataPlaneKonnectExtension is the Schema for the dataplanekonnectextension API,
// and is intended to be referenced as extension by the dataplane API.
// If a DataPlane successfully refers a DataPlaneKonnectExtension, the DataPlane
// deployment spec gets customized to include the konnect-related configuration.
type DataPlaneKonnectExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the DataPlaneKonnectExtension resource.
	Spec DataPlaneKonnectExtensionSpec `json:"spec,omitempty"`
	// Status is the status of the DataPlaneKonnectExtension resource.
	Status DataPlaneKonnectExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataPlaneKonnectExtensionList contains a list of DataPlaneKonnectExtension.
type DataPlaneKonnectExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPlaneKonnectExtension `json:"items"`
}

// DataPlaneKonnectExtensionSpec defines the desired state of DataPlaneKonnectExtension.
type DataPlaneKonnectExtensionSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this DataPlaneKonnectExtension is associated with.
	// +kubebuilder:validation:Required
	ControlPlaneRef configurationv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// ControlPlaneRegion is the region of the Konnect Control Plane.
	// ++kubebuilder:validation:Required
	ControlPlaneRegion string `json:"controlPlaneRegion"`

	// ServerURL is the URL of the Konnect server.
	// +kubebuilder:validation:Required
	ServerURL string `json:"serverURL"`

	// ClusterCertificateSecretName is a name of the Secret containing the Konnect Control Plane's cluster certificate.
	// +kubebuilder:validation:Required
	ClusterCertificateSecretName string `json:"clusterCertificateSecretName"`

	// ClusterDataPlaneLabels is a set of labels that will be applied to the Konnect DataPlane.
	// +optional
	ClusterDataPlaneLabels map[string]string `json:"clusterDataPlaneLabels,omitempty"`
}

// DataPlaneKonnectExtensionStatus defines the observed state of DataPlaneKonnectExtension.
type DataPlaneKonnectExtensionStatus struct {
	// DataPlaneRefs is the array  of DataPlane references this is associated with.
	// A new reference is set by the operator when this extension is associated with
	// a DataPlane through its extensions spec.
	//
	// +kube:validation:Optional
	DataPlaneRefs []NamespacedRef `json:"dataPlaneRefs,omitempty"`
}
