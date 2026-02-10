package v1alpha1

// ObjectRef is a reference to a Kubernetes object in the same namespace
type ObjectRef struct {
	// Name is the name of the referenced object
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

// NamespacedObjectRef is a reference to a Kubernetes object, optionally in another namespace
type NamespacedObjectRef struct {
	// Name is the name of the referenced object
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Namespace is the namespace of the referenced object
	// If empty, the same namespace as the referencing object is used
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
}

// SecretKeyRef is a reference to a key in a Secret
type SecretKeyRef struct {
	// Name is the name of the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Key is the key within the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key,omitempty"`

	// Namespace is the namespace of the Secret
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
}

// ConfigMapKeyRef is a reference to a key in a ConfigMap
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Key is the key within the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key,omitempty"`

	// Namespace is the namespace of the ConfigMap
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
}

// KonnectEntityStatus represents the status of a Konnect entity.
type KonnectEntityStatus struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	// If it's unset (empty string), it means the Konnect entity hasn't been created yet.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ID string `json:"id,omitempty"`

	// ServerURL is the URL of the Konnect server in which the entity exists.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=512
	ServerURL string `json:"serverURL,omitempty"`

	// OrgID is ID of Konnect Org that this entity has been created in.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	OrgID string `json:"organizationID,omitempty"`
}

// KonnectEntityRef is a reference to a Konnect entity.
type KonnectEntityRef struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ID string `json:"id,omitempty"`
}
