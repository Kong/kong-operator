package v1alpha1

// KeySetRefType is the enum type for the KeySetRef.
// +kubebuilder:validation:Enum=konnectID;namespacedRef
// +apireference:kgo:include
type KeySetRefType string

const (
	// KeySetRefKonnectID is the type for the KonnectID KeySetRef.
	// It is used to reference a KeySet entity by its ID on the Konnect platform.
	KeySetRefKonnectID KeySetRefType = "konnectID"

	// KeySetRefNamespacedRef is the type for the KeySetRef.
	// It is used to reference a KeySet entity inside the cluster
	// using a namespaced reference.
	KeySetRefNamespacedRef KeySetRefType = "namespacedRef"
)

// KeySetRef is the schema for the KeySetRef type.
// It is used to reference a KeySet entity.
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true", message="when type is namespacedRef, namespacedRef must be set"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KeySetRef struct {
	// Type defines type of the KeySet object reference. It can be one of:
	// - namespacedRef
	//
	// +kubebuilder:validation:Enum=namespacedRef
	Type KeySetRefType `json:"type"`

	// KonnectID is the schema for the KonnectID type.
	// This field is required when the Type is konnectID.
	// +optional
	KonnectID *string `json:"konnectID,omitempty"`

	// NamespacedRef is a reference to a KeySet entity inside the cluster.
	// This field is required when the Type is namespacedRef.
	// +optional
	NamespacedRef *KeySetNamespacedRef `json:"namespacedRef,omitempty"`
}

// KeySetNamespacedRef is the schema for the KeySetNamespacedRef type.
// +apireference:kgo:include
type KeySetNamespacedRef struct {
	// Name is the name of the KeySet object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// TODO: Implement cross namespace references:
	// https://github.com/Kong/kubernetes-configuration/issues/36
}
