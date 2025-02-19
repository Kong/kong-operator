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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&DataPlaneMetricsExtension{}, &DataPlaneMetricsExtensionList{})
}

const (
	// DataPlaneMetricsExtensionKind holds the kind for the DataPlaneMetricsExtension.
	DataPlaneMetricsExtensionKind = "DataPlaneMetricsExtension"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kong;all
// +kubebuilder:subresource:status

// DataPlaneMetricsExtension holds the configuration for the DataPlane metrics extension.
// It can be attached to a ControlPlane using its spec.extensions.
// When attached it will make the ControlPlane configure its DataPlane with
// the specified metrics configuration.
// Additionally, it will also make the operator expose DataPlane's metrics
// enriched with metadata required for in-cluster Kubernetes autoscaling.
//
// NOTE: This is an enterprise feature. In order to use it you need to use
// the EE version of Kong Gateway Operator with a valid license.
// +apireference:kgo:include
// +kong:channels=gateway-operator
type DataPlaneMetricsExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataPlaneMetricsExtensionSpec   `json:"spec,omitempty"`
	Status DataPlaneMetricsExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataPlaneMetricsExtensionList contains a list of DataPlaneMetricsExtension.
// +apireference:kgo:include
type DataPlaneMetricsExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPlaneMetricsExtension `json:"items"`
}

// DataPlaneMetricsExtensionSpec defines the spec for the DataPlaneMetricsExtension.
// +apireference:kgo:include
type DataPlaneMetricsExtensionSpec struct {
	// ServiceSelector holds the service selector specifying the services
	// for which metrics should be collected.
	//
	// +kubebuilder:validation:Required
	ServiceSelector ServiceSelector `json:"serviceSelector"`

	// Config holds the configuration for the DataPlane metrics.
	//
	// +kube:validation:Optional
	Config MetricsConfig `json:"config,omitempty"`
}

// MetricsConfig holds the configuration for the DataPlane metrics.
// +apireference:kgo:include
type MetricsConfig struct {
	// Latency indicates whether latency metrics are enabled for the DataPlane.
	// This translates into deployed instances having `latency_metrics` option set
	// on the Prometheus plugin.
	//
	// +kubebuilder:default=false
	// +kube:validation:Optional
	Latency bool `json:"latency"`

	// Bandwidth indicates whether bandwidth metrics are enabled for the DataPlane.
	// This translates into deployed instances having `bandwidth_metrics` option set
	// on the Prometheus plugin.
	//
	// +kubebuilder:default=false
	// +kube:validation:Optional
	Bandwidth bool `json:"bandwidth"`

	// UpstreamHealth indicates whether upstream health metrics are enabled for the DataPlane.
	// This translates into deployed instances having `upstream_health_metrics` option set
	// on the Prometheus plugin.
	//
	// +kubebuilder:default=false
	// +kube:validation:Optional
	UpstreamHealth bool `json:"upstreamHealth"`

	// StatusCode indicates whether status code metrics are enabled for the DataPlane.
	// This translates into deployed instances having `status_code_metrics` option set
	// on the Prometheus plugin.
	//
	// +kubebuilder:default=false
	// +kube:validation:Optional
	StatusCode bool `json:"statusCode"`
}

// DataPlaneMetricsExtensionStatus defines the status of the DataPlaneMetricsExtension.
// +apireference:kgo:include
type DataPlaneMetricsExtensionStatus struct {
	// ControlPlaneRef is a reference to the ControlPlane that this is associated with.
	// This field is set by the operator when this extension is associated with
	// a ControlPlane through its extensions spec.
	// There can only be one ControlPlane associated with a given DataPlaneMetricsExtension.
	// When this is unset it means that the association has been removed.
	//
	// +kube:validation:Optional
	ControlPlaneRef *commonv1alpha1.NamespacedRef `json:"controlPlaneRef,omitempty"`
}

// ServiceSelector holds the service selector specification.
// +apireference:kgo:include
type ServiceSelector struct {
	// MatchNames holds the list of Services names to match.
	//
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:Required
	MatchNames []ServiceSelectorEntry `json:"matchNames,omitempty"`
}

// ServiceSelectorEntry holds the name of a service to match.
// +apireference:kgo:include
type ServiceSelectorEntry struct {
	// Name is the name of the service to match.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}
