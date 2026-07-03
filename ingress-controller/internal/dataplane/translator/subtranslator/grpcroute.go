package subtranslator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

func getGRPCMatchDefaults() (
	map[gatewayapi.GRPCMethodMatchType]string,
	map[gatewayapi.GRPCMethodMatchType]string,
) {
	// Kong routes derived from a GRPCRoute use a path composed of the match's gRPC service and method
	// If either the service or method is omitted, there is a default regex determined by the match type
	// https://gateway-api.sigs.k8s.io/geps/gep-1016/#matcher-types describes the defaults

	// default path components for the GRPC service
	return map[gatewayapi.GRPCMethodMatchType]string{
			gatewayapi.GRPCMethodMatchType(""):          ".+",
			gatewayapi.GRPCMethodMatchExact:             ".+",
			gatewayapi.GRPCMethodMatchRegularExpression: ".+",
		},
		// default path components for the GRPC method
		map[gatewayapi.GRPCMethodMatchType]string{
			gatewayapi.GRPCMethodMatchType(""):          "",
			gatewayapi.GRPCMethodMatchExact:             "",
			gatewayapi.GRPCMethodMatchRegularExpression: ".+",
		}
}

func generateKongPathFromGRPCMethodMatch(methodMatch *gatewayapi.GRPCMethodMatch) *string {
	serviceMap, methodMap := getGRPCMatchDefaults()
	var method, service string
	matchMethod := methodMatch.Method
	matchService := methodMatch.Service
	matchType := gatewayapi.GRPCMethodMatchExact
	if methodMatch.Type != nil {
		matchType = *methodMatch.Type
	}
	if matchMethod == nil {
		method = methodMap[matchType]
	} else {
		method = *matchMethod
	}
	if matchService == nil {
		service = serviceMap[matchType]
	} else {
		service = *matchService
	}

	// Kong routes gRPC using a regex path (KongPathRegexPrefix), so for an Exact
	// match the service and method must be treated as literals, not patterns.
	// A fully-qualified gRPC service name is dot-separated
	// (e.g. "gateway_api_conformance.echo_basic.grpcecho.GrpcEcho") and the dot
	// "." is a regex metacharacter that would match any char. Other regex
	// metacharacters ( . + * ? ( ) | [ ] { } ^ $ \ ) may also appear, so we run
	// both parts through regexp.QuoteMeta to escape them. We also anchor the
	// method with a trailing "$" so e.g. "/svc/Echo" does not also match
	// "/svc/EchoTwo".
	if matchType == gatewayapi.GRPCMethodMatchExact {
		if matchService != nil {
			service = regexp.QuoteMeta(service)
		}
		if matchMethod != nil {
			method = regexp.QuoteMeta(method) + "$"
		}
	}

	return new(KongPathRegexPrefix + fmt.Sprintf("/%s/%s", service, method))
}

func GenerateKongRoutesFromGRPCRouteRule(
	grpcroute *gatewayapi.GRPCRoute,
	ruleNumber int,
	storer store.Storer,
) []kongstate.Route {
	if ruleNumber >= len(grpcroute.Spec.Rules) {
		return nil
	}

	routeName := func(namespace string, name string, ruleNumber int, matchNumber int) *string {
		return new(fmt.Sprintf(
			"grpcroute.%s.%s.%d.%d",
			namespace,
			name,
			ruleNumber,
			matchNumber,
		))
	}

	// Gather the K8s object information and hostnames from the GRPCRoute.
	ingressObjectInfo := util.FromK8sObject(grpcroute)
	tags := generateTagsForGRPCRoute(grpcroute)
	grpcProtocols := kong.StringSlice("grpc", "grpcs")
	rule := grpcroute.Spec.Rules[ruleNumber]
	// Kong Route expects to have for gRPC, at least one of Hosts, Headers or Paths fields set.
	// For no matches it can be a catch-all or route based on hostnames.
	if len(rule.Matches) == 0 {
		r := kongstate.Route{
			Ingress: ingressObjectInfo,
			Route: kong.Route{
				Name:      routeName(grpcroute.Namespace, grpcroute.Name, ruleNumber, 0),
				Protocols: grpcProtocols,
				Tags:      tags,
			},
		}
		if configuredHostnames := getGRPCRouteHostnamesAsSliceOfStringPointers(grpcroute, storer); len(configuredHostnames) > 0 {
			// Match based on hostnames.
			r.Hosts = configuredHostnames
		} else {
			// No hostnames configured, so this is a catch-all.
			// https://docs.konghq.com/gateway/latest/production/configuring-a-grpc-service/#single-grpc-service-and-route
			r.Paths = kong.StringSlice("/")
		}
		return []kongstate.Route{r}
	}

	// Rule matches are configured, hostname may be specified too.
	routes := make([]kongstate.Route, 0, len(rule.Matches))
	for matchNumber, match := range rule.Matches {
		r := kongstate.Route{
			Ingress: ingressObjectInfo,
			Route: kong.Route{
				Name:      routeName(grpcroute.Namespace, grpcroute.Name, ruleNumber, matchNumber),
				Protocols: grpcProtocols,
				Tags:      tags,
				Hosts:     getGRPCRouteHostnamesAsSliceOfStringPointers(grpcroute, storer),
			},
		}
		if match.Method != nil {
			r.Paths = append(r.Paths, generateKongPathFromGRPCMethodMatch(match.Method))
		}

		r.Headers = map[string][]string{}
		for _, hmatch := range match.Headers {
			name := string(hmatch.Name)
			r.Headers[name] = append(r.Headers[name], hmatch.Value)
		}

		routes = append(routes, r)
	}
	return routes
}

// -----------------------------------------------------------------------------
// Translate GRPCRoute - Utils
// -----------------------------------------------------------------------------

// getGRPCRouteHostnamesAsSliceOfStringPointers translates the hostnames defined
// in an GRPCRoute specification into a []*string slice, which is the type required
// by kong.Route{}.
// The hostname field is optional. If not specified, the configured value will be obtained from parentRefs.
func getGRPCRouteHostnamesAsSliceOfStringPointers(grpcroute *gatewayapi.GRPCRoute, storer store.Storer) []*string {
	hostnames := getEffectiveHostnamesForGRPCRoute(grpcroute, storer)
	if len(hostnames) == 0 {
		return nil
	}
	return lo.Map(hostnames, func(h gatewayapi.Hostname, _ int) *string {
		return new(string(h))
	})
}

// getEffectiveHostnamesForGRPCRoute returns the hostnames that should be used to
// match traffic for a GRPCRoute. The hostname field is optional in the GRPCRoute
// spec; if specified, those hostnames are used directly. If not specified, the
// hostnames are inherited from the listeners of the Gateways referenced in the
// GRPCRoute's parentRefs (honoring sectionName when set).
func getEffectiveHostnamesForGRPCRoute(grpcroute *gatewayapi.GRPCRoute, storer store.Storer) []gatewayapi.Hostname {
	if len(grpcroute.Spec.Hostnames) > 0 {
		return grpcroute.Spec.Hostnames
	}

	// If no hostnames are specified, we will use the hostname from the Gateway
	// that the GRPCRoute is attached to.
	namespace := grpcroute.GetNamespace()

	if grpcroute.Spec.ParentRefs == nil {
		return nil
	}

	hostnames := make([]gatewayapi.Hostname, 0)
	for _, parentRef := range grpcroute.Spec.ParentRefs {
		// we only care about Gateways
		if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
			continue
		}

		if parentRef.Namespace != nil {
			namespace = string(*parentRef.Namespace)
		}

		name := string(parentRef.Name)

		gateway, err := storer.GetGateway(namespace, name)
		// As parentRef has already been validated before, the error here will not actually occur.
		// This is where defensive programming takes place.
		if err != nil {
			// TODO: Add logging.
			// https://github.com/Kong/kubernetes-ingress-controller/pull/6166#discussion_r1631250776
			return nil
		}

		if parentRef.SectionName != nil {
			sectionName := string(*parentRef.SectionName)

			for _, listener := range gateway.Spec.Listeners {
				if string(listener.Name) == sectionName {
					if listener.Hostname != nil {
						hostnames = append(hostnames, *listener.Hostname)
					}
				}
			}
		} else {
			for _, listener := range gateway.Spec.Listeners {
				if listener.Hostname != nil {
					hostnames = append(hostnames, *listener.Hostname)
				}
			}
		}
	}

	return hostnames
}

func generateTagsForGRPCRoute(grpcroute *gatewayapi.GRPCRoute) []*string {
	ruleNames := lo.FilterMap(grpcroute.Spec.Rules, func(r gatewayapi.GRPCRouteRule, _ int) (string, bool) {
		name := strings.TrimSpace(string(lo.FromPtrOr(r.Name, "")))
		return name, len(name) > 0
	})
	return util.GenerateTagsForObject(grpcroute, util.AdditionalTagsK8sNamedRouteRule(ruleNames...)...)
}
