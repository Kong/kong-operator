package namegen

import (
	"fmt"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/samber/lo"
)

const (
	// httpProcolPrefix is the prefix used for HTTP-related resources.
	httpProcolPrefix = "http"

	// tlsProtocolPrefix is the prefix used for TLS-related resources.
	tlsProtocolPrefix = "tls"

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

func newNameWithHashSuffix(readableElements []string, hashElements []string) string {
	allElements := append(append([]string{}, readableElements...), hashElements...)
	name := strings.Join(allElements, ".")
	// If the name is too long, truncate it.
	if len(name) > maxLen {
		// No hash elements: fall back to a deterministic hash of everything.
		if len(hashElements) == 0 {
			return namegenPrefix + utils.Hash64(allElements)
		}

		hashPart := strings.Join(hashElements, ".")
		// If the hash part alone is too long, also fall back to hashing everything.
		if len(hashPart) > maxLen {
			return namegenPrefix + utils.Hash64(allElements)
		}

		remaining := maxLen - len(hashPart) - 1 // space for readable + "." + hashPart
		// Not enough room for "<readable>." prefix or no readable elements: return just the hash part.
		if remaining <= 0 || len(readableElements) == 0 {
			return hashPart
		}

		readablePart := strings.Join(readableElements, ".")
		if len(readablePart) > remaining {
			readablePart = strings.TrimRight(readablePart[:remaining], ".-")
		}

		return readablePart + "." + hashPart
	}

	return name
}

// NewKongUpstreamName generates a KongUpstream name based on the ControlPlaneRef and rul passed as arguments.
func NewKongUpstreamName[T gwtypes.SupportedRoute, R gwtypes.SupportedRouteRule](
	route T, cp *commonv1alpha1.ControlPlaneRef, rule R) string {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			// REVIEW: change the signature of the function and return error here?
			return ""
		}
		readableElements := append(
			[]string{httpProcolPrefix},
			httpBackendRefDisplayNames(route.GetNamespace(), httpRule.BackendRefs)...,
		)
		hashElements := hashElementsForServiceLikeName(r, cp, httpRule)
		return newNameWithHashSuffix(readableElements, hashElements)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			// REVIEW: change the signature of the function and return error here?
			return ""
		}
		readableElements := append(
			[]string{tlsProtocolPrefix},
			backendRefDisplayNames(r.Namespace, tlsRule.BackendRefs)...,
		)

		hashElements := []string{
			defaultCPPrefix + utils.Hash32(cp),
			// Since `backendRefs` are required in TLSRoute and has at least 1 element,
			// we can directly hash the backendRefs to generate the hash part in the name.
			utils.Hash32(tlsRule.BackendRefs),
		}
		return newNameWithHashSuffix(readableElements, hashElements)
	}
	return ""
}

// NewKongServiceName generates a KongService name based on the ControlPlaneRef and HTTPRouteRule passed as arguments.
func NewKongServiceName[T gwtypes.SupportedRoute, R gwtypes.SupportedRouteRule](
	route T, cp *commonv1alpha1.ControlPlaneRef, rule R,
) string {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return ""
		}
		readableElements := append(
			[]string{httpProcolPrefix},
			httpBackendRefDisplayNames(r.Namespace, httpRule.BackendRefs)...,
		)
		hashElements := hashElementsForServiceLikeName(r, cp, httpRule)
		return newNameWithHashSuffix(readableElements, hashElements)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return ""
		}
		readableElements := append(
			[]string{tlsProtocolPrefix},
			backendRefDisplayNames(r.Namespace, tlsRule.BackendRefs)...,
		)

		hashElements := []string{
			defaultCPPrefix + utils.Hash32(cp),
			// Since `backendRefs` are required in TLSRoute and has at least 1 element,
			// we can directly hash the backendRefs to generate the hash part in the name.
			utils.Hash32(tlsRule.BackendRefs),
		}
		return newNameWithHashSuffix(readableElements, hashElements)
	}
	return ""
}

func hashElementsForServiceLikeName(
	route *gwtypes.HTTPRoute,
	cp *commonv1alpha1.ControlPlaneRef,
	rule gatewayv1.HTTPRouteRule,
) []string {
	var hash string
	if len(rule.BackendRefs) > 0 {
		hash = utils.Hash32(rule.BackendRefs)
	} else {
		zeroBackendRuleIdentity := struct {
			RouteNamespace string
			RouteName      string
			Matches        []gatewayv1.HTTPRouteMatch
			Filters        []gatewayv1.HTTPRouteFilter
		}{
			RouteNamespace: route.Namespace,
			RouteName:      route.Name,
			Matches:        rule.Matches,
			Filters:        rule.Filters,
		}
		hash = utils.Hash32(zeroBackendRuleIdentity)
	}

	return []string{
		defaultCPPrefix + utils.Hash32(cp),
		hash,
	}
}

// NewKongRouteName generates a KongRoute name based on the HTTPRoute, ControlPlaneRef, and HTTPRouteRule passed as arguments.
func NewKongRouteName[T gwtypes.SupportedRoute, R gwtypes.SupportedRouteRule](
	route T, cp *commonv1alpha1.ControlPlaneRef, rule R) string {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return ""
		}
		readableElements := []string{
			httpProcolPrefix,
			r.Namespace + "-" + r.Name,
		}
		hashElements := []string{
			defaultCPPrefix + utils.Hash32(cp),
			utils.Hash32(httpRule.Matches),
		}
		return newNameWithHashSuffix(readableElements, hashElements)
	case *gwtypes.TLSRoute:
		readableElements := append(
			[]string{tlsProtocolPrefix},
			r.Namespace, r.Namespace+"-"+r.Name,
		)
		hashElements := []string{
			defaultCPPrefix + utils.Hash32(cp),
			utils.Hash32(rule),
		}
		return newNameWithHashSuffix(readableElements, hashElements)
	}
	return ""
}

// NewKongRouteNameForMatch generates a KongRoute name based on HTTPRoute, ControlPlaneRef,
// and a single HTTPRouteMatch. The optional index is included to avoid collisions when
// multiple matches are identical.
func NewKongRouteNameForMatch(route *gwtypes.HTTPRoute, cp *commonv1alpha1.ControlPlaneRef, match gatewayv1.HTTPRouteMatch, index int) string {
	readableElements := []string{
		httpProcolPrefix,
		route.Namespace + "-" + route.Name,
	}
	hashElements := []string{
		defaultCPPrefix + utils.Hash32(cp),
		utils.Hash32(match),
		fmt.Sprintf("m%02d", index),
	}
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongPluginName generates a KongPlugin name based on the HTTPRouteFilter passed as argument.
func NewKongPluginName(filter gatewayv1.HTTPRouteFilter, namespace string, pluginName string) string {
	return newName(namespace, pluginName, utils.Hash32(filter))
}

// NewKongPluginBindingName generates a KongPlugin name based on the KongRoute and the KongPlugin names.
func NewKongPluginBindingName(routeID, pluginID string) string {
	return newName(routeID, pluginID)
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

func KongTargetNameForBackendRef(upstreamID, endpointID string, port int, br *gwtypes.BackendRef) string {
	obj := struct {
		endpointID string
		port       int
		backend    *gwtypes.BackendRef
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

func httpBackendRefDisplayNames(routeNamespace string, refs []gatewayv1.HTTPBackendRef) []string {
	return backendRefDisplayNames(routeNamespace, lo.Map(refs, func(ref gatewayv1.HTTPBackendRef, _ int) gatewayv1.BackendRef {
		return ref.BackendRef
	}))
}

func backendRefDisplayNames(routeNamespace string, refs []gatewayv1.BackendRef) []string {
	if len(refs) == 0 {
		return nil
	}

	var name string
	for _, ref := range refs {
		displayName := backendRefDisplayName(routeNamespace, ref)
		if displayName == "" {
			continue
		}
		if displayName < name || name == "" {
			name = displayName
		}
	}
	if len(name) == 0 {
		return nil
	}
	count := fmt.Sprintf("%d", len(refs))
	return []string{name, count}
}

func backendRefDisplayName(routeNamespace string, ref gatewayv1.BackendRef) string {
	name := string(ref.Name)
	if name == "" {
		return ""
	}

	namespace := routeNamespace
	if ref.Namespace != nil && string(*ref.Namespace) != "" {
		namespace = string(*ref.Namespace)
	}

	parts := make([]string, 0, 4)
	if ref.Kind != nil {
		kind := strings.ToLower(string(*ref.Kind))
		if kind != "" && kind != "service" {
			parts = append(parts, kind)
		}
	}
	if namespace != "" {
		parts = append(parts, namespace)
	}
	parts = append(parts, name)
	if ref.Port != nil {
		parts = append(parts, fmt.Sprintf("%d", *ref.Port))
	}

	return strings.Join(parts, "-")
}
