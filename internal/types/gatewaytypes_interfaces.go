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

// SupportedRouteList defines a list of supported route.
type SupportedRouteList interface {
	TLSRouteList | HTTPRouteList
}

// SupportedRouteListPtr defines a pointer of a supported route list.
// It includes the client.ObjectList interface to be used as the receiver in the client.List.
type SupportedRouteListPtr[T SupportedRouteList] interface {
	*T
	client.ObjectList
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

// GetSpecHostnames returns the hostnames in the route spec.
func GetSpecHostnames[T SupportedRoute](route T) []Hostname {
	switch r := any(route).(type) {
	case HTTPRoute:
		return r.Spec.Hostnames
	case TLSRoute:
		return r.Spec.Hostnames
	}
	return []Hostname{}
}
