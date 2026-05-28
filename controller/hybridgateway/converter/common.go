package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func getHybridGatewayParents[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](
	ctx context.Context, logger logr.Logger,
	cl client.Client, route TPtr,
) ([]hybridGatewayParent, error) {
	parentRefs := gwtypes.GetSpecParentRefs(*route)
	log.Debug(logger, "Getting hybrid gateway parents", "parentRefCount", len(parentRefs))

	result := []hybridGatewayParent{}
	for i, pRef := range parentRefs {
		log.Debug(logger, "Processing parent reference", "index", i, "parentRef", pRef)

		cp, err := refs.GetControlPlaneRefByParentRef(ctx, logger, cl, route, pRef)
		if err != nil {
			switch {
			case errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup):
				// These are expected errors to be handled gracefully. Log and skip this ParentRef, continue with others.
				log.Debug(logger, "Skipping ParentRef due to expected error", "parentRef", pRef, "error", err)
				continue
			default:
				// Unexpected system error, fail the entire translation.
				return nil, fmt.Errorf("failed to get ControlPlaneRef for ParentRef %s: %w", pRef.Name, err)
			}
		}

		if cp == nil {
			log.Debug(logger, "No ControlPlaneRef found for ParentRef, skipping", "parentRef", pRef)
			continue
		}

		log.Debug(logger, "Found ControlPlaneRef for ParentRef", "parentRef", pRef, "controlPlane", cp.KonnectNamespacedRef)

		hostnames, err := getHostnamesByParentRef(ctx, logger, cl, route, pRef)
		if err != nil {
			log.Error(logger, err, "Failed to get hostnames for ParentRef", "parentRef", pRef)
			return nil, err
		}
		if hostnames == nil {
			log.Debug(logger, "No hostnames found for ParentRef, skipping", "parentRef", pRef)
			continue
		}

		log.Debug(logger, "Adding parent reference to result", "parentRef", pRef, "hostnames", hostnames)
		result = append(result, hybridGatewayParent{
			parentRef: pRef,
			cpRef:     cp,
			hostnames: hostnames,
		})
	}

	log.Debug(logger, "Finished processing parent references", "supportedParents", len(result))
	return result, nil
}

// getHostnamesByParentRef returns the hostnames that match between the route and the Gateway listeners.
func getHostnamesByParentRef[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](
	ctx context.Context, logger logr.Logger, cl client.Client, route TPtr, pRef gwtypes.ParentReference,
) ([]string, error) {
	logger = logger.WithValues("parentRef", pRef.Name)
	log.Debug(logger, "Getting hostnames for ParentRef")

	var err error
	var hostnames []string
	routeHostnames := routeHostNamesString(*route)

	listeners, err := refs.GetListenersByParentRef(ctx, cl, route, pRef)
	if err != nil {
		log.Error(logger, err, "Failed to get listeners for ParentRef")
		return nil, err
	}

	log.Debug(logger, "Found listeners for ParentRef", "listenerCount", len(listeners))

	for _, listener := range listeners {
		// Check section reference if present
		if pRef.SectionName != nil {
			sectionName := string(*pRef.SectionName)
			if string(listener.Name) != sectionName {
				// This listener doesn't match the section reference, skip it
				continue
			}
		}
		if pRef.Port != nil {
			if listener.Port != lo.FromPtr(pRef.Port) {
				// This listener doesn't match the port reference, skip it
				continue
			}
		}

		// If the listener has no hostname, it means it accepts all HTTPRoute hostnames.
		// No need to do further checks.
		if listener.Hostname == nil || *listener.Hostname == "" {
			log.Debug(logger, "Listener accepts all hostnames", "listener", listener.Name)
			return routeHostnames, nil
		}

		// If the route does not specify hostnames, it matches all listener hostnames.
		if len(routeHostnames) == 0 {
			hostnames = append(hostnames, string(*listener.Hostname))
			continue
		}

		// Handle wildcard hostnames - get intersection
		log.Debug(logger, "Processing listener with hostname", "listener", listener.Name, "listenerHostname", *listener.Hostname)
		for _, host := range routeHostnames {
			if intersection, ok := utils.HostnameIntersection(string(*listener.Hostname), host); ok {
				log.Trace(logger, "Found hostname intersection", "listenerHostname", *listener.Hostname, "routeHostname", host, "intersection", intersection)
				hostnames = append(hostnames, intersection)
			}
		}
	}

	hostnames = lo.Uniq(hostnames)
	if len(hostnames) == 0 {
		// Returning nil tells the caller to skip this parent entirely. An empty slice
		// would flow into WithHosts() and create a host-less KongRoute that matches any host.
		log.Debug(logger, "No hostname intersection found for ParentRef")
		return nil, nil
	}

	log.Debug(logger, "Finished processing hostnames", "finalHostnames", hostnames)
	return hostnames, nil
}

func routeHostNamesString[T gwtypes.SupportedRoute](route T) []string {
	return lo.Map(gwtypes.GetSpecHostnames(route), func(h gwtypes.Hostname, _ int) string {
		return string(h)
	})
}
