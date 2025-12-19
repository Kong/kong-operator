package namegen

import (
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// httpProcolPrefix is the prefix used for HTTP-related resources.
	httpProcolPrefix = "http"

	// defaultCPPrefix is the prefix used when including a control-plane identifier.
	defaultCPPrefix = "cp"

	// namegenPrefix is used as the prefix for hashed names when concatenated
	// components exceed the maximum Kubernetes resource name length.
	namegenPrefix = "ngn"

	// certPrefix is the default prefix for KongCertificate names.
	certPrefix = "cert"

	// maxLen is the maximum length for Kubernetes resource names.
	maxLen = 253
)

// newName generates a name by concatenating the given components if the length is within the limit of
// Kubernetes resource names, otherwise it returns a hashed version of the concatenated elements.
func newName(elements ...string) string {
	if name := strings.Join(elements, "."); len(name) <= maxLen {
		return name
	}

	// If the name exceeds the max length, return a hashed version of the concatenated elements
	return namegenPrefix + utils.Hash64(elements)
}

// NewKongUpstreamName generates a KongUpstream name based on the ControlPlaneRef and HTTPRouteRule passed as arguments.
func NewKongUpstreamName(cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return newName(
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.BackendRefs),
	)
}

// NewKongServiceName generates a KongService name based on the ControlPlaneRef and HTTPRouteRule passed as arguments.
func NewKongServiceName(cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return newName(
		httpProcolPrefix,
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.BackendRefs),
	)
}

// NewKongRouteName generates a KongRoute name based on the HTTPRoute, ControlPlaneRef, and HTTPRouteRule passed as arguments.
func NewKongRouteName(route *gwtypes.HTTPRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return newName(
		httpProcolPrefix,
		route.Namespace+"-"+route.Name,
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.Matches),
	)
}

// NewKongPluginName generates a KongPlugin name based on the HTTPRouteFilter passed as argument.
func NewKongPluginName(filter gatewayv1.HTTPRouteFilter) string {
	return newName("pl" + utils.Hash32(filter))
}

// NewKongPluginBindingName generates a KongPlugin name based on the KongRoute and the KongPlugin names.
func NewKongPluginBindingName(routeID, pluginId string) string {
	return newName(routeID, pluginId)
}

// NewKongTargetName generates a KongTarget name based on the KongUpstream name, the Service Endpoint ip,
// the service port and the HTTPBackendRef.
func NewKongTargetName(upstreamID, endpointID string, port int, br *gwtypes.HTTPBackendRef) string {
	obj := struct {
		endpointID string
		port       int
		backend    *gwtypes.HTTPBackendRef
	}{
		endpointID: endpointID,
		port:       port,
		backend:    br,
	}
	return newName(upstreamID, utils.Hash32(obj))
}

// NewKongCertificateName generates a KongCertificate name based on the Gateway name and listener port.
// It uses the hybrid naming approach: readable names for short combinations, hashed names for long ones.
func NewKongCertificateName(gatewayName, listenerPort string) string {
	return newName(
		certPrefix,
		gatewayName,
		listenerPort,
	)
}
