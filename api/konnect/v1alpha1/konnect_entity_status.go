package v1alpha1

type KonnectEntityStatus struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	// If it's unset (empty string), it means the Konnect entity hasn't been created yet.
	ID string `json:"id,omitempty"`

	// ServerURL is the URL of the Konnect server in which the entity exists.
	ServerURL string `json:"serverURL,omitempty"`

	// OrgID is ID of Konnect Org that this entity has been created in.
	OrgID string `json:"organizationID,omitempty"`
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
	return in.ID
}

// SetKonnectID sets the ID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetKonnectID(id string) {
	in.ID = id
}

// GetServerURL returns the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetServerURL() string {
	return in.ServerURL
}

// SetServerURL sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetServerURL(s string) {
	in.ServerURL = s
}

type KonnectEntityStatusWithControlPlaneRef struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this Route is associated with.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`
}

type KonnectEntityStatusWithControlPlaneAndServiceRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ServiceID is the Konnect ID of the Service this entity is associated with.
	ServiceID string `json:"serviceID,omitempty"`
}
