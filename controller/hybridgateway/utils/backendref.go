package utils

import (
	"github.com/samber/lo"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// HTTPBackendRefsToBackendRefs unwraps []HTTPBackendRef to []BackendRef.
func HTTPBackendRefsToBackendRefs(refs []gwtypes.HTTPBackendRef) []gwtypes.BackendRef {
	out := make([]gwtypes.BackendRef, len(refs))
	for i, r := range refs {
		out[i] = r.BackendRef
	}
	return out
}

// BackendRefGroupKind returns the effective group and kind for a BackendRef,
// applying Gateway API defaults: kind defaults to "Service" and group defaults
// to "" (core) when nil or empty.
func BackendRefGroupKind(group *gwtypes.Group, kind *gwtypes.Kind) (gwtypes.Group, gwtypes.Kind) {
	g := lo.FromPtr(group)
	k := lo.FromPtr(kind)
	if k == "" {
		k = "Service"
	}
	return g, k
}

// IsBackendRefSupported returns true if the BackendRef group and kind are supported by Gateway API.
// Only core "Service" is supported.
func IsBackendRefSupported(group *gwtypes.Group, kind *gwtypes.Kind) bool {
	g, k := BackendRefGroupKind(group, kind)
	return (g == "" || g == "core") && k == "Service"
}
