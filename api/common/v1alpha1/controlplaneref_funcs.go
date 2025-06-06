package v1alpha1

import (
	"fmt"
)

func (r *ControlPlaneRef) String() string {
	if r == nil {
		return "<nil>"
	}
	switch r.Type {
	case ControlPlaneRefKonnectNamespacedRef:
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
