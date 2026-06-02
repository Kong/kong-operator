package kongroute

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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
		return RoutesForHTTPRouteRule(ctx, logger, cl, r, httpRule, pRef, cp, namingParentRef, serviceName, hostnames)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongRoute: unmatched route type and rule type: %T and %T", route, rule)
		}
		return routesForTLSRouteRule(ctx, logger, cl, r, tlsRule, pRef, cp, namingParentRef, serviceName, hostnames)
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

	stripPath, err := metadata.ExtractStripPath(httpRoute.Annotations)
	if err != nil {
		log.Error(logger, err, fmt.Sprintf("Failed to extract strip path annotation, defaulting to %t", stripPath),
			"httpRoute", fmt.Sprintf("%s/%s", httpRoute.GetNamespace(), httpRoute.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in future versions, please fix the annotation value to be a valid boolean")
	}
	preserveHost, err := metadata.ExtractPreserveHost(httpRoute.Annotations)
	if err != nil {
		log.Error(logger, err, fmt.Sprintf("Failed to extract preserve host annotation, defaulting to %t", preserveHost),
			"httpRoute", fmt.Sprintf("%s/%s", httpRoute.GetNamespace(), httpRoute.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in future versions, please fix the annotation value to be a valid boolean")
	}

	for i, match := range rule.Matches {
		routeName := namegen.NewKongRouteNameForMatch(httpRoute, cp, namingParentRef, match, i)
		mLog := logger.WithValues("kongroute", routeName, "matchIndex", i)
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
			WithKongService(serviceName).
			WithHTTPRouteMatch(match, setCaptureGroup)

		newRoute, buildErr := routeBuilder.Build()
		if buildErr != nil {
			log.Error(mLog, buildErr, "Failed to build KongRoute resource")
			return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, buildErr)
		}

		if _, updErr := translator.VerifyAndUpdate(ctx, mLog, cl, &newRoute, httpRoute, true); updErr != nil {
			return nil, updErr
		}

		// Add to result slice as an explicit copy for clarity.
		// Using DeepCopy expresses the intent that each match yields an
		// independent KongRoute object.
		kongRoutes = append(kongRoutes, newRoute.DeepCopy())
	}

	return kongRoutes, nil
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

	var protocol sdkkonnectcomp.RouteJSONProtocols
	tlsPassthrough, err := isTLSRoutePassthrough(ctx, cl, tlsRoute, pRef)
	if err != nil {
		return nil, err
	}
	if tlsPassthrough {
		protocol = sdkkonnectcomp.RouteJSONProtocolsTLSPassthrough
	} else {
		protocol = sdkkonnectcomp.RouteJSONProtocolsTLS
	}

	routeBuilder := builder.NewKongRoute().WithName(routeName).
		WithNamespace(metadata.NamespaceFromParentRef(tlsRoute, pRef)).
		WithLabels(tlsRoute, pRef).
		WithAnnotations(tlsRoute, pRef).
		WithSpecName(routeName).
		WithKongService(serviceName).
		WithProtocols(protocol).
		WithSNIs(hostnames)

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

// protocolsFromGatewayListener derives Kong route protocols from the Gateway listener
// referenced by the HTTPRoute's parentRef. It inspects the listener protocol and maps:
//   - HTTP  → "http"
//   - HTTPS → "https"
//
// Returns nil when no matching listeners are found (relies on Kong Gateway defaults).
func protocolsFromGatewayListener(
	ctx context.Context, cl client.Client, httpRoute *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference,
) ([]sdkkonnectcomp.RouteJSONProtocols, error) {
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

	protocols := make([]sdkkonnectcomp.RouteJSONProtocols, 0, len(protos))
	for _, p := range protos {
		protocols = append(protocols, sdkkonnectcomp.RouteJSONProtocols(p))
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
