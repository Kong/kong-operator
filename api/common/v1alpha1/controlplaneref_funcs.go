package v1alpha1

import (
	"fmt"

	"github.com/samber/lo"
)

func (r *ControlPlaneRef) String() string {
	if r == nil {
		return "<nil>"
	}
	switch r.Type {
	case ControlPlaneRefKonnectID:
		// It's safe to assume KonnectID is not nil as it's guarded by CEL rules, but just in case let's have a fallback.
		konnectID := lo.FromPtrOr(r.KonnectID, "nil")
		return fmt.Sprintf("<%s:%s>", r.Type, konnectID)
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
