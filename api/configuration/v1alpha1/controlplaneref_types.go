package v1alpha1

import (
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

// NOTE: ControlPlaneRef is not a type alias because that doesn't work with crd-ref-docs.
// 2025-02-12T11:41:26.708Z	ERROR	crd-ref-docs	Failed to process source directory	{"error": "type not loaded: github.com/kong/kubernetes-configuration/api/common/v1alpha1.ControlPlaneRef"}

// ControlPlaneRef is the schema for the ControlPlaneRef type.
// It is used to reference a Control Plane entity.
type ControlPlaneRef commonv1alpha1.ControlPlaneRef

// KonnectNamespacedRef is the schema for the KonnectNamespacedRef type.
type KonnectNamespacedRef = commonv1alpha1.KonnectNamespacedRef

const (
	// ControlPlaneRefKonnectID is the type for the KonnectID ControlPlaneRef.
	// It is used to reference a Konnect Control Plane entity by its ID on the Konnect platform.
	ControlPlaneRefKonnectID = commonv1alpha1.ControlPlaneRefKonnectID
	// ControlPlaneRefKonnectNamespacedRef is the type for the KonnectNamespacedRef ControlPlaneRef.
	// It is used to reference a Konnect Control Plane entity inside the cluster
	// using a namespaced reference.
	ControlPlaneRefKonnectNamespacedRef = commonv1alpha1.ControlPlaneRefKonnectNamespacedRef
	// ControlPlaneRefKIC is the type for KIC ControlPlaneRef.
	// It is used to reference a KIC as Control Plane.
	ControlPlaneRefKIC = commonv1alpha1.ControlPlaneRefKIC
)
