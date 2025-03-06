package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
)

func init() {
	SchemeBuilder.Register(&KonnectGatewayControlPlane{}, &KonnectGatewayControlPlaneList{})
}

// KonnectGatewayControlPlane is the Schema for the KonnectGatewayControlplanes API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:validation:XValidation:message="spec.konnect.authRef is immutable when an entity is already Programmed", rule="!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True') ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef"
// +kubebuilder:validation:XValidation:message="spec.konnect.authRef is immutable when an entity refers to a Valid API Auth Configuration", rule="!self.status.conditions.exists(c, c.type == 'APIAuthValid' && c.status == 'True') ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectGatewayControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KonnectGatewayControlPlane.
	Spec KonnectGatewayControlPlaneSpec `json:"spec,omitempty"`

	// Status defines the observed state of KonnectGatewayControlPlane.
	Status KonnectGatewayControlPlaneStatus `json:"status,omitempty"`
}

// KonnectGatewayControlPlaneSpec defines the desired state of KonnectGatewayControlPlane.
// +kubebuilder:validation:XValidation:message="spec.labels must not have more than 40 entries", rule="has(self.labels) ? size(self.labels) <= 40 : true"
// +kubebuilder:validation:XValidation:message="spec.labels keys must be of length 1-63 characters", rule="has(self.labels) ? self.labels.all(key, size(key) >= 1 && size(key) <= 63) : true"
// +kubebuilder:validation:XValidation:message="spec.labels values must be of length 1-63 characters", rule="has(self.labels) ? self.labels.all(key, size(self.labels[key]) >= 1 && size(self.labels[key]) <= 63) : true"
// +kubebuilder:validation:XValidation:message="spec.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'", rule="has(self.labels) ? self.labels.all(key, !key.startsWith('k8s') && !key.startsWith('kong') && !key.startsWith('konnect') && !key.startsWith('mesh') && !key.startsWith('kic') && !key.startsWith('_') && !key.startsWith('insomnia')) : true"
// +kubebuilder:validation:XValidation:message="spec.labels keys must satisfy the '^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$' pattern", rule="has(self.labels) ? self.labels.all(key, key.matches('^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$')) : true"
// +kubebuilder:validation:XValidation:message="when specified, spec.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'", rule="!has(self.cluster_type) ? true : ['CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE', 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'].exists(ct, ct == self.cluster_type)"
// +kubebuilder:validation:XValidation:message="spec.members is only applicable for ControlPlanes that are created as groups", rule="(has(self.cluster_type) && self.cluster_type != 'CLUSTER_TYPE_CONTROL_PLANE_GROUP') ? !has(self.members) : true"
// +kubebuilder:validation:XValidation:message="spec.cluster_type is immutable", rule="!has(self.cluster_type) ? !has(oldSelf.cluster_type) : self.cluster_type == oldSelf.cluster_type"
// +apireference:kgo:include
type KonnectGatewayControlPlaneSpec struct {
	sdkkonnectcomp.CreateControlPlaneRequest `json:",inline"`

	// Members is a list of references to the KonnectGatewayControlPlaneMembers that are part of this control plane group.
	// Only applicable for ControlPlanes that are created as groups.
	Members []corev1.LocalObjectReference `json:"members,omitempty"`

	KonnectConfiguration KonnectConfiguration `json:"konnect,omitempty"`
}

// KonnectGatewayControlPlaneStatus defines the observed state of KonnectGatewayControlPlane.
// +apireference:kgo:include
type KonnectGatewayControlPlaneStatus struct {
	KonnectEntityStatus `json:",inline"`

	// Endpoints defines the Konnect endpoints for the control plane.
	// They are required by the DataPlane to be properly configured in
	// Konnect and connect to the control plane.
	//
	// +kubebuilder:validation:Optional
	Endpoints *KonnectEndpoints `json:"konnectEndpoints,omitempty"`

	// Conditions describe the current conditions of the KonnectGatewayControlPlane.
	//
	// Known condition types are:
	//
	// * "Programmed"
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KonnectEndpoints defines the Konnect endpoints for the control plane.
type KonnectEndpoints struct {
	// TelemetryEndpoint is the endpoint for telemetry.
	//
	// +kubebuilder:validation:Required
	TelemetryEndpoint string `json:"telemetry"`

	// ControlPlaneEndpoint is the endpoint for the control plane.
	//
	// +kubebuilder:validation:Required
	ControlPlaneEndpoint string `json:"controlPlane"`
}

// GetKonnectLabels gets the Konnect Labels from object's spec.
func (c *KonnectGatewayControlPlane) GetKonnectLabels() map[string]string {
	return c.Spec.Labels
}

// SetKonnectLabels sets the Konnect Labels in object's spec.
func (c *KonnectGatewayControlPlane) SetKonnectLabels(labels map[string]string) {
	c.Spec.Labels = labels
}

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (c *KonnectGatewayControlPlane) GetKonnectAPIAuthConfigurationRef() KonnectAPIAuthConfigurationRef {
	return c.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// KonnectGatewayControlPlaneList contains a list of KonnectGatewayControlPlane.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KonnectGatewayControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectGatewayControlPlane `json:"items"`
}
