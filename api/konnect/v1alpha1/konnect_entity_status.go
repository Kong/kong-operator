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
	if in == nil {
		return ""
	}
	return in.OrgID
}

// SetOrgID sets the OrgID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetOrgID(id string) {
	in.OrgID = id
}

// GetKonnectID returns the ID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetKonnectID() string {
	if in == nil {
		return ""
	}
	return in.ID
}

// SetKonnectID sets the ID field of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) SetKonnectID(id string) {
	in.ID = id
}

// GetServerURL returns the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatus) GetServerURL() string {
	if in == nil {
		return ""
	}
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

// SetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneRef) SetControlPlaneID(id string) {
	in.ControlPlaneID = id
}

// GetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneRef) GetControlPlaneID() string {
	return in.ControlPlaneID
}

type KonnectEntityStatusWithControlPlaneAndConsumerRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this Route is associated with.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ConsumerID is the Konnect ID of the Consumer this entity is associated with.
	ConsumerID string `json:"consumerID,omitempty"`
}

// SetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndConsumerRefs) SetControlPlaneID(id string) {
	in.ControlPlaneID = id
}

// GetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndConsumerRefs) GetControlPlaneID() string {
	return in.ControlPlaneID
}

// SetConsumerID sets the Consumer ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndConsumerRefs) SetConsumerID(id string) {
	in.ConsumerID = id
}

// GetConsumerID sets the Consumer ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndConsumerRefs) GetConsumerID() string {
	return in.ConsumerID
}

type KonnectEntityStatusWithControlPlaneAndServiceRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ServiceID is the Konnect ID of the Service this entity is associated with.
	ServiceID string `json:"serviceID,omitempty"`
}

// SetServerURL sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndServiceRefs) SetControlPlaneID(id string) {
	in.ControlPlaneID = id
}

// GetServerURL sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndServiceRefs) GetControlPlaneID() string {
	return in.ControlPlaneID
}

type KonnectEntityStatusWithControlPlaneAndUpstreamRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// UpstreamID is the Konnect ID of the Upstream this entity is associated with.
	UpstreamID string `json:"upstreamID,omitempty"`
}
