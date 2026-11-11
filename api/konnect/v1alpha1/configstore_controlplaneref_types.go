package v1alpha1

import commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"

// ConfigStoreControlPlaneRef references the Konnect Control Plane that owns a
// KonnectConfigStore, using the same konnectNamespacedRef format as KongVault
// and KongCertificate.
//
// +kong:channels=kong-operator
type ConfigStoreControlPlaneRef struct {
	// Type must be konnectNamespacedRef.
	//
	// +required
	// +kubebuilder:validation:Enum=konnectNamespacedRef
	Type string `json:"type,omitempty"`

	// KonnectNamespacedRef references a KonnectGatewayControlPlane by name.
	// An omitted namespace defaults to the KonnectConfigStore's namespace.
	//
	// +required
	KonnectNamespacedRef *commonv1alpha1.KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`
}

// ToObjectRef converts the reference for generic parent resolution.
func (r ConfigStoreControlPlaneRef) ToObjectRef() commonv1alpha1.ObjectRef {
	ref := commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef}
	if r.KonnectNamespacedRef == nil {
		return ref
	}
	ref.NamespacedRef = &commonv1alpha1.NamespacedRef{
		Name: r.KonnectNamespacedRef.Name,
	}
	if ns := r.KonnectNamespacedRef.Namespace; ns != "" {
		ref.NamespacedRef.Namespace = new(ns)
	}
	return ref
}

// ConfigStoreControlPlaneRefFromObjectRef converts a generic parent reference.
func ConfigStoreControlPlaneRefFromObjectRef(ref commonv1alpha1.ObjectRef) ConfigStoreControlPlaneRef {
	r := ConfigStoreControlPlaneRef{Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef}
	if ref.NamespacedRef == nil {
		return r
	}
	r.KonnectNamespacedRef = &commonv1alpha1.KonnectNamespacedRef{
		Name: ref.NamespacedRef.Name,
	}
	if ref.NamespacedRef.Namespace != nil {
		r.KonnectNamespacedRef.Namespace = *ref.NamespacedRef.Namespace
	}
	return r
}

// GetControlPlaneRef returns the normalized reference to the parent Control Plane.
func (obj *KonnectConfigStore) GetControlPlaneRef() commonv1alpha1.ObjectRef {
	return obj.GetParentRef()
}
