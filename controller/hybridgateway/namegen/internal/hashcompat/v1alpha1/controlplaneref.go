// Package v1alpha1 keeps the shape that generated hybrid gateway names were
// hashed from.
//
// Hybrid gateway names its generated Kong entities with a hash of the
// ControlPlaneRef as printed by k8s.io/apimachinery/pkg/util/dump.ForHash. That
// text includes the type name and every field, so adding a field to
// commonv1alpha1.ControlPlaneRef would change every generated name and make the
// operator recreate the entities on upgrade. Names are hashed from this copy of
// the original shape instead. The package name (v1alpha1), the type name
// (ControlPlaneRef) and the field names and order are part of the hashed text:
// do not change them.
package v1alpha1

import (
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// ControlPlaneRef is the original shape of commonv1alpha1.ControlPlaneRef,
// used only as hash input for generated names.
type ControlPlaneRef struct {
	Type                 string
	KonnectNamespacedRef *commonv1alpha1.KonnectNamespacedRef
}
