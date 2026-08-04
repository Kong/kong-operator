package kongroute

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
)

// RoutesForRule creates or updates KongRoutes for the given rule.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongRoutes are derived
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the KongRoutes
//   - serviceName: The name of the KongService these KongRoutes should point to
//   - hostnames: The hostnames for the KongRoutes
//
// Returns:
//   - kongRoutes: The created or updated KongRoute resources.
//   - err: Any error that occurred during the process
func RoutesForRule[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedRouteRule,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	route TPtr,
	rule R,
	ruleIndex int,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	namingParentRef *gwtypes.ParentReference,
	serviceName string,
	hostnames []string,
) (kongRoutes []*configurationv1alpha1.KongRoute, err error) {
	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongRoute: unmatched route type and rule type: %T and %T", route, rule)
		}
		return RoutesForHTTPRouteRule(ctx, logger, cl, r, httpRule, ruleIndex, pRef, cp, namingParentRef, serviceName, hostnames)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongRoute: unmatched route type and rule type: %T and %T", route, rule)
		}
		return routesForTLSRouteRule(ctx, logger, cl, r, tlsRule, pRef, cp, namingParentRef, serviceName, hostnames)
	case *gwtypes.TCPRoute:
		tcpRule, ok := any(rule).(gwtypes.TCPRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongRoute: unmatched route type and rule type: %T and %T", route, rule)
		}
		return routesForTCPRouteRule(ctx, logger, cl, r, tcpRule, pRef, cp, namingParentRef, serviceName)
	}
	return nil, fmt.Errorf("failed to build KongRoute: unsupported route type: %T", route)
}

// RoutesForHTTPRouteRule creates or updates KongRoutes for the given HTTPRoute rule.
// It generates one KongRoute per match in the rule.
//
// Gateway API semantics are:
// - Within a single HTTPRouteRule, entries in .Matches are ORed
// - Within a single HTTPRouteMatch, individual criteria (path/method/headers) are ANDed
//
// To faithfully represent this in Kong, we generate one KongRoute for each HTTPRouteMatch
// and attach only that match's criteria to the route. All routes point to the same KongService.
// This fixes Hybrid Gateway conformance failures such as HTTPRouteMatching, which includes
// rules that combine independent path-only and header-only matches.
func RoutesForHTTPRouteRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	ruleIndex int,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	namingParentRef *gwtypes.ParentReference,
	serviceName string,
	hostnames []string,
) ([]*configurationv1alpha1.KongRoute, error) {
	var kongRoutes []*configurationv1alpha1.KongRoute

	// If the rule has no matches, create a single catch-all route.
	// Kong requires at least one matcher; use "/" path to represent catch-all.
	if len(rule.Matches) == 0 {
		match := gatewayv1.HTTPRouteMatch{
			Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new("/")},
		}
		rule.Matches = append(rule.Matches, match)
	}

	// Derive protocols from the parent Gateway listener(s).
	protocols, err := protocolsFromGatewayListener(ctx, cl, httpRoute, pRef)
	if err != nil {
		return nil, err
	}

	// Check filters to determine if we need capture groups in paths.
	setCaptureGroup := needsCaptureGroup(rule)
	priorities := httpRouteMatchPriorities(httpRoute)

	stripPath, err := metadata.ExtractStripPath(httpRoute.Annotations)
	if err != nil {
		return nil, fmt.Errorf("%w: konghq.com/strip-path on %s/%s: %w",
			hgerrors.ErrMalformedAnnotation, httpRoute.GetNamespace(), httpRoute.GetName(), err)
	}
	preserveHost, err := metadata.ExtractPreserveHost(httpRoute.Annotations)
	if err != nil {
		return nil, fmt.Errorf("%w: konghq.com/preserve-host on %s/%s: %w",
			hgerrors.ErrMalformedAnnotation, httpRoute.GetNamespace(), httpRoute.GetName(), err)
	}
	tags := pkgmetadata.ExtractTags(httpRoute)

	for i, match := range rule.Matches {
		variants := precedenceVariantsForHTTPRouteMatch(httpRoute, match, priorities, ruleIndex, i)
		for variantIndex, variant := range variants {
			nameIndex := i + variantIndex*len(rule.Matches)
			routeName := namegen.NewKongRouteNameForMatch(httpRoute, cp, namingParentRef, variant, nameIndex)
			mLog := logger.WithValues("kongroute", routeName, "matchIndex", i, "precedenceVariant", variantIndex)
			log.Debug(mLog, "Creating KongRoute for HTTPRoute match")

			routeBuilder := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
				WithLabels(httpRoute, pRef).
				WithAnnotations(httpRoute, pRef).
				WithSpecName(routeName).
				WithProtocols(protocols...).
				WithHosts(hostnames).
				WithStripPath(stripPath).
				WithPreserveHost(preserveHost).
				WithSpecTags(tags).
				WithKongService(serviceName).
				WithHTTPRouteMatch(variant, setCaptureGroup)
			if priority := priorityForTraditionalHTTPRouteMatch(match, priorities, ruleIndex, i); priority != nil {
				routeBuilder.WithRegexPriority(priority)
			}

			newRoute, buildErr := routeBuilder.Build()
			if buildErr != nil {
				log.Error(mLog, buildErr, "Failed to build KongRoute resource")
				return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, buildErr)
			}

			if _, updErr := translator.VerifyAndUpdate(ctx, mLog, cl, &newRoute, httpRoute, true); updErr != nil {
				return nil, updErr
			}

			kongRoutes = append(kongRoutes, newRoute.DeepCopy())
		}
	}

	return kongRoutes, nil
}

type httpRouteMatchPriorityKey struct {
	ruleIndex  int
	matchIndex int
}

type httpRoutePriorityClass struct {
	pathType       gatewayv1.PathMatchType
	pathLength     int
	hasMethodMatch bool
	headerCount    int
}

func httpRouteMatchPriorities(httpRoute *gwtypes.HTTPRoute) map[httpRouteMatchPriorityKey]int64 {
	classes := make([]httpRoutePriorityClass, 0)
	classSet := make(map[httpRoutePriorityClass]struct{})
	matchCountByClass := make(map[httpRoutePriorityClass]int)
	for _, rule := range httpRoute.Spec.Rules {
		for _, match := range rule.Matches {
			class := calculateHTTPRoutePriorityClass(match)
			if _, ok := classSet[class]; !ok {
				classSet[class] = struct{}{}
				classes = append(classes, class)
			}
			matchCountByClass[class]++
		}
	}

	slices.SortFunc(classes, compareHTTPRoutePriorityClass)
	rankByClass := make(map[httpRoutePriorityClass]int64, len(classes))
	for rank, class := range classes {
		rankByClass[class] = int64(rank)
	}

	// priorityClassSize bounds the number of matches that can share a single
	// specificity class without their offsets overflowing into the adjacent
	// class' range. Gateway API caps the matches per HTTPRoute well below this,
	// so the assumption holds in practice.
	const priorityClassSize = int64(1 << 10)
	seenByClass := make(map[httpRoutePriorityClass]int)
	priorities := make(map[httpRouteMatchPriorityKey]int64)
	for ruleIndex, rule := range httpRoute.Spec.Rules {
		for matchIndex, match := range rule.Matches {
			class := calculateHTTPRoutePriorityClass(match)
			offset := matchCountByClass[class] - 1 - seenByClass[class]
			seenByClass[class]++
			priorities[httpRouteMatchPriorityKey{
				ruleIndex:  ruleIndex,
				matchIndex: matchIndex,
			}] = rankByClass[class]*priorityClassSize + int64(offset)
		}
	}
	return priorities
}

func priorityForTraditionalHTTPRouteMatch(
	match gatewayv1.HTTPRouteMatch,
	priorities map[httpRouteMatchPriorityKey]int64,
	ruleIndex, matchIndex int,
) *int64 {
	if isDefaultPathHTTPRouteMatch(match) || match.Path == nil || match.Path.Value == nil {
		return nil
	}
	pathType := gatewayv1.PathMatchPathPrefix
	if match.Path.Type != nil {
		pathType = *match.Path.Type
	}
	// Gateway API leaves RegularExpression precedence implementation-specific.
	if pathType == gatewayv1.PathMatchRegularExpression {
		return nil
	}
	priority := priorityForHTTPRouteMatch(priorities, ruleIndex, matchIndex)
	return &priority
}

// precedenceVariantsForHTTPRouteMatch returns the original match plus specialized copies that
// include the non-path constraints of lower-priority matches it can overlap. Kong's traditional
// router considers the number of populated match fields before regex_priority. Without these
// copies a less-specific method or header match can beat a more-specific path match regardless of
// regex_priority. The specialized copy has at least the same native Kong specificity as the lower
// match, after which path length/type and regex_priority preserve Gateway API precedence. The
// original copy remains necessary for requests that do not satisfy any lower match's constraints.
func precedenceVariantsForHTTPRouteMatch(
	httpRoute *gwtypes.HTTPRoute,
	match gatewayv1.HTTPRouteMatch,
	priorities map[httpRouteMatchPriorityKey]int64,
	ruleIndex, matchIndex int,
) []gatewayv1.HTTPRouteMatch {
	variants := []gatewayv1.HTTPRouteMatch{*match.DeepCopy()}
	priority := priorityForHTTPRouteMatch(priorities, ruleIndex, matchIndex)

	for lowerRuleIndex, rule := range httpRoute.Spec.Rules {
		for lowerMatchIndex, lower := range rule.Matches {
			if priorityForHTTPRouteMatch(priorities, lowerRuleIndex, lowerMatchIndex) >= priority {
				continue
			}
			if !needsPrecedenceVariant(match, lower) {
				continue
			}
			variant, changed, overlaps := augmentHTTPRouteMatch(match, lower)
			if !changed || !overlaps || slices.ContainsFunc(variants, func(existing gatewayv1.HTTPRouteMatch) bool {
				return reflect.DeepEqual(existing, variant)
			}) {
				continue
			}
			variants = append(variants, variant)
		}
	}

	return variants
}

func needsPrecedenceVariant(higher, lower gatewayv1.HTTPRouteMatch) bool {
	higherWeight := traditionalKongMatchWeight(higher)
	lowerWeight := traditionalKongMatchWeight(lower)
	if lowerWeight != higherWeight {
		return lowerWeight > higherWeight
	}
	return countEffectiveHTTPHeaderMatches(lower.Headers) > countEffectiveHTTPHeaderMatches(higher.Headers)
}

// traditionalKongMatchWeight returns the portion of Kong's traditional-compatible priority that
// is determined before regex_priority for the fields produced from an HTTPRouteMatch. Hostnames
// and protocols are omitted because they are identical for every match of the parent HTTPRoute.
func traditionalKongMatchWeight(match gatewayv1.HTTPRouteMatch) int {
	weight := 0
	if match.Path != nil && match.Path.Value != nil {
		weight++
	}
	if match.Method != nil {
		weight++
	}
	if len(match.Headers) > 0 {
		weight++
	}
	return weight
}

func augmentHTTPRouteMatch(
	higher gatewayv1.HTTPRouteMatch,
	lower gatewayv1.HTTPRouteMatch,
) (gatewayv1.HTTPRouteMatch, bool, bool) {
	if !httpRoutePathsCanOverlap(higher.Path, lower.Path) {
		return gatewayv1.HTTPRouteMatch{}, false, false
	}

	variant := *higher.DeepCopy()
	changed := false
	if variant.Method != nil && lower.Method != nil && *variant.Method != *lower.Method {
		return gatewayv1.HTTPRouteMatch{}, false, false
	}
	if variant.Method == nil && lower.Method != nil {
		variant.Method = new(*lower.Method)
		changed = true
	}

	for _, lowerHeader := range lower.Headers {
		matchingHeader := slices.IndexFunc(variant.Headers, func(h gatewayv1.HTTPHeaderMatch) bool {
			return strings.EqualFold(string(h.Name), string(lowerHeader.Name))
		})
		if matchingHeader < 0 {
			variant.Headers = append(variant.Headers, *lowerHeader.DeepCopy())
			changed = true
			continue
		}
		if !httpHeaderMatchesEqual(variant.Headers[matchingHeader], lowerHeader) {
			// Exact matches with different values cannot overlap. Intersections involving
			// regular-expression header matches cannot be represented by a KongRoute header
			// value list (which is ORed), so leave those implementation-specific cases alone.
			return gatewayv1.HTTPRouteMatch{}, false, false
		}
	}

	return variant, changed, true
}

func httpHeaderMatchesEqual(a, b gatewayv1.HTTPHeaderMatch) bool {
	aType := gatewayv1.HeaderMatchExact
	if a.Type != nil {
		aType = *a.Type
	}
	bType := gatewayv1.HeaderMatchExact
	if b.Type != nil {
		bType = *b.Type
	}
	return strings.EqualFold(string(a.Name), string(b.Name)) && aType == bType && a.Value == b.Value
}

func httpRoutePathsCanOverlap(a, b *gatewayv1.HTTPPathMatch) bool {
	aType, aValue := normalizedHTTPPathMatch(a)
	bType, bValue := normalizedHTTPPathMatch(b)
	if aType == gatewayv1.PathMatchRegularExpression || bType == gatewayv1.PathMatchRegularExpression {
		return true
	}
	if aType == gatewayv1.PathMatchExact && bType == gatewayv1.PathMatchExact {
		return aValue == bValue
	}
	if aType == gatewayv1.PathMatchExact {
		return httpPathPrefixMatches(bValue, aValue)
	}
	if bType == gatewayv1.PathMatchExact {
		return httpPathPrefixMatches(aValue, bValue)
	}
	return httpPathPrefixMatches(aValue, bValue) || httpPathPrefixMatches(bValue, aValue)
}

func normalizedHTTPPathMatch(path *gatewayv1.HTTPPathMatch) (gatewayv1.PathMatchType, string) {
	if path == nil || path.Value == nil {
		return gatewayv1.PathMatchPathPrefix, "/"
	}
	pathType := gatewayv1.PathMatchPathPrefix
	if path.Type != nil {
		pathType = *path.Type
	}
	return pathType, *path.Value
}

func httpPathPrefixMatches(prefix, path string) bool {
	if prefix == "/" || prefix == path {
		return true
	}
	prefix = strings.TrimSuffix(prefix, "/")
	return strings.HasPrefix(path, prefix+"/")
}

func isDefaultPathHTTPRouteMatch(match gatewayv1.HTTPRouteMatch) bool {
	if match.Path == nil {
		return true
	}

	pathType := gatewayv1.PathMatchPathPrefix
	if match.Path.Type != nil {
		pathType = *match.Path.Type
	}
	return pathType == gatewayv1.PathMatchPathPrefix && match.Path.Value != nil && *match.Path.Value == "/"
}

func priorityForHTTPRouteMatch(priorities map[httpRouteMatchPriorityKey]int64, ruleIndex, matchIndex int) int64 {
	return priorities[httpRouteMatchPriorityKey{
		ruleIndex:  ruleIndex,
		matchIndex: matchIndex,
	}]
}

func calculateHTTPRoutePriorityClass(match gatewayv1.HTTPRouteMatch) httpRoutePriorityClass {
	class := httpRoutePriorityClass{}
	if isDefaultPathHTTPRouteMatch(match) {
		class.pathType = gatewayv1.PathMatchPathPrefix
		class.pathLength = len("/")
	} else if match.Path != nil {
		class.pathType = gatewayv1.PathMatchPathPrefix
		if match.Path.Type != nil {
			class.pathType = *match.Path.Type
		}
		if match.Path.Value != nil {
			class.pathLength = len(*match.Path.Value)
		}
	}
	class.hasMethodMatch = match.Method != nil
	class.headerCount = countEffectiveHTTPHeaderMatches(match.Headers)
	return class
}

func compareHTTPRoutePriorityClass(a, b httpRoutePriorityClass) int {
	if c := comparePathTypePriority(a.pathType, b.pathType); c != 0 {
		return c
	}
	if c := a.pathLength - b.pathLength; c != 0 {
		return c
	}
	if c := compareBool(a.hasMethodMatch, b.hasMethodMatch); c != 0 {
		return c
	}
	if c := a.headerCount - b.headerCount; c != 0 {
		return c
	}
	return 0
}

func comparePathTypePriority(a, b gatewayv1.PathMatchType) int {
	return pathTypePriority(a) - pathTypePriority(b)
}

func pathTypePriority(t gatewayv1.PathMatchType) int {
	switch t {
	case gatewayv1.PathMatchRegularExpression:
		return 3
	case gatewayv1.PathMatchExact:
		return 2
	case gatewayv1.PathMatchPathPrefix:
		return 1
	default:
		return 0
	}
}

func compareBool(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return 1
	}
	return -1
}

func countEffectiveHTTPHeaderMatches(headers []gatewayv1.HTTPHeaderMatch) int {
	seenHeaders := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		name := strings.ToLower(string(header.Name))
		if _, ok := seenHeaders[name]; ok {
			continue
		}
		seenHeaders[name] = struct{}{}
	}
	return len(seenHeaders)
}

// needsCaptureGroup checks if the given HTTPRoute rule requires a capture group
// in the KongRoute paths based on the presence of URLRewrite or RequestRedirect
// filters with ReplacePrefixMatch.
func needsCaptureGroup(rule gwtypes.HTTPRouteRule) bool {
	for _, filter := range rule.Filters {
		switch {
		case filter.Type == gatewayv1.HTTPRouteFilterURLRewrite &&
			filter.URLRewrite != nil &&
			filter.URLRewrite.Path != nil &&
			filter.URLRewrite.Path.ReplacePrefixMatch != nil:
			return true
		case filter.Type == gatewayv1.HTTPRouteFilterRequestRedirect &&
			filter.RequestRedirect != nil &&
			filter.RequestRedirect.Path != nil &&
			filter.RequestRedirect.Path.ReplacePrefixMatch != nil:
			return true
		}
	}
	return false
}

// routesForTLSRouteRule generates Kong routes for the given rule in a TLSRoute and its parent route.
// It generates a L4 Kong route with the following fields configured by:
//
// - protocols: set to `tls_passthrough` if the route's parent Gateway listener uses TLS passthrough.
// - snis: Set to match the SNI of the request by the hostnames in the parent TLSRoute.
func routesForTLSRouteRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	tlsRoute *gwtypes.TLSRoute,
	rule gwtypes.TLSRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	namingParentRef *gwtypes.ParentReference,
	serviceName string,
	hostnames []string,
) ([]*configurationv1alpha1.KongRoute, error) {
	routeName := namegen.NewKongRouteNameForTLSRouteRule(tlsRoute, cp, namingParentRef, rule)
	logger = logger.WithValues("kongroute", routeName)

	var protocol sdkkonnectcomp.Protocols
	tlsPassthrough, err := isTLSRoutePassthrough(ctx, cl, tlsRoute, pRef)
	if err != nil {
		return nil, err
	}
	if tlsPassthrough {
		protocol = sdkkonnectcomp.ProtocolsTLSPassthrough
	} else {
		protocol = sdkkonnectcomp.ProtocolsTLS
	}

	tags := pkgmetadata.ExtractTags(tlsRoute)

	routeBuilder := builder.NewKongRoute().WithName(routeName).
		WithNamespace(metadata.NamespaceFromParentRef(tlsRoute, pRef)).
		WithLabels(tlsRoute, pRef).
		WithAnnotations(tlsRoute, pRef).
		WithSpecName(routeName).
		WithKongService(serviceName).
		WithProtocols(protocol).
		WithSNIs(hostnames).
		WithSpecTags(tags)

	kongRoute, err := routeBuilder.Build()
	if err != nil {
		logger.Error(err, "Failed to build KongRoute resource")
		return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err)
	}
	// Verify that the KongRoute is only owned by the TLSRoute.
	if _, updErr := translator.VerifyAndUpdate(ctx, logger, cl, &kongRoute, tlsRoute, true); updErr != nil {
		return nil, updErr
	}

	return []*configurationv1alpha1.KongRoute{kongRoute.DeepCopy()}, nil
}

// routesForTCPRouteRule generates an L4 Kong route for the given TCPRoute rule.
func routesForTCPRouteRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	tcpRoute *gwtypes.TCPRoute,
	rule gwtypes.TCPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	namingParentRef *gwtypes.ParentReference,
	serviceName string,
) ([]*configurationv1alpha1.KongRoute, error) {
	routeName := namegen.NewKongRouteNameForTCPRouteRule(tcpRoute, cp, namingParentRef, rule)
	logger = logger.WithValues("kongroute", routeName)

	// Kong requires at least one of sources, destinations or snis on a route whose
	// protocols include tcp. Derive the destination ports from the parent Gateway's
	// TCP listeners so Kong can match incoming connections by the port they arrive on.
	ports, err := tcpDestinationPorts(ctx, cl, tcpRoute, pRef)
	if err != nil {
		return nil, err
	}
	// A tcp route requires at least one destination. If the parent Gateway has no TCP
	// listener matching parentRef, we cannot build a valid route, so fail here rather
	// than emitting an inert, destination-less KongRoute that Kong would reject.
	if len(ports) == 0 {
		return nil, fmt.Errorf("no TCP listener found on Gateway %s for TCPRoute %s/%s",
			pRef.Name, tcpRoute.Namespace, tcpRoute.Name)
	}

	return RoutesForTCPRouteRuleWithPorts(ctx, logger, cl, tcpRoute, rule, pRef, cp, namingParentRef, serviceName, ports)
}

// RoutesForTCPRouteRuleWithPorts generates an L4 Kong route for the given TCPRoute rule
// using the already-arbitrated Gateway listener ports supplied by the caller.
func RoutesForTCPRouteRuleWithPorts(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	tcpRoute *gwtypes.TCPRoute,
	rule gwtypes.TCPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	namingParentRef *gwtypes.ParentReference,
	serviceName string,
	ports []int32,
) ([]*configurationv1alpha1.KongRoute, error) {
	if len(ports) == 0 {
		return nil, fmt.Errorf("no TCP listener ports selected for TCPRoute %s/%s", tcpRoute.Namespace, tcpRoute.Name)
	}

	tags := pkgmetadata.ExtractTags(tcpRoute)
	routeName := namegen.NewKongRouteNameForTCPRouteRule(tcpRoute, cp, namingParentRef, rule)
	logger = logger.WithValues("kongroute", routeName)

	routeBuilder := builder.NewKongRoute().WithName(routeName).
		WithNamespace(metadata.NamespaceFromParentRef(tcpRoute, pRef)).
		WithLabels(tcpRoute, pRef).
		WithAnnotations(tcpRoute, pRef).
		WithSpecName(routeName).
		WithKongService(serviceName).
		WithProtocols(sdkkonnectcomp.ProtocolsTCP).
		WithDestinations(ports).
		WithSpecTags(tags)

	kongRoute, err := routeBuilder.Build()
	if err != nil {
		logger.Error(err, "Failed to build KongRoute resource")
		return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err)
	}
	if _, updErr := translator.VerifyAndUpdate(ctx, logger, cl, &kongRoute, tcpRoute, true); updErr != nil {
		return nil, updErr
	}

	return []*configurationv1alpha1.KongRoute{kongRoute.DeepCopy()}, nil
}

// tcpDestinationPorts resolves the destination ports for a TCPRoute rule from the
// TCP listeners of the parent Gateway referenced by parentRef.
func tcpDestinationPorts(
	ctx context.Context, cl client.Client, tcpRoute *gwtypes.TCPRoute, parentRef *gwtypes.ParentReference,
) ([]int32, error) {
	ns := tcpRoute.Namespace
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		ns = string(*parentRef.Namespace)
	}

	gw := &gwtypes.Gateway{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(parentRef.Name)}, gw); err != nil {
		return nil, fmt.Errorf("failed to get parent Gateway %s/%s for TCPRoute %s/%s: %w",
			ns, parentRef.Name, tcpRoute.Namespace, tcpRoute.Name, err)
	}

	return gatewayutils.TCPPortsFromListeners(gw, parentRef.SectionName, parentRef.Port), nil
}

// protocolsFromGatewayListener derives Kong route protocols from the Gateway listener
// referenced by the HTTPRoute's parentRef. It inspects the listener protocol and maps:
//   - HTTP  → "http"
//   - HTTPS → "https"
//
// Returns nil when no matching listeners are found (relies on Kong Gateway defaults).
func protocolsFromGatewayListener(
	ctx context.Context, cl client.Client, httpRoute *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference,
) ([]sdkkonnectcomp.Protocols, error) {
	ns := httpRoute.Namespace
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		ns = string(*parentRef.Namespace)
	}

	gw := &gwtypes.Gateway{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(parentRef.Name)}, gw); err != nil {
		return nil, fmt.Errorf("failed to get parent Gateway %s/%s for HTTPRoute %s/%s: %w",
			ns, parentRef.Name, httpRoute.Namespace, httpRoute.Name, err)
	}

	protos := gatewayutils.ProtocolsFromListeners(gw, parentRef.SectionName)
	if len(protos) == 0 {
		return nil, nil
	}

	protocols := make([]sdkkonnectcomp.Protocols, 0, len(protos))
	for _, p := range protos {
		protocols = append(protocols, sdkkonnectcomp.Protocols(p))
	}
	return protocols, nil
}

// isTLSRoutePassthrough checks if the TLSRoute's parent Gateway listener uses TLS passthrough mode
// to determine the protocols of the translated route from the TLSRoute.
// If the parent listener configures TLS mode to passthrough, it returns true to make the translated route use `tls_passthrough` protocol.
// Returns an error if it fails to get the parent Gateway listener.
func isTLSRoutePassthrough(
	ctx context.Context, cl client.Client, tlsRoute *gwtypes.TLSRoute, parentRef *gwtypes.ParentReference,
) (bool, error) {
	ns := tlsRoute.Namespace
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		ns = string(*parentRef.Namespace)
	}

	gw := &gwtypes.Gateway{}
	err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(parentRef.Name)}, gw)
	if err != nil {
		return false, fmt.Errorf("failed to get parent Gateway %s/%s for TLSRoute %s/%s", ns, parentRef.Name, tlsRoute.Namespace, tlsRoute.Name)
	}
	// If any of the gateway's listeners is configured to passthrough
	// TLS requests, we return true.
	for _, listener := range gw.Spec.Listeners {
		if parentRef.SectionName == nil || listener.Name == *parentRef.SectionName {
			if listener.TLS != nil && listener.TLS.Mode != nil &&
				*listener.TLS.Mode == gatewayv1.TLSModePassthrough {
				return true, nil
			}
		}
	}
	return false, nil
}
