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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&KongPluginInstallation{}, &KongPluginInstallationList{})
}

//+genclient
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=kpi,categories=kong;all
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// KongPluginInstallation allows using a custom Kong Plugin distributed as a container image available in a registry.
// Such a plugin can be associated with GatewayConfiguration or DataPlane to be available for particular Kong Gateway
// and configured with KongPlugin CRD.
type KongPluginInstallation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KongPluginInstallationSpec   `json:"spec,omitempty"`
	Status KongPluginInstallationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// KongPluginInstallationList contains a list of KongPluginInstallation.
type KongPluginInstallationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongPluginInstallation `json:"items"`
}

// KongPluginInstallationSpec defines the desired state of KongPluginInstallation.
type KongPluginInstallationSpec struct {

	// The image is an OCI image URL for a packaged Custom Kong Plugin.
	//
	//+kubebuilder:validation:Required
	Image string `json:"image"`

	// SecretRef allows referring specific Kubernetes Secret to use for OCI registry authentication for pulling an image with Custom Kong Plugin.
	// The Secret format should follow https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry.
	// When the field is omitted it is assumed that the image is public and can be fetched without providing credentials.
	//
	//+optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`
}

// KongPluginInstallationStatus defines the observed state of KongPluginInstallation.
type KongPluginInstallationStatus struct {
	// Conditions describe the current conditions of this KongPluginInstallation.
	//
	//+listType=map
	//+listMapKey=type
	//+kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongPluginInstallationConditionType is the type for status conditions on
// KongPluginInstallation resources. This type should be used with the
// KongPluginInstallationStatus.Conditions field.
type KongPluginInstallationConditionType string

// KongPluginInstallationConditionReason defines the set of reasons that explain why
// a particular KongPluginInstallation condition type has been raised.
type KongPluginInstallationConditionReason string

const (
	// This condition indicates whether the controller has fetched the plugin image
	// and made it available for use as a specific Custom Kong Plugin.
	//
	// It is a positive-polarity summary condition, and so should always be
	// present on the resource with ObservedGeneration set.
	//
	// It should be set to Unknown if the controller performs updates to the
	// status before it has all the information it needs to be able to determine
	// if the condition is true.
	//
	// Possible reasons for this condition to be "True" are:
	//
	// * "Ready"
	//
	// Possible reasons for this condition to be "False" are:
	//
	// * "Failed"
	// * "Pending"
	//
	// Possible reasons for this condition to be "Unknown" are:
	//
	// * "Pending".
	//
	KongPluginInstallationConditionStatusAccepted KongPluginInstallationConditionType = "Ready"

	// KongPluginInstallationReasonReady is used with the "Ready" condition when
	// the condition is "True".
	KongPluginInstallationReasonReady KongPluginInstallationConditionReason = "Ready"

	// KongPluginInstallationReasonFailed is used with the "Ready" condition when
	// the KongPluginInstallation can't be configured e.g. image can't be fetched.
	// More details can be obtained from the condition's message.
	KongPluginInstallationReasonFailed KongPluginInstallationConditionReason = "Failed"

	// KongPluginInstallationReasonPending is used with the "Ready" condition when the requested
	// controller has started processing the KongPluginInstallation, but it hasn't finished yet.
	KongPluginInstallationReasonPending KongPluginInstallationConditionReason = "Pending"
)
