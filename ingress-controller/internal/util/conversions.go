package util

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// -----------------------------------------------------------------------------
// Type conversion Utilities
// -----------------------------------------------------------------------------

// StringToTypedPtr converts a string to pointer to a typed designated by the provided type parameter.
func StringToTypedPtr[
	TT *T,
	T ~string,
](s string) TT {
	ret := T(s)
	return &ret
}

// StringToGatewayAPIKindPtr converts a string to a *gwtypes.Kind.
func StringToGatewayAPIKindPtr(kind string) *gwtypes.Kind {
	return StringToTypedPtr[*gwtypes.Kind](kind)
}
