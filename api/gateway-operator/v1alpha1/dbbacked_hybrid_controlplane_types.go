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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DBBackedHybridControlPlane stands for a control plane role in the DB backed hybrid mode deployed on prem.
// In the DB backed hybrid mode, the control plane is responsible accepting configuration from KO spawned KIC instances
// and storing it in the database. The control plane is also responsible for propagating the configuration to the
// data plane instances. The control plane is not responsible for serving traffic, which is the responsibility of the
// data plane instances.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dbcp,categories=kong
// +kubebuilder:subresource:status
// +kong:channels=kong-operator
type DBBackedHybridControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DBBackedHybridControlPlaneSpec   `json:"spec,omitempty"`
	Status DBBackedHybridControlPlaneStatus `json:"status,omitempty"`
}

// DBBackedHybridControlPlaneSpec stands for the desired state of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneSpec struct{}

// DBBackedHybridControlPlaneStatus represents the observed state of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneStatus struct {
	// Conditions represent the latest available observations of a DBBackedHybridControlPlane's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
