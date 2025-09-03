package v1alpha2

// KonnectEntityStatus represents the status of a Konnect entity.
// +apireference:kgo:include
type KonnectEntityStatus struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	// If it's unset (empty string), it means the Konnect entity hasn't been created yet.
	//
	// +optional
	ID string `json:"id,omitempty"`

	// ServerURL is the URL of the Konnect server in which the entity exists.
	//
	// +optional
	ServerURL string `json:"serverURL,omitempty"`

	// OrgID is ID of Konnect Org that this entity has been created in.
	//
	// +optional
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

// KonnectEntityStatusWithControlPlaneRef represents the status of a Konnect entity with a reference to a ControlPlane.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneRef struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this Route is associated with.
	//
	// +optional
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

// KonnectEntityStatusWithControlPlaneAndConsumerRefs represents the status of a Konnect entity with references to a ControlPlane and a Consumer.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneAndConsumerRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this Route is associated with.
	//
	// +optional
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ConsumerID is the Konnect ID of the Consumer this entity is associated with.
	//
	// +optional
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

// KonnectEntityStatusWithControlPlaneAndServiceRefs represents the status of a Konnect entity with references to a ControlPlane and a Service.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneAndServiceRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	//
	// +optional
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ServiceID is the Konnect ID of the Service this entity is associated with.
	//
	// +optional
	ServiceID string `json:"serviceID,omitempty"`
}

// SetControlPlaneID sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndServiceRefs) SetControlPlaneID(id string) {
	in.ControlPlaneID = id
}

// GetControlPlaneID sets the server URL of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndServiceRefs) GetControlPlaneID() string {
	return in.ControlPlaneID
}

// KonnectEntityStatusWithControlPlaneAndUpstreamRefs represents the status of a Konnect entity with references to a ControlPlane and an Upstream.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneAndUpstreamRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	//
	// +optional
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// UpstreamID is the Konnect ID of the Upstream this entity is associated with.
	//
	// +optional
	UpstreamID string `json:"upstreamID,omitempty"`
}

// KonnectEntityStatusWithControlPlaneAndKeySetRef represents the status of a Konnect entity with references to a ControlPlane and a KeySet.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneAndKeySetRef struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	//
	// +optional
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// KeySetID is the Konnect ID of the KeySet this entity is associated with.
	//
	// +optional
	KeySetID string `json:"keySetID,omitempty"`
}

// SetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndKeySetRef) SetControlPlaneID(id string) {
	in.ControlPlaneID = id
}

// GetControlPlaneID sets the ControlPlane ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndKeySetRef) GetControlPlaneID() string {
	return in.ControlPlaneID
}

// SetKeySetID sets the KeySet ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndKeySetRef) SetKeySetID(id string) {
	in.KeySetID = id
}

// GetKeySetID sets the KeySet ID of the KonnectEntityStatus struct.
func (in *KonnectEntityStatusWithControlPlaneAndKeySetRef) GetKeySetID() string {
	return in.KeySetID
}

// KonnectEntityStatusWithControlPlaneAndCertificateRefs represents the status of a Konnect entity with references to a ControlPlane and a Certificate.
// +apireference:kgo:include
type KonnectEntityStatusWithControlPlaneAndCertificateRefs struct {
	KonnectEntityStatus `json:",inline"`

	// ControlPlaneID is the Konnect ID of the ControlPlane this entity is associated with.
	//
	// +optional
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// CertificateID is the Konnect ID of the Certificate this entity is associated with.
	//
	// +optional
	CertificateID string `json:"certificateID,omitempty"`
}

// KonnectEntityStatusWithNetworkRef represents the status of a Konnect entity with reference to a Konnect cloud gateway network.
type KonnectEntityStatusWithNetworkRef struct {
	KonnectEntityStatus `json:",inline"`
	// NetworkID is the Konnect ID of the Konnect cloud gateway network this entity is associated with.
	//
	// +optional
	NetworkID string `json:"networkID,omitempty"`
}
