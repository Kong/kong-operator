package types

import "sigs.k8s.io/controller-runtime/pkg/client"

// SupportedRoute defines a supported route type.
type SupportedRoute interface {
	TLSRoute | HTTPRoute
}

// SupportedRoutePtr defines a pointer of a supported route type.
// It includes the client.Object interface to extract metadata.
type SupportedRoutePtr[T SupportedRoute] interface {
	*T
	client.Object
}

// GetSpecParentRefs returns the parent references of a supported route.
func GetSpecParentRefs[T SupportedRoute](route T) []ParentReference {
	switch r := any(route).(type) {
	case HTTPRoute:
		return r.Spec.ParentRefs
	case TLSRoute:
		return r.Spec.ParentRefs
	}
	return []ParentReference{}
}
