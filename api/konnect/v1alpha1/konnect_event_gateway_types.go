package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func init() {
	SchemeBuilder.Register(&KonnectEventGateway{}, &KonnectEventGatewayList{})
}

// KonnectEventGateway is the Schema for the Konnect Event Gateways API.
// It represents an Event Gateway in Konnect, backed by the /v1/event-gateways API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced,shortName=keg,categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:message="spec.konnect.authRef is immutable when an entity is already Programmed",rule="(!has(self.status) || !has(self.status.conditions) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef"
// +kubebuilder:validation:XValidation:message="spec.konnect.authRef is immutable when an entity refers to a Valid API Auth Configuration",rule="(!has(self.status) || !has(self.status.conditions) || !self.status.conditions.exists(c, c.type == 'APIAuthValid' && c.status == 'True')) ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef"
// +kong:channels=kong-operator
type KonnectEventGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KonnectEventGateway.
	//
	// +required
	Spec KonnectEventGatewaySpec `json:"spec"`

	// Status defines the observed state of KonnectEventGateway.
	//
	// +optional
	Status KonnectEventGatewayStatus `json:"status,omitempty"`
}

// KonnectEventGatewaySpec defines the desired state of KonnectEventGateway.
//
// +kubebuilder:validation:XValidation:message="spec.source is immutable",rule="self.source == oldSelf.source"
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest cannot be set when source is Mirror",rule="self.source == 'Mirror' ? !has(self.createGatewayRequest) : true"
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest with name must be set when source is Origin",rule="self.source == 'Origin' ? has(self.createGatewayRequest) : true"
// +kubebuilder:validation:XValidation:message="spec.mirror must be set when source is Mirror",rule="self.source == 'Mirror' ? has(self.mirror) : true"
// +kubebuilder:validation:XValidation:message="spec.mirror cannot be set when source is Origin",rule="self.source == 'Origin' ? !has(self.mirror) : true"
type KonnectEventGatewaySpec struct {
	// Source represents the source type of the Konnect entity.
	// Origin means the operator owns the lifecycle — it creates, updates, and deletes the Event
	// Gateway in Konnect. Mirror means the Event Gateway already exists in Konnect and the
	// operator only reads its state and populates the status.
	//
	// +kubebuilder:validation:Enum=Origin;Mirror
	// +kubebuilder:default=Origin
	// +optional
	Source *commonv1alpha1.EntitySource `json:"source,omitempty"`

	// Mirror holds the configuration for a mirrored Event Gateway.
	// Only applicable when source is Mirror.
	//
	// +optional
	Mirror *EventGatewayMirrorSpec `json:"mirror,omitempty"`

	// CreateGatewayRequest groups all fields sent to POST /v1/event-gateways.
	// Only applicable when source is Origin.
	//
	// +optional
	CreateGatewayRequest *CreateEventGatewayRequest `json:"createGatewayRequest,omitempty"`

	// KonnectConfiguration contains the Konnect API authentication configuration.
	//
	// +optional
	// TODO: Decide if we want the crossnamespace reference for APIAuthConfigurationRef here, 
	// or if we want to enforce that the referenced APIAuthConfiguration must be in the same namespace as the KonnectEventGateway. 
	// If we allow cross-namespace references, we need to change this type to v1alpha2.ControlPlaneKonnectConfiguration to reuse the 
	// logic we already have for cross-namespace references in control planes.
	KonnectConfiguration konnectv1alpha2.KonnectConfiguration `json:"konnect,omitempty"`
}

// CreateEventGatewayRequest maps to the Konnect CreateGatewayRequest / UpdateGatewayRequest schema.
//
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest.labels must not have more than 50 entries",rule="!has(self.labels) || size(self.labels) <= 50"
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest.labels keys must be of length 1-63 characters",rule="!has(self.labels) || self.labels.all(key, size(key) >= 1 && size(key) <= 63)"
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest.labels values must be of length 1-63 characters",rule="!has(self.labels) || self.labels.all(key, size(self.labels[key]) >= 1 && size(self.labels[key]) <= 63)"
// +kubebuilder:validation:XValidation:message="spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'",rule="!has(self.labels) || self.labels.all(key, !key.startsWith('kong') && !key.startsWith('konnect') && !key.startsWith('mesh') && !key.startsWith('kic') && !key.startsWith('_'))"
type CreateEventGatewayRequest struct {
	// Name is the human-readable name of the Event Gateway.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Description is a human-readable description of the Event Gateway.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=512
	Description *string `json:"description,omitempty"`

	// MinRuntimeVersion is the minimum keg version that can connect to this gateway.
	// Must match the pattern X.Y (e.g. "1.1").
	//
	// +optional
	// +kubebuilder:validation:Pattern=`^\d+\.\d+$`
	MinRuntimeVersion *string `json:"minRuntimeVersion,omitempty"`

	// Labels are metadata key-value pairs for filtering and searching.
	//
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// EventGatewayMirrorSpec holds the configuration for a mirrored Event Gateway.
type EventGatewayMirrorSpec struct {
	// Konnect contains the ID of the existing Event Gateway in Konnect.
	//
	// +required
	Konnect EventGatewayMirrorKonnect `json:"konnect"`
}

// EventGatewayMirrorKonnect contains the Konnect ID of an existing Event Gateway.
type EventGatewayMirrorKonnect struct {
	// ID is the UUID of the existing Event Gateway in Konnect.
	//
	// +required
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ID commonv1alpha1.KonnectIDType `json:"id"`
}

// KonnectEventGatewayStatus defines the observed state of KonnectEventGateway.
type KonnectEventGatewayStatus struct {
	// Conditions describe the current conditions of the KonnectEventGateway.
	//
	// Known condition types are:
	//
	// * "Programmed"
	// * "APIAuthValid"
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type:"Programmed",status:"Unknown",reason:"Pending",message:"Waiting for controller",lastTransitionTime:"1970-01-01T00:00:00Z"}}
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// KonnectEntityStatus holds the Konnect ID and organization ID.
	konnectv1alpha2.KonnectEntityStatus `json:",inline"` //nolint:embeddedstructfieldcheck
}

// GetKonnectLabels returns the labels from the CreateGatewayRequest.
func (eg *KonnectEventGateway) GetKonnectLabels() map[string]string {
	if eg.Spec.CreateGatewayRequest == nil {
		return nil
	}
	return eg.Spec.CreateGatewayRequest.Labels
}

// SetKonnectLabels sets the labels in the CreateGatewayRequest.
func (eg *KonnectEventGateway) SetKonnectLabels(labels map[string]string) {
	if eg.Spec.CreateGatewayRequest == nil {
		eg.Spec.CreateGatewayRequest = &CreateEventGatewayRequest{}
	}
	eg.Spec.CreateGatewayRequest.Labels = labels
}

// GetKonnectName returns the name from the CreateGatewayRequest.
func (eg *KonnectEventGateway) GetKonnectName() string {
	if eg.Spec.CreateGatewayRequest == nil {
		return ""
	}
	return eg.Spec.CreateGatewayRequest.Name
}

// SetKonnectName sets the name in the CreateGatewayRequest.
func (eg *KonnectEventGateway) SetKonnectName(name string) {
	if eg.Spec.CreateGatewayRequest == nil {
		eg.Spec.CreateGatewayRequest = &CreateEventGatewayRequest{}
	}
	eg.Spec.CreateGatewayRequest.Name = name
}

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (eg *KonnectEventGateway) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef {
	return konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name: eg.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
	}
}

// KonnectEventGatewayList contains a list of KonnectEventGateway.
//
// +kubebuilder:object:root=true
type KonnectEventGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectEventGateway `json:"items"`
}
