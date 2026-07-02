package namegen

import (
	"fmt"
	"strings"
	"time"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// MaxKongServiceTimeout is the largest timeout Kong accepts for Service connect, read, and
// write timeouts. Kong has no "disable" value, so a zero-duration backendRequest timeout
// (which the Gateway API defines as "no timeout") is mapped to this as the closest emulation.
const MaxKongServiceTimeout int64 = 2147483646

// BackendRequestTimeoutMilliseconds returns the Kong service timeout (in milliseconds) derived
// from an HTTPRoute rule's spec.timeouts.backendRequest, or nil when the rule sets no
// backendRequest timeout (or it cannot be parsed). A zero duration maps to MaxKongServiceTimeout
// per the Gateway API semantics where "0s" disables the timeout.
func BackendRequestTimeoutMilliseconds(rule gatewayv1.HTTPRouteRule) *int64 {
	if rule.Timeouts == nil || rule.Timeouts.BackendRequest == nil {
		return nil
	}
	duration, err := time.ParseDuration(string(*rule.Timeouts.BackendRequest))
	// The value is CEL-validated to a strict subset of time.ParseDuration, so this should not happen.
	if err != nil {
		return nil
	}
	ms := duration.Milliseconds()
	if duration == 0 {
		ms = MaxKongServiceTimeout
	}
	return &ms
}

const (
	// httpProcolPrefix is the prefix used for HTTP-related resources.
	httpProcolPrefix = "http"

	// tlsProtocolPrefix is the prefix for TLS-related resources.
	tlsProtocolPrefix = "tls"

	// defaultCPPrefix is the prefix used when including a control-plane identifier.
	defaultCPPrefix = "cp"

	// parentRefPrefix is the prefix used when including a ParentRef identifier.
	parentRefPrefix = "pr"

	// backendNotFoundPrefix is the prefix used for service names that intentionally
	// route to request-termination because no backend target could be produced.
	backendNotFoundPrefix = "bnf"

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

// NewKongUpstreamNameForHTTPRouteRule generates a KongUpstream name based on the ControlPlaneRef and HTTPRouteRule passed as arguments.
func NewKongUpstreamNameForHTTPRouteRule(route *gwtypes.HTTPRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	readableElements := append(
		[]string{httpProcolPrefix},
		backendRefDisplayNames(route.Namespace, rule.BackendRefs)...,
	)
	hashElements := hashElementsForServiceLikeName(route, cp, rule)
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongUpstreamNameForTLSRouteRule generates a KongUpstream name based on the ControlPlaneRef and TLSRouteRule passed as arguments.
func NewKongUpstreamNameForTLSRouteRule(route *gwtypes.TLSRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.TLSRouteRule) string {
	readableElements := append(
		[]string{tlsProtocolPrefix},
		backendRefDisplayNames(route.Namespace, rule.BackendRefs)...,
	)
	hashElements := hashElementsForServiceLikeNameTLSRouteRule(cp, rule)
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongServiceNameForHTTPRouteRule generates a KongService name based on the ControlPlaneRef and HTTPRouteRule passed as arguments.
func NewKongServiceNameForHTTPRouteRule(route *gwtypes.HTTPRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.HTTPRouteRule) string {
	readableElements := append(
		[]string{httpProcolPrefix},
		backendRefDisplayNames(route.Namespace, rule.BackendRefs)...,
	)
	hashElements := hashElementsForServiceLikeName(route, cp, rule)
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongServiceNameForHTTPRouteRuleBackendNotFound generates a route-scoped KongService name
// for rules that have BackendRefs but no valid backend targets. These services get a
// request-termination plugin, so they must not share the normal backend service name.
func NewKongServiceNameForHTTPRouteRuleBackendNotFound(
	route *gwtypes.HTTPRoute,
	cp *commonv1alpha1.ControlPlaneRef,
	rule gatewayv1.HTTPRouteRule,
) string {
	readableElements := append(
		[]string{httpProcolPrefix},
		backendRefDisplayNames(route.Namespace, rule.BackendRefs)...,
	)
	hashElements := []string{
		defaultCPPrefix + utils.Hash32(cp),
		backendNotFoundPrefix + utils.Hash32(struct {
			Namespace string
			Name      string
		}{
			Namespace: route.Namespace,
			Name:      route.Name,
		}),
		hashForHTTPRouteRuleServiceLikeName(route, rule),
	}
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongServiceNameForTLSRouteRule generates a KongService name based on the ControlPlaneRef and TLSRouteRule passed as arguments.
func NewKongServiceNameForTLSRouteRule(route *gwtypes.TLSRoute, cp *commonv1alpha1.ControlPlaneRef, rule gatewayv1.TLSRouteRule) string {
	readableElements := append(
		[]string{tlsProtocolPrefix},
		backendRefDisplayNames(route.Namespace, rule.BackendRefs)...,
	)
	hashElements := hashElementsForServiceLikeNameTLSRouteRule(cp, rule)
	return newNameWithHashSuffix(readableElements, hashElements)
}

func hashElementsForServiceLikeName(
	route *gwtypes.HTTPRoute,
	cp *commonv1alpha1.ControlPlaneRef,
	rule gatewayv1.HTTPRouteRule,
) []string {
	hash := hashForHTTPRouteRuleServiceLikeName(route, rule)
	return []string{
		defaultCPPrefix + utils.Hash32(cp),
		hash,
	}
}

func hashForHTTPRouteRuleServiceLikeName(route *gwtypes.HTTPRoute, rule gatewayv1.HTTPRouteRule) string {
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

	// Fold the backendRequest timeout into the hash so rules sharing the same backends but
	// requesting different timeouts map to distinct KongServices (a KongService can only carry a
	// single timeout). Rules without a backendRequest timeout keep the original hash, so enabling
	// the feature does not rename existing KongServices.
	if timeoutMS := BackendRequestTimeoutMilliseconds(rule); timeoutMS != nil {
		hash = utils.Hash32(struct {
			Base                    string
			BackendRequestTimeoutMS int64
		}{
			Base:                    hash,
			BackendRequestTimeoutMS: *timeoutMS,
		})
	}

	return hash
}

func hashElementsForServiceLikeNameTLSRouteRule(
	cp *commonv1alpha1.ControlPlaneRef,
	rule gwtypes.TLSRouteRule,
) []string {
	// Since in TLSRoute, `backendRefs` list is required in rules and has at least one backend reference,
	// We can directly run hash on backendRefs.
	hash := utils.Hash32(rule.BackendRefs)
	return []string{
		defaultCPPrefix + utils.Hash32(cp),
		hash,
	}
}

// NewKongRouteNameForMatch generates a KongRoute name based on HTTPRoute, ControlPlaneRef,
// ParentRef, and a single HTTPRouteMatch. The optional index is included to avoid collisions
// when multiple matches are identical.
func NewKongRouteNameForMatch(
	route *gwtypes.HTTPRoute,
	cp *commonv1alpha1.ControlPlaneRef,
	parentRef *gwtypes.ParentReference,
	match gatewayv1.HTTPRouteMatch,
	index int,
) string {
	readableElements := []string{
		httpProcolPrefix,
		route.Namespace + "-" + route.Name,
	}
	hashElements := []string{defaultCPPrefix + utils.Hash32(cp)}
	if parentRef != nil {
		hashElements = append(hashElements, parentRefHashElement(parentRef))
	}
	hashElements = append(hashElements, utils.Hash32(match), fmt.Sprintf("m%02d", index))
	return newNameWithHashSuffix(readableElements, hashElements)
}

// NewKongRouteNameForTLSRouteRule generates a KongRoute name based on the TLSRoute rule,
// ControlPlaneRef, ParentRef, and its parent TLSRoute.
func NewKongRouteNameForTLSRouteRule(
	route *gwtypes.TLSRoute,
	cp *commonv1alpha1.ControlPlaneRef,
	parentRef *gwtypes.ParentReference,
	rule gatewayv1.TLSRouteRule,
) string {
	readableElements := []string{
		tlsProtocolPrefix,
		route.Namespace + "-" + route.Name,
	}
	hashElements := []string{defaultCPPrefix + utils.Hash32(cp)}
	if parentRef != nil {
		hashElements = append(hashElements, parentRefHashElement(parentRef))
	}
	hashElements = append(hashElements, utils.Hash32(rule))
	return newNameWithHashSuffix(readableElements, hashElements)
}

func parentRefHashElement(parentRef *gwtypes.ParentReference) string {
	return parentRefPrefix + utils.Hash32(*parentRef)
}

// NewKongPluginName generates a KongPlugin name based on the HTTPRouteFilter passed as argument.
func NewKongPluginName(filter gatewayv1.HTTPRouteFilter, namespace string, pluginName string) string {
	return newName(namespace, pluginName, utils.Hash32(filter))
}

// NewKongPluginNameForFilters generates a KongPlugin name for a plugin produced by one or more
// HTTPRoute filters. When several filters map to the same Kong plugin type (e.g. URLRewrite and
// RequestHeaderModifier both map to request-transformer) they are merged into a single KongPlugin,
// so the name must be derived from the whole set of contributing filters. The single-filter case
// is kept identical to NewKongPluginName to avoid renaming existing resources.
func NewKongPluginNameForFilters(filters []gatewayv1.HTTPRouteFilter, namespace string, pluginName string) string {
	if len(filters) == 1 {
		return NewKongPluginName(filters[0], namespace, pluginName)
	}
	return newName(namespace, pluginName, utils.Hash32(filters))
}

// NewKongPluginNameForService generates a KongPlugin name tied to a KongService.
func NewKongPluginNameForService(serviceName, pluginName string) string {
	return newName(serviceName, pluginName)
}

// NewKongPluginBindingName generates a KongPlugin name based on the KongRoute and the KongPlugin names.
func NewKongPluginBindingName(routeID, pluginID string) string {
	return newName(routeID, pluginID)
}

// NewKongTargetName generates the Kong target name based on the KongUpstream name, the Service Endpoint IP, and the backendRef.
func NewKongTargetName[T gwtypes.SupportedBackendRef](upstreamID, endpointID string, port int, br *T) string {
	switch b := any(br).(type) {
	case *gwtypes.HTTPBackendRef:
		return newKongTargetNameForHTTPBackendRef(upstreamID, endpointID, port, b)
	case *gwtypes.BackendRef:
		return newKongTargetNameForBackendRef(upstreamID, endpointID, port, b)
		// TODO: add other types of BackendRefs (like GRPCBackendRef) when we support them.
	}
	// Should be unreachable.
	return ""
}

// newKongTargetNameForHTTPBackendRef generates a KongTarget name based on the KongUpstream name, the Service Endpoint ip,
// the service port and the HTTPBackendRef.
func newKongTargetNameForHTTPBackendRef(upstreamID, endpointID string, port int, br *gwtypes.HTTPBackendRef) string {
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

// newKongTargetNameForBackendRef generates a KongTarget name based on the KongUpstream name, the Service Endpoint ip,
// the service port and the BackendRef.
func newKongTargetNameForBackendRef(upstreamID, endpointID string, port int, br *gwtypes.BackendRef) string {
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

// NewKongCertificateName generates a KongCertificate name based on the Gateway name,
// listener port, and listener name.
// It uses the hybrid naming approach: readable names for short combinations, hashed names for long ones.
func NewKongCertificateName(gatewayName, listenerPort, listenerName string) string {
	return newName(
		certPrefix,
		gatewayName,
		listenerPort,
		listenerName,
	)
}

func backendRefDisplayNames[T gwtypes.SupportedBackendRef](routeNamespace string, refs []T) []string {
	if len(refs) == 0 {
		return nil
	}

	var name string
	for _, ref := range refs {
		var backendRef gwtypes.BackendRef
		switch r := any(ref).(type) {
		case gwtypes.HTTPBackendRef:
			backendRef = r.BackendRef
		case gwtypes.BackendRef:
			backendRef = r
		}
		displayName := backendRefDisplayName(routeNamespace, backendRef)
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
