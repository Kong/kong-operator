package v1alpha1

// ObjectRefType is the enum type for the ObjectRef.
//
// +kubebuilder:validation:Enum=konnectID;namespacedRef
// +apireference:kgo:include
type ObjectRefType string

const (
	// ObjectRefTypeKonnectID is the type for the KonnectID KonnectRef.
	// It is used to reference an entity by its ID on the Konnect platform.
	ObjectRefTypeKonnectID ObjectRefType = "konnectID"

	// ObjectRefTypeNamespacedRef is the type for the KonnectRef.
	// It is used to reference an entity inside the cluster
	// using a namespaced reference.
	ObjectRefTypeNamespacedRef ObjectRefType = "namespacedRef"
)

// ObjectRef is the schema for the ObjectRef type.
// It is used to reference an entity. Currently it is possible to reference
// a remote Konnect entity by its ID or a local in cluster entity by its namespaced name.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true", message="when type is namespacedRef, namespacedRef must be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? !has(self.konnectID) : true", message="when type is namespacedRef, konnectID must not be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectID' ? has(self.konnectID) : true", message="when type is konnectID, konnectID must be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectID' ? !has(self.namespacedRef) : true", message="when type is konnectID, namespacedRef must not be set"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type ObjectRef struct {
	// Type defines type of the object which is referenced. It can be one of:
	//
	// - konnectID
	// - namespacedRef
	//
	// +required
	Type ObjectRefType `json:"type"`

	// KonnectID is the schema for the KonnectID type.
	// This field is required when the Type is konnectID.
	//
	// +optional
	KonnectID *string `json:"konnectID,omitempty"`

	// NamespacedRef is a reference to a KeySet entity inside the cluster.
	// This field is required when the Type is namespacedRef.
	//
	// +optional
	NamespacedRef *NamespacedRef `json:"namespacedRef,omitempty"`
}

// NamespacedRef is a reference to a namespaced resource.
//
// +apireference:kgo:include
type NamespacedRef struct {
	// Name is the name of the referred resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// TODO: Implement cross namespace references:
	// https://github.com/Kong/kubernetes-configuration/issues/36

	// Namespace is the namespace of the referred resource.
	//
	// For namespace-scoped resources if no Namespace is provided then the
	// namespace of the parent object MUST be used.
	//
	// This field MUST not be set when referring to cluster-scoped resources.
	//
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}
