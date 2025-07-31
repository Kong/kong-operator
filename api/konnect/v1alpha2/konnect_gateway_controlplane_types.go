package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&KonnectGatewayControlPlane{}, &KonnectGatewayControlPlaneList{})
}

// KonnectGatewayControlPlane is the Schema for the KonnectGatewayControlplanes API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
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
//
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.labels must not have more than 40 entries", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.labels) ? size(self.createControlPlaneRequest.labels) <= 40 : true"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.labels keys must be of length 1-63 characters", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.labels) ? self.createControlPlaneRequest.labels.all(key, size(key) >= 1 && size(key) <= 63) : true"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.labels values must be of length 1-63 characters", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.labels) ? self.createControlPlaneRequest.labels.all(key, size(self.createControlPlaneRequest.labels[key]) >= 1 && size(self.createControlPlaneRequest.labels[key]) <= 63) : true"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.labels keys must not start with 'k8s', 'kong', 'konnect', 'mesh', 'kic', 'insomnia' or '_'", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.labels) ? self.createControlPlaneRequest.labels.all(key, !key.startsWith('k8s') && !key.startsWith('kong') && !key.startsWith('konnect') && !key.startsWith('mesh') && !key.startsWith('kic') && !key.startsWith('_') && !key.startsWith('insomnia')) : true"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.labels keys must satisfy the '^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$' pattern", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.labels) ? self.createControlPlaneRequest.labels.all(key, key.matches('^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$')) : true"
// +kubebuilder:validation:XValidation:message="spec.members is only applicable for ControlPlanes that are created as groups", rule="(has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.cluster_type) && self.createControlPlaneRequest.cluster_type != 'CLUSTER_TYPE_CONTROL_PLANE_GROUP') ? !has(self.members) : true"
// +kubebuilder:validation:XValidation:message="when specified, spec.createControlPlaneRequest.cluster_type must be one of 'CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE' or 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'", rule="!has(self.createControlPlaneRequest) || !has(self.createControlPlaneRequest.cluster_type) ? true : ['CLUSTER_TYPE_CONTROL_PLANE_GROUP', 'CLUSTER_TYPE_CONTROL_PLANE', 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'].exists(ct, ct == self.createControlPlaneRequest.cluster_type)"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.cluster_type is immutable", rule="!has(self.createControlPlaneRequest) && !has(oldSelf.createControlPlaneRequest) ? true : (has(self.createControlPlaneRequest) && has(oldSelf.createControlPlaneRequest) && (!has(self.createControlPlaneRequest.cluster_type) ? !has(oldSelf.createControlPlaneRequest.cluster_type) : self.createControlPlaneRequest.cluster_type == oldSelf.createControlPlaneRequest.cluster_type))"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest.cloud_gateway cannot be set for spec.createControlPlaneRequest.cluster_type 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER'", rule="has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.cluster_type) && self.createControlPlaneRequest.cluster_type == 'CLUSTER_TYPE_K8S_INGRESS_CONTROLLER' ? !has(self.createControlPlaneRequest.cloud_gateway) : true"
// +kubebuilder:validation:XValidation:message="createControlPlaneRequest fields cannot be set for type Mirror", rule="has(self.source) && self.source == 'Mirror' ? !has(self.createControlPlaneRequest) : true"
// +kubebuilder:validation:XValidation:message="spec.source is immutable", rule="!has(self.source) && !has(oldSelf.source) ? true : self.source == oldSelf.source"
// +kubebuilder:validation:XValidation:message="mirror field must be set for type Mirror", rule="self.source == 'Mirror' ? has(self.mirror) : true"
// +kubebuilder:validation:XValidation:message="mirror field cannot be set for type Origin", rule="self.source == 'Origin' ? !has(self.mirror) : true"
// +kubebuilder:validation:XValidation:message="spec.createControlPlaneRequest must be set for type Origin", rule="self.source == 'Origin' ? has(self.createControlPlaneRequest) && has(self.createControlPlaneRequest.name) : true"
// +apireference:kgo:include
type KonnectGatewayControlPlaneSpec struct {
	// CreateControlPlaneRequest is the request to create a Konnect Control Plane.
	//
	// +optional
	CreateControlPlaneRequest *sdkkonnectcomp.CreateControlPlaneRequest `json:"createControlPlaneRequest,omitempty"`

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
	KonnectConfiguration konnectv1alpha1.KonnectConfiguration `json:"konnect,omitempty"`
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

// KonnectGatewayControlPlaneStatus defines the observed state of KonnectGatewayControlPlane.
// +apireference:kgo:include
type KonnectGatewayControlPlaneStatus struct {
	// Conditions describe the current conditions of the KonnectGatewayControlPlane.
	//
	// Known condition types are:
	//
	// * "Programmed"
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	konnectv1alpha1.KonnectEntityStatus `json:",inline"`

	// Endpoints defines the Konnect endpoints for the control plane.
	// They are required by the DataPlane to be properly configured in
	// Konnect and connect to the control plane.
	//
	// +optional
	Endpoints *KonnectEndpoints `json:"konnectEndpoints,omitempty"`
}

// GetKonnectLabels gets the Konnect Labels from object's spec.
func (c *KonnectGatewayControlPlane) GetKonnectLabels() map[string]string {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}

	return c.Spec.CreateControlPlaneRequest.Labels
}

// SetKonnectLabels sets the Konnect Labels in object's spec.
func (c *KonnectGatewayControlPlane) SetKonnectLabels(labels map[string]string) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.Labels = labels
}

// GetKonnectName gets the Name from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectName() string {
	if c.Spec.CreateControlPlaneRequest == nil {
		return ""
	}
	return c.Spec.CreateControlPlaneRequest.Name
}

// SetKonnectName sets the Name in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectName(name string) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.Name = name
}

// GetKonnectClusterType gets the ClusterType from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectClusterType() *sdkkonnectcomp.CreateControlPlaneRequestClusterType {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}
	return c.Spec.CreateControlPlaneRequest.ClusterType
}

// SetKonnectClusterType sets the ClusterType in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectClusterType(clusterType *sdkkonnectcomp.CreateControlPlaneRequestClusterType) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.ClusterType = clusterType
}

// GetKonnectCloudGateway gets the CloudGateway from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectCloudGateway() *bool {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}
	return c.Spec.CreateControlPlaneRequest.CloudGateway
}

// SetKonnectCloudGateway sets the CloudGateway in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectCloudGateway(cloudGateway *bool) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.CloudGateway = cloudGateway
}

// GetKonnectAuthType gets the AuthType from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectAuthType() *sdkkonnectcomp.AuthType {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}
	return c.Spec.CreateControlPlaneRequest.AuthType
}

// SetKonnectAuthType sets the AuthType in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectAuthType(authType *sdkkonnectcomp.AuthType) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.AuthType = authType
}

// GetKonnectProxyURLs gets the ProxyUrls from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectProxyURLs() []sdkkonnectcomp.ProxyURL {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}
	return c.Spec.CreateControlPlaneRequest.ProxyUrls
}

// SetKonnectProxyURLs sets the ProxyUrls in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectProxyURLs(proxyURLs []sdkkonnectcomp.ProxyURL) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.ProxyUrls = proxyURLs
}

// GetKonnectDescription gets the Description from CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) GetKonnectDescription() *string {
	if c.Spec.CreateControlPlaneRequest == nil {
		return nil
	}
	return c.Spec.CreateControlPlaneRequest.Description
}

// SetKonnectDescription sets the Description in CreateControlPlaneRequest.
func (c *KonnectGatewayControlPlane) SetKonnectDescription(description *string) {
	if c.Spec.CreateControlPlaneRequest == nil {
		c.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
	}
	c.Spec.CreateControlPlaneRequest.Description = description
}

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (c *KonnectGatewayControlPlane) GetKonnectAPIAuthConfigurationRef() konnectv1alpha1.KonnectAPIAuthConfigurationRef {
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
