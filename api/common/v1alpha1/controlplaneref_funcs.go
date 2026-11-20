package v1alpha1

import (
	"fmt"
)

// GetWorkspace returns the Konnect Workspace set on the ControlPlaneRef,
// or an empty string for the Control Plane's default Workspace.
func (r *ControlPlaneRef) GetWorkspace() string {
	if r == nil {
		return ""
	}
	return r.Workspace
}

// String returns the string representation of the ControlPlaneRef.
func (r *ControlPlaneRef) String() string {
	if r == nil {
		return "<nil>"
	}
	switch r.Type {
	case ControlPlaneRefKonnectNamespacedRef:
		if r.KonnectNamespacedRef == nil {
			return fmt.Sprintf("<%s:nil>", r.Type)
		}
		if r.KonnectNamespacedRef.Namespace == "" {
			return fmt.Sprintf("<%s:%s>", r.Type, r.KonnectNamespacedRef.Name)
		}
		return fmt.Sprintf("<%s:%s/%s>", r.Type, r.KonnectNamespacedRef.Namespace, r.KonnectNamespacedRef.Name)
	case ControlPlaneRefKIC:
		return fmt.Sprintf("<%s>", r.Type)
	default:
		return fmt.Sprintf("<unknown:%s>", r.Type)
	}
}
