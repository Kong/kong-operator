package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&KonnectGatewayControlPlane{}, &KonnectGatewayControlPlaneList{})
}

// KonnectGatewayControlPlane is the Schema for the KonnectGatewayControlplanes API.
//
// +genclient
// +kubebuilder:deprecatedversion:warning="This API version has been deprecated in favor of v1alpha2 and it will be removed in the future."
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
	//
	// +optional
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
// +kubebuilder:validation:XValidation:message="cloud_gateway cannot be set for cluster_type 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'", rule="has(self.cluster_type) && self.cluster_type == 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER' ? !has(self.cloud_gateway) : true"
// +kubebuilder:validation:XValidation:message="createControlPlaneRequest fields cannot be set for type Mirror", rule="self.source == 'Mirror' ? !has(self.name) && !has(self.description) && !has(self.cluster_type) && !has(self.auth_type) && !has(self.cloud_gateway) && !has(self.proxy_urls) && !has(self.labels) : true"
// +kubebuilder:validation:XValidation:message="spec.source is immutable", rule="self.source == oldSelf.source"
// +kubebuilder:validation:XValidation:message="mirror field must be set for type Mirror", rule="self.source == 'Mirror' ? has(self.mirror) : true"
// +kubebuilder:validation:XValidation:message="mirror field cannot be set for type Origin", rule="self.source == 'Origin' ? !has(self.mirror) : true"
// +kubebuilder:validation:XValidation:message="Name must be set for type Origin", rule="self.source == 'Origin' ? has(self.name) : true"
// +apireference:kgo:include
type KonnectGatewayControlPlaneSpec struct {
	CreateControlPlaneRequest `json:",inline"`

	// Mirror is the Konnect Mirror configuration.
	// It is only applicable for ControlPlanes that are created as Mirrors.
	//
	// +optional
	Mirror *MirrorSpec `json:"mirror,omitempty"`

	// Source represents the source type of the Konnect entity.
	//
	// +kubebuilder:validation:Enum=Origin;Mirror
	// +optional
	// +kubebuilder:default=Origin
	Source *commonv1alpha1.EntitySource `json:"source,omitempty"`

	// Members is a list of references to the KonnectGatewayControlPlaneMembers that are part of this control plane group.
	// Only applicable for ControlPlanes that are created as groups.
	//
	// +optional
	Members []corev1.LocalObjectReference `json:"members,omitempty"`

	// KonnectConfiguration contains the Konnect configuration for the control plane.
	//
	// +optional
	KonnectConfiguration KonnectConfiguration `json:"konnect,omitempty"`
}

// MirrorSpec contains the Konnect Mirror configuration.
type MirrorSpec struct {
	// Konnect contains the KonnectID of the KonnectGatewayControlPlane that
	// is mirrored.
	//
	// +required
	Konnect MirrorKonnect `json:"konnect"`
}

// MirrorKonnect contains the Konnect Mirror configuration.
type MirrorKonnect struct {
	// ID is the ID of the Konnect entity. It can be set only in case
	// the ControlPlane type is Mirror.
	//
	// +required
	ID commonv1alpha1.KonnectIDType `json:"id"`
}

// CreateControlPlaneRequest - The request schema for the create control plane request.
type CreateControlPlaneRequest struct {
	// The name of the control plane.
	// +optional
	Name *string `json:"name,omitempty"`
	// The description of the control plane in Konnect.
	// +optional
	Description *string `json:"description,omitempty"`
	// The ClusterType value of the cluster associated with the Control Plane.
	// +optional
	ClusterType *sdkkonnectcomp.CreateControlPlaneRequestClusterType `json:"cluster_type,omitempty"`
	// The auth type value of the cluster associated with the Runtime Group.
	// +optional
	AuthType *sdkkonnectcomp.AuthType `json:"auth_type,omitempty"`
	// Whether this control-plane can be used for cloud-gateways.
	// +optional
	CloudGateway *bool `json:"cloud_gateway,omitempty"`
	// Array of proxy URLs associated with reaching the data-planes connected to a control-plane.
	// +optional
	ProxyUrls []sdkkonnectcomp.ProxyURL `json:"proxy_urls,omitempty"`
	// Labels store metadata of an entity that can be used for filtering an entity list or for searching across entity types.
	//
	// Keys must be of length 1-63 characters, and cannot start with "kong", "konnect", "mesh", "kic", or "_".
	//
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// KonnectGatewayControlPlaneStatus defines the observed state of KonnectGatewayControlPlane.
// +apireference:kgo:include
type KonnectGatewayControlPlaneStatus struct {
	KonnectEntityStatus `json:",inline"`

	// Endpoints defines the Konnect endpoints for the control plane.
	// They are required by the DataPlane to be properly configured in
	// Konnect and connect to the control plane.
	//
	// +optional
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
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KonnectEndpoints defines the Konnect endpoints for the control plane.
type KonnectEndpoints struct {
	// TelemetryEndpoint is the endpoint for telemetry.
	//
	// +required
	TelemetryEndpoint string `json:"telemetry"`

	// ControlPlaneEndpoint is the endpoint for the control plane.
	//
	// +required
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
