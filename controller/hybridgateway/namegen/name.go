package namegen

import (
	"fmt"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Name represents a structured naming scheme for Kong entities to be identified by the HTTPRoute, ParentReference,
// and HTTPRoute section from which the resource is derived.
type Name struct {
	httpRouteID    string
	controlPlaneID string
	sectionID      string
}

const defaultHTTPRoutePrefix = "http"
const defaultCPPrefix = "cp"
const defaultResPrefix = "res"

// String returns the full name as a dot-separated string.
func (h *Name) String() string {
	const maxLen = 253 - len("faf385ae") - 1 // reserve space for one extra hash (+ 1 dot) for child resources (e.g., KongTargets)

	parts := []string{}
	if h.httpRouteID != "" {
		parts = append(parts, h.httpRouteID)
	}
	if h.controlPlaneID != "" {
		parts = append(parts, h.controlPlaneID)
	}
	if h.sectionID != "" {
		parts = append(parts, h.sectionID)
	}
	fullName := strings.Join(parts, ".")

	if len(fullName) <= maxLen {
		return fullName
	}

	// If the full name exceeds the max length, we fallback to a hashed version of each component
	const maxCompLen = (maxLen - 2) / 3

	httpRouteID := h.httpRouteID
	parentRefID := h.controlPlaneID
	sectionID := h.sectionID

	if len(httpRouteID) > maxCompLen {
		httpRouteID = defaultHTTPRoutePrefix + utils.Hash32(httpRouteID)
	}
	if len(parentRefID) > maxCompLen {
		parentRefID = defaultCPPrefix + utils.Hash32(parentRefID)
	}
	if len(sectionID) > maxCompLen {
		sectionID = defaultResPrefix + utils.Hash32(sectionID)
	}

	parts = []string{httpRouteID, parentRefID}
	if sectionID != "" {
		parts = append(parts, sectionID)
	}
	fullName = strings.Join(parts, ".")

	return fullName
}

// NewName creates a new Name instance with the given components.
func NewName(httpRouteID, controlPlaneID, sectionID string) *Name {
	return &Name{
		httpRouteID:    httpRouteID,
		controlPlaneID: controlPlaneID,
		sectionID:      sectionID,
	}
}

// NewKongUpstreamName generates a KongUpstream name based on the ControlPlaneRef and HTTPRouteRule.
func NewKongUpstreamName(cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return NewName(
		"",
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.BackendRefs),
	).String()
}

// NewKongServiceName generates a KongService name based on the ControlPlaneRef and HTTPRouteRule.
func NewKongServiceName(cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return NewName(
		"",
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.BackendRefs),
	).String()
}

// NewKongRouteName generates a KongRoute name based on the HTTPRoute, ControlPlaneRef, and HTTPRouteRule.
func NewKongRouteName(route *gwtypes.HTTPRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	return NewName(
		route.Namespace+"-"+route.Name,
		defaultCPPrefix+utils.Hash32(cp),
		utils.Hash32(rule.Matches),
	).String()
}

// NewKongPluginName generates a KongPlugin name based on the HTTPRouteFilter.
func NewKongPluginName(filter gatewayv1.HTTPRouteFilter) string {
	return NewName("", "", "pl"+utils.Hash32(filter)).String()
}

// NewKongPluginBindingName generates a KongPlugin name based on the HTTPRoute, ControlPlaneRef and HTTPRouteFilter.
func NewKongPluginBindingName(routeID, pluginId string) string {
	return NewName(routeID, "", pluginId).String()
}

// NewKongTargetName generates a KongTarget name based on the KongUpstream name, the Service Endpoint ip,
// the service port and the HTTPBackendRef.
func NewKongTargetName(upstreamID, endpointID string, port int, br *gwtypes.HTTPBackendRef) string {
	return NewName(upstreamID, "",
		utils.Hash32(utils.Hash32(br)+fmt.Sprintf("%s:%d", endpointID, port))).String()
}
