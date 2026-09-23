package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// KonnectConfigStoreSync is the Schema for the KonnectConfigStoreSync API.
//
// A KonnectConfigStoreSync declares a one-way mapping from one Kubernetes
// Secret to one Konnect Config Store: the operator keeps the store entry (or
// entries) in step with the Secret so that certificate rotation is a pure
// data event. Private key material is written only to the Config Store and
// never lands in Konnect configuration entities.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories=kong;konnect
// +kubebuilder:printcolumn:name="Store",description="Name of the referenced KonnectConfigStore",type=string,JSONPath=`.spec.configStoreRef.name`
// +kubebuilder:printcolumn:name="Secret",description="Name of the referenced Secret",type=string,JSONPath=`.spec.secretRef.name`
// +kubebuilder:printcolumn:name="Mode",description="Sync mode (Combined or Split)",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Entries",description="Number of entries currently synced to the Config Store",type=integer,JSONPath=`.status.entriesSynced`
// +kubebuilder:printcolumn:name="Synced",description="The entries are synced to the Config Store",type=string,JSONPath=`.status.conditions[?(@.type=='Synced')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +kubebuilder:validation:XValidation:rule="self.spec.mode == oldSelf.spec.mode",message="spec.mode is immutable"
// +kubebuilder:validation:XValidation:rule="self.spec.configStoreRef == oldSelf.spec.configStoreRef",message="spec.configStoreRef is immutable"
// +kong:channels=kong-operator
type KonnectConfigStoreSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the KonnectConfigStoreSync resource.
	//
	// +required
	Spec KonnectConfigStoreSyncSpec `json:"spec,omitzero"`

	// Status is the status of the KonnectConfigStoreSync resource.
	//
	// +optional
	Status KonnectConfigStoreSyncStatus `json:"status,omitempty"`
}

// KonnectConfigStoreSyncMode is the mode in which Secret data is mapped to
// Config Store entries.
//
// +kubebuilder:validation:Enum=Combined;Split
type KonnectConfigStoreSyncMode string

const (
	// KonnectConfigStoreSyncModeCombined writes the certificate/key pair as a
	// single Config Store entry whose value is a JSON object with the frozen
	// subfields "certificate" and "key". This is the default and the only mode
	// in which a mismatched cert/key pair is unrepresentable.
	KonnectConfigStoreSyncModeCombined KonnectConfigStoreSyncMode = "Combined"

	// KonnectConfigStoreSyncModeSplit writes each listed Secret data field to
	// its own Config Store entry. Split is intended for single-field and
	// non-TLS material; it is not a fallback for oversize cert/key pairs
	// because the intermediate state of a two-entry rotation is exactly the
	// mismatched-pair condition Combined mode exists to prevent.
	KonnectConfigStoreSyncModeSplit KonnectConfigStoreSyncMode = "Split"
)

// KonnectConfigStoreSyncDeletionPolicy determines what happens to the Config
// Store entries owned by a sync when the sync is deleted.
//
// +kubebuilder:validation:Enum=Orphan;Delete
type KonnectConfigStoreSyncDeletionPolicy string

const (
	// KonnectConfigStoreSyncDeletionPolicyOrphan keeps the Config Store
	// entries when the sync is deleted. This is the default: a stale entry is
	// recoverable, a deleted entry breaks live TLS with no fallback.
	KonnectConfigStoreSyncDeletionPolicyOrphan KonnectConfigStoreSyncDeletionPolicy = "Orphan"

	// KonnectConfigStoreSyncDeletionPolicyDelete deletes the Config Store
	// entries owned by the sync when the sync is deleted, after a best-effort
	// in-use check.
	KonnectConfigStoreSyncDeletionPolicyDelete KonnectConfigStoreSyncDeletionPolicy = "Delete"
)

// KonnectConfigStoreSyncSpec defines the desired state of KonnectConfigStoreSync.
//
// +kubebuilder:validation:XValidation:rule="has(self.combined) == (self.mode == 'Combined') && has(self.split) == (self.mode == 'Split')",message="exactly one of spec.combined or spec.split must be set, matching spec.mode"
type KonnectConfigStoreSyncSpec struct {
	// ConfigStoreRef is a reference to the KonnectConfigStore this sync writes
	// to. The sync reads the store's Konnect ID and Control Plane ID from the
	// referenced store's status and never resolves a Control Plane itself.
	//
	// The reference is immutable. Moving synchronization to another store
	// requires creating a new KonnectConfigStoreSync and explicitly migrating
	// consumers before removing this one. Referencing a store in another
	// namespace requires a KongReferenceGrant in the referenced namespace
	// allowing it; that is enforced by the controller, not by CRD validation.
	//
	// +required
	ConfigStoreRef commonv1alpha1.NamespacedRef `json:"configStoreRef"`

	// SecretRef is a reference to the Secret whose data is synced to the
	// Config Store. It is mutable: repointing at another Secret changes only
	// the synced value, not the entry key.
	//
	// Referencing a Secret in another namespace requires a KongReferenceGrant
	// in the referenced namespace allowing it; that is enforced by the
	// controller, not by CRD validation.
	//
	// +required
	SecretRef commonv1alpha1.NamespacedRef `json:"secretRef"`

	// Mode selects how Secret data is mapped to Config Store entries.
	// It is immutable: re-keying is an explicit create-new-sync-and-migrate
	// procedure because the reference string itself changes with the key.
	//
	// +optional
	// +kubebuilder:default=Combined
	Mode KonnectConfigStoreSyncMode `json:"mode,omitempty"`

	// Combined configures the single-entry mapping. It must be set if and only
	// if mode is Combined.
	//
	// +optional
	Combined *KonnectConfigStoreSyncCombined `json:"combined,omitempty"`

	// Split configures the per-field mapping. It must be set if and only if
	// mode is Split.
	//
	// +optional
	Split *KonnectConfigStoreSyncSplit `json:"split,omitempty"`

	// DeletionPolicy determines what happens to the Config Store entries owned
	// by this sync when the sync is deleted. It is mutable, including during
	// deletion - that is the intended escape hatch.
	//
	// +optional
	// +kubebuilder:default=Orphan
	DeletionPolicy KonnectConfigStoreSyncDeletionPolicy `json:"deletionPolicy,omitempty"`
}

// KonnectConfigStoreSyncCombined configures Combined mode: one Config Store
// entry holds the certificate/key pair as a JSON object with the frozen
// subfields "certificate" and "key". Those subfield names are part of the API
// contract - they appear verbatim in every vault reference string - and will
// never be renamed.
//
// +kubebuilder:validation:XValidation:rule="self.certificateField != self.keyField",message="spec.combined.certificateField and spec.combined.keyField must be different"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.storeKey) || (has(self.storeKey) && self.storeKey == oldSelf.storeKey)",message="spec.combined.storeKey is immutable once set"
type KonnectConfigStoreSyncCombined struct {
	// StoreKey is the key of the Config Store entry holding the pair. When
	// unset, the controller derives it from the sync's identity using the
	// length-prefixed format k8s-<len(namespace)>-<namespace>-<len(name)>-<name>.
	// Set it explicitly only for topologies that require identical reference
	// strings across Control Planes (e.g. multi-CP fan-out).
	//
	// It is immutable once set.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+$`
	StoreKey *string `json:"storeKey,omitempty"`

	// CertificateField is the key of the Secret data entry holding the
	// certificate PEM (chain included, if any). It maps to the JSON subfield
	// "certificate" in the stored value.
	//
	// +optional
	// +kubebuilder:default="tls.crt"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	CertificateField string `json:"certificateField,omitempty"`

	// KeyField is the key of the Secret data entry holding the private key
	// PEM. It maps to the JSON subfield "key" in the stored value.
	//
	// +optional
	// +kubebuilder:default="tls.key"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	KeyField string `json:"keyField,omitempty"`
}

// KonnectConfigStoreSyncSplit configures Split mode: each listed Secret data
// field is written to its own Config Store entry.
//
// +kubebuilder:validation:XValidation:rule="self.entries.all(e, self.entries.filter(o, has(o.storeKey) && has(e.storeKey) && o.storeKey == e.storeKey).size() <= 1)",message="spec.split.entries storeKeys must be unique"
// +kubebuilder:validation:XValidation:rule="self.entries.all(n, !oldSelf.entries.exists(o, o.field == n.field && has(o.storeKey)) || (has(n.storeKey) && oldSelf.entries.exists(o, o.field == n.field && has(o.storeKey) && o.storeKey == n.storeKey)))",message="spec.split.entries storeKey is immutable for entries that keep their field"
type KonnectConfigStoreSyncSplit struct {
	// Entries lists the Secret data fields to sync. Fields may be added or
	// removed freely; the storeKey of an existing entry (identified by its
	// field) is immutable.
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=field
	Entries []KonnectConfigStoreSyncSplitEntry `json:"entries"`
}

// KonnectConfigStoreSyncSplitEntry maps one Secret data field to one Config
// Store entry.
type KonnectConfigStoreSyncSplitEntry struct {
	// Field is the key of the Secret data entry to sync. Any Secret data key
	// may be mapped; it is validated as a Secret key name, not whitelisted.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	Field string `json:"field"`

	// StoreKey is the key of the Config Store entry holding this field's
	// value. When unset, the controller derives it as <derived sync key>-<field>.
	//
	// It is immutable once set (per entry, identified by its field).
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._-]+$`
	StoreKey *string `json:"storeKey,omitempty"`
}

// KonnectConfigStoreSyncStatus defines the observed state of KonnectConfigStoreSync.
//
// The status never contains Secret plaintext: values are represented only by
// their hashes and sizes.
type KonnectConfigStoreSyncStatus struct {
	// Conditions describe the status of the KonnectConfigStoreSync.
	// All four condition types (ConfigStoreRefValid, SecretRefValid, PairValid,
	// Synced) are always present so that kubectl wait never hangs.
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=4
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "ConfigStoreRefValid", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"},{type: "SecretRefValid", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"},{type: "PairValid", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"},{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// StoreID is the Konnect ID of the referenced Config Store, observed from
	// the store's status.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	StoreID string `json:"storeID,omitempty"`

	// ControlPlaneID is the Konnect ID of the Control Plane the referenced
	// Config Store belongs to, observed from the store's status. A sync never
	// resolves a Control Plane itself.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ControlPlaneID string `json:"controlPlaneID,omitempty"`

	// ObservedSecretResourceVersion is the resourceVersion of the Secret at
	// the last successful sync. It is an observation aid only and is never an
	// input to a push decision.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=64
	ObservedSecretResourceVersion string `json:"observedSecretResourceVersion,omitempty"`

	// Entries reports durable state for desired Config Store entries this sync
	// has written, including entries currently lost in per-key conflict
	// election, plus entries retained while cleanup is blocked. The limit
	// accommodates one full desired set and one full set awaiting cleanup.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=128
	// +listType=map
	// +listMapKey=storeKey
	Entries []KonnectConfigStoreSyncEntryStatus `json:"entries,omitempty"`

	// References publishes the reference suffixes consumers need to build
	// vault reference strings. A full reference is
	// {vault://<KongVault prefix>/<suffix>}.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=suffix
	References []KonnectConfigStoreSyncReference `json:"references,omitempty"`

	// EntriesSynced is the number of desired entries this sync currently wins
	// and has synced to the Config Store.
	//
	// +optional
	EntriesSynced int32 `json:"entriesSynced,omitempty"`

	// EntriesTotal is the total number of entries this sync manages.
	//
	// +optional
	EntriesTotal int32 `json:"entriesTotal,omitempty"`
}

// KonnectConfigStoreSyncEntryStatus reports durable state for one Config Store
// entry written by the sync.
type KonnectConfigStoreSyncEntryStatus struct {
	// StoreKey is the resolved key of the Config Store entry.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	StoreKey string `json:"storeKey"`

	// SourceFields are the Secret data keys whose values produced this entry.
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	SourceFields []string `json:"sourceFields"`

	// Hash is the SHA-256 hash ("sha256:<hex>") of the value written to the
	// store. The value itself is never exposed: status, logs and events must
	// never contain Secret plaintext.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	Hash string `json:"hash,omitempty"`

	// ValueBytes is the size in bytes of the decoded value last written,
	// measured against the 5120-byte entry value cap.
	//
	// +optional
	ValueBytes int64 `json:"valueBytes,omitempty"`

	// KeyBytes is the size in bytes of the resolved store key, measured
	// against the 512-byte key cap.
	//
	// +optional
	KeyBytes int64 `json:"keyBytes,omitempty"`

	// NotAfter is the expiry time of the certificate last written. It is only
	// set when the entry holds a certificate.
	//
	// +optional
	NotAfter *metav1.Time `json:"notAfter,omitempty"`

	// LastPushTime is the time the entry was last pushed to the Config Store.
	//
	// +optional
	LastPushTime *metav1.Time `json:"lastPushTime,omitempty"`

	// ObservedUpdatedAt is the store-side updated_at timestamp of the entry,
	// observed from the Config Store API. It is a drift signal: a value
	// advancing without a corresponding push means the entry was modified
	// outside this sync.
	//
	// +optional
	ObservedUpdatedAt *metav1.Time `json:"observedUpdatedAt,omitempty"`
}

// KonnectConfigStoreSyncReference publishes the reference suffix for one
// synced entry (or one JSON subfield of it), so consumers can assemble a
// vault reference string as {vault://<KongVault prefix>/<suffix>} without
// hand-assembling the store key and subfield fragments.
type KonnectConfigStoreSyncReference struct {
	// SubField is the JSON subfield of the Config Store entry value. It is
	// set for Combined mode entries ("certificate" or "key") and omitted for
	// Split mode entries, whose raw values are referenced by store key alone.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	SubField string `json:"subfield,omitempty"`

	// Suffix is the suffix of the vault reference: "<storeKey>/<subfield>"
	// for Combined mode entries (e.g. "mytls/certificate"), or "<storeKey>"
	// for Split mode entries.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=577
	Suffix string `json:"suffix"`
}

// KonnectConfigStoreSyncList contains a list of KonnectConfigStoreSync resources.
//
// +kubebuilder:object:root=true
type KonnectConfigStoreSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectConfigStoreSync `json:"items"`
}
