package v1alpha1

const (
	// ControlPlaneRefKonnectNamespacedRef is the type for the KonnectNamespacedRef ControlPlaneRef.
	// It is used to reference a Konnect Control Plane entity inside the cluster
	// using a namespaced reference.
	ControlPlaneRefKonnectNamespacedRef = "konnectNamespacedRef"
	// ControlPlaneRefKIC is the type for KIC ControlPlaneRef.
	// It is used to reference a KIC as Control Plane.
	ControlPlaneRefKIC = "kic"
)

// ControlPlaneRef is the schema for the ControlPlaneRef type.
// It is used to reference a Control Plane entity.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.type) && self.type == 'konnectNamespacedRef') ? has(self.konnectNamespacedRef) : true", message="when type is konnectNamespacedRef, konnectNamespacedRef must be set"
// +kubebuilder:validation:XValidation:rule="(has(self.type) && self.type == 'kic') ? !has(self.konnectNamespacedRef) : true", message="when type is kic, konnectNamespacedRef must not be set"
// +kubebuilder:validation:XValidation:rule="!has(self.type) ? !has(self.konnectNamespacedRef) : true", message="when type is unset, konnectNamespacedRef must not be set"
// +apireference:kgo:include
type ControlPlaneRef struct {
	// Type indicates the type of the control plane being referenced. Allowed values:
	// - konnectNamespacedRef
	// - kic
	//
	// The default is kic, which implies that the Control Plane is KIC.
	//
	// +kubebuilder:validation:Enum=konnectNamespacedRef;kic
	// +kubebuilder:default:=kic
	Type string `json:"type,omitempty"`

	// KonnectNamespacedRef is a reference to a Konnect Control Plane entity inside the cluster.
	// It contains the name of the Konnect Control Plane.
	// This field is required when the Type is konnectNamespacedRef.
	// +optional
	KonnectNamespacedRef *KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`
}

// KonnectIDType is the schema for the KonnectID type.
//
// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}(?:\-[0-9a-f]{4}){3}-[0-9a-f]{12}$`
type KonnectIDType string

// KonnectNamespacedRef is the schema for the KonnectNamespacedRef type.
//
// +kubebuilder:object:generate=true
// +apireference:kgo:include
type KonnectNamespacedRef struct {
	// Name is the name of the Konnect Control Plane.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// TODO: Implement cross namespace references:
	// https://github.com/Kong/kubernetes-configuration/issues/36

	// Namespace is the namespace where the Konnect Control Plane is in.
	// Currently only cluster scoped resources (KongVault) are allowed to set `konnectNamespacedRef.namespace`.
	//
	// +optional
	Namespace string `json:"namespace,omitempty"`
}
