package v1alpha1

const (
	ControlPlaneRefKonnectID            = "konnectID"
	ControlPlaneRefKonnectNamespacedRef = "konnectNamespacedRef"
	ControlPlaneRefKIC                  = "kic"
)

type ControlPlaneRef struct {
	// Type can be one of:
	// - konnectID
	// - konnectNamespacedRef
	// - kic
	Type string `json:"type,omitempty"`

	// TODO(pmalek)
	KonnectID *string `json:"konnectID,omitempty"`

	// KonnectNamespacedRef is a reference to a Konnect Control Plane entity inside the cluster.
	// It contains the name of the Konnect Control Plane and the namespace in which it exists.
	// If the namespace is not provided, it is assumed that the Konnect Control Plane
	// is in the same namespace as the resource that references it.
	KonnectNamespacedRef *KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`

	// TODO(pmalek)
	KIC *KIC `json:"kic,omitempty"`
}

// KonnectNamespacedRef is the schema for the KonnectNamespacedRef type.
type KonnectNamespacedRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

// TODO(pmalek)
type KIC struct{}
