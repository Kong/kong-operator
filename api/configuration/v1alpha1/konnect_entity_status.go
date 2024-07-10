package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type KonnectEntityStatus struct {
	// KonnectID is the unique identifier of the Konnect entity as assigned by Konnect API.
	// If it's unset (empty string), it means the Konnect entity hasn't been created yet.
	KonnectID string `json:"id,omitempty"`

	// ServerURL is the URL of the Konnect server in which the entity exists.
	ServerURL string `json:"serverURL,omitempty"`

	// OrgID is ID of Konnect Org that this entity has been created in.
	OrgID string `json:"organizationID,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetOrgID returns the OrgID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetOrgID() string {
	return in.OrgID
}

// SetOrgID sets the OrgID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetOrgID(id string) {
	in.OrgID = id
}

// GetKonnectID returns the ID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetKonnectID() string {
	return in.KonnectID
}

// SetKonnectID sets the ID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetKonnectID(id string) {
	in.KonnectID = id
}

// GetServerURL returns the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetServerURL() string {
	return in.ServerURL
}

// SetServerURL sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetServerURL(s string) {
	in.ServerURL = s
}

// GetConditions returns the Status Conditions
func (in *KonnectEntityStatus) GetConditions() []metav1.Condition {
	return in.Conditions
}

// SetConditions sets the Status Conditions
func (in *KonnectEntityStatus) SetConditions(conditions []metav1.Condition) {
	in.Conditions = conditions
}
