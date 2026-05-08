package utils

import gwtypes "github.com/kong/kong-operator/v2/internal/types"

// HTTPBackendRefsToBackendRefs unwraps []HTTPBackendRef to []BackendRef.
func HTTPBackendRefsToBackendRefs(refs []gwtypes.HTTPBackendRef) []gwtypes.BackendRef {
	out := make([]gwtypes.BackendRef, len(refs))
	for i, r := range refs {
		out[i] = r.BackendRef
	}
	return out
}
