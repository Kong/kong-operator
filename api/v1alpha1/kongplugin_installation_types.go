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
//+kubebuilder:printcolumn:name="Accepted",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Accepted')].status`

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

// KongPluginInstallationSpec provides the information necessary to retrieve and install a Kong custom plugin.
type KongPluginInstallationSpec struct {

	// The image is an OCI image URL for a packaged custom Kong plugin.
	//
	//+kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullSecretRef is a reference to a Kubernetes Secret containing credentials necessary to pull the OCI image
	// in Image. It must follow the format in https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry.
	// It is optional. If the image is public, omit this field.
	//
	//+optional
	ImagePullSecretRef *corev1.SecretReference `json:"imagePullSecretRef,omitempty"`
}

// KongPluginInstallationStatus defines the observed state of KongPluginInstallation.
type KongPluginInstallationStatus struct {
	// Conditions describe the current conditions of this KongPluginInstallation.
	//
	//+listType=map
	//+listMapKey=type
	//+kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// UnderlyingConfigMapName is the name of the ConfigMap that contains the plugin's content.
	// It is set when the plugin is successfully fetched and unpacked.
	//
	//+optional
	UnderlyingConfigMapName string `json:"underlyingConfigMapName,omitempty"`
}

// The following are KongPluginInstallation specific types for
// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#condition-v1-meta fields

// KongPluginInstallationConditionType is the type for Conditions in a KongPluginInstallation's
// Status.Conditions array.
type KongPluginInstallationConditionType string

// KongPluginInstallationConditionReason is a reason for the KongPluginInstallation condition's last transition.
type KongPluginInstallationConditionReason string

const (
	// This condition indicates whether the controller has fetched the plugin image
	// and made it available for use as a specific custom Kong Plugin.
	//
	// It is a positive-polarity summary condition, and so should always be
	// present on the resource with ObservedGeneration set.
	//
	// It should be set to Unknown if the controller performs updates to the
	// status before it has all the information it needs to be able to determine
	// if the condition is true (e.g. haven't started the download yet).
	//
	// Possible reasons for this condition to be "True" are:
	//
	// * "Ready"
	//
	// Possible reasons for this condition to be "False" are:
	//
	// * "Pending"
	// * "Failed"
	//
	// Possible reasons for this condition to be "Unknown" are:
	//
	// * "Pending".
	//
	KongPluginInstallationConditionStatusAccepted KongPluginInstallationConditionType = "Accepted"

	// KongPluginInstallationReasonReady indicates that the controller has downloaded the plugin
	// and can install it on a DataPlane or Gateway.
	KongPluginInstallationReasonReady KongPluginInstallationConditionReason = "Ready"

	// KongPluginInstallationReasonFailed is used with the "Accepted" condition type when
	// the KongPluginInstallation can't be fetched e.g. image can't be fetched due to lack
	// of permissions or the image doesn't exist. It's a state that can't be recovered without
	// manual intervention.
	// More details can be obtained from the condition's message.
	KongPluginInstallationReasonFailed KongPluginInstallationConditionReason = "Failed"

	// KongPluginInstallationReasonPending is used with the "Accepted" condition type when the requested
	// controller has started processing the KongPluginInstallation, but it hasn't finished yet, e.g.
	// fetching and unpacking the image is in progress.
	KongPluginInstallationReasonPending KongPluginInstallationConditionReason = "Pending"
)
