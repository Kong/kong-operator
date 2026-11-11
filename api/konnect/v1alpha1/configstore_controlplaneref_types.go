package v1alpha1

import commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"

// ConfigStoreControlPlaneRef references the Konnect Control Plane that owns a
// KonnectConfigStore. Use konnectNamespacedRef, matching KongVault and
// KongCertificate. The namespacedRef and konnectID formats remain supported.
// Programmed resources must retain their reference format because the reference
// remains immutable.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true",message="when type is namespacedRef, namespacedRef must be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? !has(self.konnectID) : true",message="when type is namespacedRef, konnectID must not be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectID' ? has(self.konnectID) : true",message="when type is konnectID, konnectID must be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectID' ? !has(self.namespacedRef) : true",message="when type is konnectID, namespacedRef must not be set"
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectNamespacedRef' ? has(self.konnectNamespacedRef) : !has(self.konnectNamespacedRef)",message="konnectNamespacedRef must be set only when type is konnectNamespacedRef"
// +kubebuilder:validation:XValidation:rule="self.type != 'konnectNamespacedRef' || (!has(self.namespacedRef) && !has(self.konnectID))",message="when type is konnectNamespacedRef, namespacedRef and konnectID must not be set"
// +kong:channels=kong-operator
type ConfigStoreControlPlaneRef struct {
	// Type selects the reference format. Prefer konnectNamespacedRef.
	//
	// +required
	// +kubebuilder:validation:Enum=konnectID;namespacedRef;konnectNamespacedRef
	Type string `json:"type,omitempty"`

	// KonnectNamespacedRef references a KonnectGatewayControlPlane by name.
	// An omitted namespace defaults to the KonnectConfigStore's namespace.
	//
	// +optional
	KonnectNamespacedRef *commonv1alpha1.KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`

	// NamespacedRef references a KonnectGatewayControlPlane by name using the
	// legacy format. Prefer KonnectNamespacedRef for new resources.
	//
	// +optional
	NamespacedRef *commonv1alpha1.NamespacedRef `json:"namespacedRef,omitempty"`

	// KonnectID references a Control Plane by its Konnect ID.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=36
	KonnectID *string `json:"konnectID,omitempty"`
}

// ToObjectRef normalizes both namespaced formats for reference resolution.
func (r ConfigStoreControlPlaneRef) ToObjectRef() commonv1alpha1.ObjectRef {
	if r.Type == commonv1alpha1.ControlPlaneRefKonnectNamespacedRef {
		ref := commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef}
		if r.KonnectNamespacedRef != nil {
			ref.NamespacedRef = &commonv1alpha1.NamespacedRef{
				Name: r.KonnectNamespacedRef.Name,
			}
			if ns := r.KonnectNamespacedRef.Namespace; ns != "" {
				ref.NamespacedRef.Namespace = new(ns)
			}
		}
		return ref
	}
	return commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefType(r.Type),
		NamespacedRef: r.NamespacedRef,
		KonnectID:     r.KonnectID,
	}
}

// ConfigStoreControlPlaneRefFromObjectRef preserves the legacy reference format.
func ConfigStoreControlPlaneRefFromObjectRef(ref commonv1alpha1.ObjectRef) ConfigStoreControlPlaneRef {
	return ConfigStoreControlPlaneRef{
		Type:          string(ref.Type),
		NamespacedRef: ref.NamespacedRef,
		KonnectID:     ref.KonnectID,
	}
}

// GetControlPlaneRef returns the normalized reference to the parent Control Plane.
func (obj *KonnectConfigStore) GetControlPlaneRef() commonv1alpha1.ObjectRef {
	return obj.GetParentRef()
}
