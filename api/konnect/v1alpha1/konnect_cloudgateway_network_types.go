package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

func init() {
	SchemeBuilder.Register(&KonnectCloudGatewayNetwork{}, &KonnectCloudGatewayNetworkList{})
}

// KonnectCloudGatewayNetwork is the Schema for the Konnect Network API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="State",description="The state the network is in",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.name == self.spec.name",message="spec.name is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.cloud_gateway_provider_account_id == self.spec.cloud_gateway_provider_account_id",message="spec.cloud_gateway_provider_account_id is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.region == self.spec.region",message="spec.region is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.availability_zones == self.spec.availability_zones",message="spec.availability_zones is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.cidr_block == self.spec.cidr_block",message="spec.cidr_block is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : (!has(self.spec.state) && !has(oldSelf.spec.state)) || self.spec.state == oldSelf.spec.state",message="spec.state is immutable when an entity is already Programmed"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectCloudGatewayNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KonnectCloudGatewayNetwork.
	Spec KonnectCloudGatewayNetworkSpec `json:"spec"`

	// Status defines the observed state of KonnectCloudGatewayNetwork.
	//
	// +optional
	Status KonnectCloudGatewayNetworkStatus `json:"status,omitempty"`
}

// KonnectCloudGatewayNetworkSpec defines the desired state of KonnectCloudGatewayNetwork.
//
// +apireference:kgo:include
type KonnectCloudGatewayNetworkSpec struct {
	// NOTE: These fields are extracted from sdkkonnectcomp.CreateNetworkRequest
	// because for some reason when embedding the struct, the fields deserialization
	// doesn't work (the embedded field is always empty).

	// Specifies the name of the network on Konnect.
	//
	// +required
	Name string `json:"name"`

	// Specifies the provider Account ID.
	//
	// +required
	CloudGatewayProviderAccountID string `json:"cloud_gateway_provider_account_id"`

	// Region ID for cloud provider region.
	//
	// +required
	Region string `json:"region"`

	// List of availability zones that the network is attached to.
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=5
	AvailabilityZones []string `json:"availability_zones"`

	// CIDR block configuration for the network.
	//
	// +required
	CidrBlock string `json:"cidr_block"`

	// Initial state for creating a network.
	//
	// +optional
	State *sdkkonnectcomp.NetworkCreateState `json:"state"`

	// +required
	KonnectConfiguration konnectv1alpha2.KonnectConfiguration `json:"konnect"`
}

// KonnectCloudGatewayNetworkStatus defines the observed state of KonnectCloudGatewayNetwork.
// +apireference:kgo:include
type KonnectCloudGatewayNetworkStatus struct {
	konnectv1alpha2.KonnectEntityStatus `json:",inline"`

	// State is the current state of the network. Can be e.g. initializing, ready, terminating.
	//
	// +optional
	State string `json:"state,omitempty"`

	// Conditions describe the current conditions of the KonnectCloudGatewayNetwork.
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

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (c *KonnectCloudGatewayNetwork) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.KonnectAPIAuthConfigurationRef {
	return c.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// KonnectCloudGatewayNetworkList contains a list of KonnectCloudGatewayNetwork.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KonnectCloudGatewayNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectCloudGatewayNetwork `json:"items"`
}
