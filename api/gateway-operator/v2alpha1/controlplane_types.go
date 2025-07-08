/*
Copyright 2025 Kong Inc.

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

package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func init() {
	SchemeBuilder.Register(&ControlPlane{}, &ControlPlaneList{})
}

// ControlPlane is the Schema for the controlplanes API
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kocp,categories=kong;all
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +apireference:kgo:include
// +kong:channels=gateway-operator
type ControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the ControlPlane resource.
	Spec ControlPlaneSpec `json:"spec,omitempty"`

	// Status is the status of the ControlPlane resource.
	//
	// +optional
	Status ControlPlaneStatus `json:"status,omitempty"`
}

// ControlPlaneList contains a list of ControlPlane
//
// +kubebuilder:object:root=true
// +apireference:kgo:include
type ControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlane `json:"items"`
}

// ControlPlaneSpec defines the desired state of ControlPlane
//
// +apireference:kgo:include
type ControlPlaneSpec struct {
	// DataPlane designates the target data plane to configure.
	//
	// It can be:
	// - a name of a DataPlane resource that is managed by the operator,
	// - a DataPlane that is managed by the owner of the ControlPlane (e.g. a Gateway resource)
	//
	// +required
	DataPlane ControlPlaneDataPlaneTarget `json:"dataplane"`

	ControlPlaneOptions `json:",inline"`

	// Extensions provide additional or replacement features for the ControlPlane
	// resources to influence or enhance functionality.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:message="Extension not allowed for ControlPlane",rule="self.all(e, (e.group == 'konnect.konghq.com' && e.kind == 'KonnectExtension') || (e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneMetricsExtension'))"
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`
}

// ControlPlaneOptions indicates the specific information needed to
// deploy and connect a ControlPlane to a DataPlane object.
//
// +apireference:kgo:include
type ControlPlaneOptions struct {
	// IngressClass enables support for the Ingress resources and indicates
	// which Ingress resources this ControlPlane should be responsible for.
	//
	// If omitted, Ingress resources will not be supported by the ControlPlane.
	//
	// +optional
	IngressClass *string `json:"ingressClass,omitempty"`

	// WatchNamespaces indicates the namespaces to watch for resources.
	//
	// +optional
	// +kubebuilder:default={type: all}
	WatchNamespaces *operatorv1beta1.WatchNamespaces `json:"watchNamespaces,omitempty"`

	// FeatureGates is a list of feature gates that are enabled for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	FeatureGates []ControlPlaneFeatureGate `json:"featureGates,omitempty"`

	// Controllers defines the controllers that are enabled for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Controllers []ControlPlaneController `json:"controllers,omitempty"`

	// GatewayDiscovery defines the configuration for the Gateway Discovery feature.
	//
	// +optional
	GatewayDiscovery *ControlPlaneGatewayDiscovery `json:"gatewayDiscovery,omitempty"`
}

// ControlPlaneDataPlaneTarget defines the target for the DataPlane that the ControlPlane
// is responsible for configuring.
//
// +kubebuilder:validation:XValidation:message="Ref has to be provided when type is set to ref",rule="self.type != 'ref' || has(self.ref)"
// +kubebuilder:validation:XValidation:message="Ref cannot be provided when type is set to managedByOwner",rule="self.type != 'managedByOwner' || !has(self.ref)"
type ControlPlaneDataPlaneTarget struct {
	// Type indicates the type of the DataPlane target.
	//
	// +required
	// +kubebuilder:validation:Enum=ref;managedByOwner
	Type ControlPlaneDataPlaneTargetType `json:"type"`

	// Ref is the name of the DataPlane to configure.
	//
	// +optional
	Ref *ControlPlaneDataPlaneTargetRef `json:"ref,omitempty"`
}

// ControlPlaneDataPlaneTargetType defines the type of the DataPlane target
// that the ControlPlane is responsible for configuring.
type ControlPlaneDataPlaneTargetType string

const (
	// ControlPlaneDataPlaneTargetRefType indicates that the DataPlane target is a ref
	// of a DataPlane resource managed by the operator.
	// This is used for configuring DataPlanes that are managed by the operator.
	ControlPlaneDataPlaneTargetRefType ControlPlaneDataPlaneTargetType = "ref"

	// ControlPlaneDataPlaneTargetManagedByType indicates that the DataPlane target
	// is managed by the owner of the ControlPlane.
	// This is the case when using a Gateway resource to manage the DataPlane
	// and the ControlPlane is responsible for configuring it.
	ControlPlaneDataPlaneTargetManagedByType ControlPlaneDataPlaneTargetType = "managedByOwner"
)

// ControlPlaneDataPlaneTargetRef defines the reference to a DataPlane resource
// that the ControlPlane is responsible for configuring.
type ControlPlaneDataPlaneTargetRef struct {
	// Ref is the name of the DataPlane to configure.
	//
	// +required
	Name string `json:"name"`
}

// ControlPlaneGatewayDiscovery defines the configuration for the Gateway Discovery
// feature of the ControlPlane.
type ControlPlaneGatewayDiscovery struct {
	// ReadinessCheckInterval defines the interval at which the ControlPlane
	// checks the readiness of the DataPlanes it is responsible for.
	// If not specified, the default interval as defined by the operator will be used.
	//
	// +optional
	ReadinessCheckInterval *metav1.Duration `json:"readinessCheckInterval,omitempty"`

	// ReadinessCheckTimeout defines the timeout for the DataPlane readiness check.
	// If not specified, the default interval as defined by the operator will be used.
	//
	// +optional
	ReadinessCheckTimeout *metav1.Duration `json:"readinessCheckTimeout,omitempty"`
}

// ControllerState defines the state of a feature gate.
type ControllerState string

const (
	// ControllerStateEnabled indicates that the feature gate is enabled.
	ControllerStateEnabled ControllerState = "enabled"
	// ControllerStateDisabled indicates that the feature gate is disabled.
	ControllerStateDisabled ControllerState = "disabled"
)

// ControlPlaneController defines a controller state for the ControlPlane.
// It overrides the default behavior as defined in the deployed operator version.
//
// +apireference:kgo:include
type ControlPlaneController struct {
	// Name is the name of the controller.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// State indicates whether the feature gate is enabled or disabled.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	State ControllerState `json:"state"`
}

// FeatureGateState defines the state of a feature gate.
type FeatureGateState string

const (
	// FeatureGateStateEnabled indicates that the feature gate is enabled.
	FeatureGateStateEnabled FeatureGateState = "enabled"
	// FeatureGateStateDisabled indicates that the feature gate is disabled.
	FeatureGateStateDisabled FeatureGateState = "disabled"
)

// ControlPlaneFeatureGate defines a feature gate state for the ControlPlane.
// It overrides the default behavior as defined in the deployed operator version.
//
// +apireference:kgo:include
type ControlPlaneFeatureGate struct {
	// Name is the name of the feature gate.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// State indicates whether the feature gate is enabled or disabled.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	State FeatureGateState `json:"state"`
}

// ControlPlaneStatus defines the observed state of ControlPlane
//
// +apireference:kgo:include
type ControlPlaneStatus struct {
	// Conditions describe the current conditions of the Gateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Scheduled", status: "Unknown", reason:"NotReconciled", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// FeatureGates is a list of effective feature gates for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	FeatureGates []ControlPlaneFeatureGate `json:"featureGates,omitempty"`

	// Controllers is a list of enabled and disabled controllers for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Controllers []ControlPlaneController `json:"controllers,omitempty"`
}

// GetConditions returns the ControlPlane Status Conditions
func (c *ControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the ControlPlane Status Conditions
func (c *ControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetExtensions retrieves the ControlPlane Extensions
func (c *ControlPlane) GetExtensions() []commonv1alpha1.ExtensionRef {
	return c.Spec.Extensions
}
