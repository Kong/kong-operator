package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// GatewaysOnRoute extracts and returns a list of unique Gateway references (in "namespace/name" format)
// from the ParentRefs of the given object.
func GatewaysOnRoute[T gwtypes.SupportedRoute](o client.Object) []string {
	route, ok := o.(T)
	if !ok {
		return nil
	}

	var gateways []string
	parentRefs := gwtypes.GetSpecParentRefs(route)

	for _, parentRef := range parentRefs {
		// Only consider ParentRefs that refer to Gateways
		if parentRef.Group != nil && *parentRef.Group != "" && *parentRef.Group != "gateway.networking.k8s.io" {
			continue
		}
		if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
			continue
		}
		ns := route.GetNamespace()
		if parentRef.Namespace != nil {
			ns = string(*parentRef.Namespace)
		}
		gateways = append(gateways, ns+"/"+string(parentRef.Name))
	}
	return lo.Uniq(gateways)
}

// backendRefToServiceKey returns the key of the Service in the namespace/name format in the backendRef
// if the backendRef reference to a service.
// The second return value is true when the backendRef is a service, and false if the type does not match.
func backendRefToServiceKey(backendRef gwtypes.BackendRef, routeNamespace string) (string, bool) {
	if backendRef.Group != nil && *backendRef.Group != "" && *backendRef.Group != "core" {
		return "", false
	}
	if backendRef.Kind != nil && *backendRef.Kind != "Service" {
		return "", false
	}
	if backendRef.Name == "" || backendRef.Port == nil {
		return "", false
	}
	ns := routeNamespace
	if backendRef.Namespace != nil {
		ns = string(*backendRef.Namespace)
	}
	return ns + "/" + string(backendRef.Name), true
}
