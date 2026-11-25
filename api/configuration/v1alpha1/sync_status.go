package v1alpha1

// SyncStatus contains the internal synchronization state of a Kong Gateway
// configuration entity managed in Konnect.
//
// It lets the operator skip periodic PUT requests for entities that did not
// change, also across operator restarts and leader changes. The values are
// internal implementation details: do not rely on them, and do not edit them.
//
// If the CRD schema installed in the cluster predates this field, Kubernetes
// prunes it from the status. The operator then falls back to the in-memory
// tracker: it sends one PUT per entity after a restart instead of one PUT per
// sync period, and it does not retry the pruned status write on every reconcile.
type SyncStatus struct {
	// PayloadHash is the canonical hash of the payload the operator last sent to
	// Konnect for this entity.
	//
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	PayloadHash string `json:"payloadHash,omitempty"`

	// RepresentationHash is the canonical hash of the entity representation the
	// operator last received from Konnect.
	//
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	RepresentationHash string `json:"representationHash,omitempty"`

	// HashVersion identifies the hashing scheme used to compute PayloadHash and
	// RepresentationHash. A change of hash version makes the stored hashes
	// unverifiable, so the entity is sent to Konnect once more.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=16
	HashVersion string `json:"hashVersion,omitempty"`
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongService) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongService) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongRoute) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongRoute) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongPluginBinding) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongPluginBinding) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongUpstream) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongUpstream) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongTarget) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongTarget) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCACertificate) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCACertificate) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCertificate) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCertificate) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongSNI) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongSNI) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongKey) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongKey) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongKeySet) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongKeySet) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCredentialBasicAuth) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCredentialBasicAuth) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCredentialAPIKey) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCredentialAPIKey) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCredentialACL) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCredentialACL) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCredentialJWT) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCredentialJWT) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongCredentialHMAC) GetSyncStatus() *SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongCredentialHMAC) SetSyncStatus(s *SyncStatus) {
	obj.Status.Sync = s
}

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (v *KongVault) GetSyncStatus() *SyncStatus {
	return v.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (v *KongVault) SetSyncStatus(s *SyncStatus) {
	v.Status.Sync = s
}
