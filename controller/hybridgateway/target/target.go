package target

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// ValidBackendRef represents a BackendRef that has passed all validation checks.
type ValidBackendRef struct {
	BackendRef  *gwtypes.HTTPBackendRef
	Service     *corev1.Service
	ServicePort *corev1.ServicePort
	// ReadyEndpoints contains merged endpoint addresses from all EndpointSlices for this service.
	ReadyEndpoints []string
	// TargetPort is the actual port to use in Kong targets (already resolved based on service type).
	TargetPort int
	// Weight is the calculated weight per endpoint for this backend (after weight recalculation).
	Weight int32
}

// TargetsForBackendRefs creates KongTargets for all BackendRefs in a rule.
// This function processes all BackendRefs together, enabling better weight distribution and optimization.
func TargetsForBackendRefs(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	backendRefs []gwtypes.HTTPBackendRef,
	pRef *gwtypes.ParentReference,
	upstreamName string,
	referenceGrantEnabled bool,
	fqdn bool,
	clusterDomain string,
) ([]configurationv1alpha1.KongTarget, error) {
	// Step 1: Filter and validate all BackendRefs, extracting endpoints.
	validBackendRefs, err := filterValidBackendRefs(ctx, logger, cl, httpRoute, backendRefs, referenceGrantEnabled, fqdn, clusterDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to filter valid BackendRefs: %w", err)
	}

	if len(validBackendRefs) == 0 {
		logger.V(1).Info("no valid BackendRefs found for rule")
		return []configurationv1alpha1.KongTarget{}, nil
	}

	// Step 2: Recalculate weights across all valid BackendRefs.
	validBackendRefs = recalculateWeightsAcrossBackendRefs(validBackendRefs)

	// Step 3: Create KongTargets from the processed ValidBackendRef structs.
	targets, err := createTargetsFromValidBackendRefs(httpRoute, pRef, upstreamName, validBackendRefs)
	if err != nil {
		return nil, fmt.Errorf("failed to create targets from valid BackendRefs: %w", err)
	}

	logger.V(1).Info("created targets for BackendRefs",
		"totalBackendRefs", len(backendRefs),
		"validBackendRefs", len(validBackendRefs),
		"createdTargets", len(targets))

	return targets, nil
}

// findBackendRefPortInService returns the ServicePort from svc that matches the port specified in bRef.
// If bRef.Port is nil or no matching port is found in svc.Spec.Ports, an error is returned.
// This function is used to validate and resolve the actual ServicePort for a given BackendRef.
func findBackendRefPortInService(bRef *gwtypes.HTTPBackendRef, svc *corev1.Service) (*corev1.ServicePort, error) {
	// Check if the port is specified in the BackendRef. The port is required.
	if bRef.Port == nil {
		// If the port is not specified, return an error.
		return nil, fmt.Errorf("port not specified in BackendRef")
	}

	// Find the port in the service that matches the port in the BackendRef.
	svcPort, svcPortFound := lo.Find(svc.Spec.Ports, func(p corev1.ServicePort) bool {
		return p.Port == int32(*bRef.Port)
	})
	if !svcPortFound {
		// If the port is not found, return an error.
		return nil, fmt.Errorf("port %v not found in service %s/%s", *bRef.Port, svc.Namespace, svc.Name)
	}

	return &svcPort, nil
}

// getEndpointSlicesForService retrieves all EndpointSlices for a given service.
func getEndpointSlicesForService(ctx context.Context, cl client.Client, svc *corev1.Service) (*discoveryv1.EndpointSliceList, error) {
	endpointSlices := &discoveryv1.EndpointSliceList{}
	req, err := labels.NewRequirement(discoveryv1.LabelServiceName, selection.Equals, []string{svc.Name})
	if err != nil {
		return nil, err
	}
	labelSelector := labels.NewSelector().Add(*req)
	err = cl.List(ctx, endpointSlices, &client.ListOptions{Namespace: svc.Namespace, LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("failed to list endpointslices for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	return endpointSlices, nil
}

// extractReadyEndpointAddresses extracts all ready endpoint addresses that match the service port.
func extractReadyEndpointAddresses(endpointSlices *discoveryv1.EndpointSliceList, svcPort *corev1.ServicePort) []string {
	var addresses []string

	for _, endpointSlice := range endpointSlices.Items {
		for _, p := range endpointSlice.Ports {
			// Skip ports that don't match the service port.
			if p.Port == nil || *p.Port < 0 || *p.Protocol != svcPort.Protocol || *p.Name != svcPort.Name {
				continue
			}

			for _, endpoint := range endpointSlice.Endpoints {
				// Only include ready endpoints.
				if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
					addresses = append(addresses, endpoint.Addresses...)
				}
			}
		}
	}

	return addresses
}

// resolveServiceEndpoints determines the appropriate endpoints for a service based on its type and configuration.
// Returns (endpoints, shouldSkip, error) where shouldSkip indicates the service should be skipped.
func resolveServiceEndpoints(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	svc *corev1.Service,
	svcPort *corev1.ServicePort,
	fqdn bool,
	clusterDomain string,
) ([]string, bool, error) {
	switch {
	case fqdn && svc.Spec.ClusterIP != "None":
		// For FQDN mode with regular services (non-headless), use the service FQDN as the single "endpoint".
		return resolveFQDNEndpoints(svc, clusterDomain), false, nil

	case svc.Spec.Type == corev1.ServiceTypeExternalName:
		// For ExternalName services, use the external name as the endpoint.
		return resolveExternalNameEndpoints(logger, svc)

	default:
		// For all other cases (headless services, regular services without FQDN mode).
		return resolveEndpointSliceEndpoints(ctx, logger, cl, svc, svcPort)
	}
}

// resolveFQDNEndpoints creates FQDN-based endpoints for regular services.
func resolveFQDNEndpoints(svc *corev1.Service, clusterDomain string) []string {
	var serviceFQDN string
	if clusterDomain == "" {
		// Use the shorter DNS form which works across different cluster domain configurations.
		serviceFQDN = fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)
	} else {
		// Use the full FQDN with the configured cluster domain.
		serviceFQDN = fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, clusterDomain)
	}
	return []string{serviceFQDN}
}

// resolveExternalNameEndpoints handles ExternalName service endpoints.
func resolveExternalNameEndpoints(logger logr.Logger, svc *corev1.Service) ([]string, bool, error) {
	if svc.Spec.ExternalName != "" {
		return []string{svc.Spec.ExternalName}, false, nil
	}
	logger.V(1).Info("skipping ExternalName service with empty externalName", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
	return nil, true, nil
}

// resolveEndpointSliceEndpoints fetches and processes EndpointSlices for a service.
func resolveEndpointSliceEndpoints(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	svc *corev1.Service,
	svcPort *corev1.ServicePort,
) ([]string, bool, error) {
	endpointSlices, err := getEndpointSlicesForService(ctx, cl, svc)
	if err != nil {
		// If it's a not found error, log and skip (EndpointSlices might not be created yet).
		// For other errors (network, permissions, etc.), return the error.
		if client.IgnoreNotFound(err) != nil {
			return nil, false, fmt.Errorf("error fetching EndpointSlices for service %s/%s: %w", svc.Namespace, svc.Name, err)
		}
		logger.V(1).Info("skipping service with no EndpointSlices found", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
		return nil, true, nil
	}

	// Extract ready endpoint addresses.
	readyEndpoints := extractReadyEndpointAddresses(endpointSlices, svcPort)

	// Skip services with no ready endpoints.
	if len(readyEndpoints) == 0 {
		logger.V(1).Info("skipping service with no ready endpoints", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
		return nil, true, nil
	}

	return readyEndpoints, false, nil
}

// resolveTargetPort determines the appropriate target port based on service type and mode.
func resolveTargetPort(svc *corev1.Service, svcPort *corev1.ServicePort, fqdn bool) int {
	switch {
	case fqdn && svc.Spec.ClusterIP != "None":
		// For FQDN mode with regular services, use service port (Kong will resolve via DNS).
		return int(svcPort.Port)

	case svc.Spec.Type == corev1.ServiceTypeExternalName:
		// For ExternalName services, use service port (external service expectation).
		return int(svcPort.Port)

	default:
		// For all other cases (headless services, regular services with endpoint discovery).
		// Use target port since we're connecting directly to pod endpoints.
		if svcPort.TargetPort.IntVal > 0 {
			return int(svcPort.TargetPort.IntVal)
		}
		// Default to service port if target port is not specified.
		return int(svcPort.Port)
	}
}

// filterValidBackendRefs filters a list of BackendRefs and returns only the valid ones.
// It performs the following checks for each BackendRef:
// 1. Check if the BackendRef is supported (currently only Service).
// 2. Check if the referenced Service exists.
// 3. Check if the specified port exists in the Service.
// 4. Check if ReferenceGrant permits cross-namespace access (if needed).
// 5. Fetch EndpointSlices and extract ready endpoints (for headless services, regular services, or when FQDN is disabled).
func filterValidBackendRefs(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	backendRefs []gwtypes.HTTPBackendRef,
	referenceGrantEnabled bool,
	fqdn bool,
	clusterDomain string,
) ([]ValidBackendRef, error) {
	var validBackendRefs []ValidBackendRef

	for _, bRef := range backendRefs {
		// Check if the backendRef is supported.
		if !route.IsBackendRefSupported(bRef.Group, bRef.Kind) {
			logger.Info("skipping unsupported backendRef", "group", bRef.Group, "kind", bRef.Kind)
			continue
		}

		// Determine the namespace for the referenced Service.
		bRefNamespace := httpRoute.Namespace
		if bRef.Namespace != nil && *bRef.Namespace != "" {
			bRefNamespace = string(*bRef.Namespace)
		}

		// Check if the referenced Service exists.
		svc := &corev1.Service{}
		err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(bRef.Name)}, svc)
		if err != nil {
			logger.Info("skipping nonexistent Service", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
			continue
		}

		// Find and validate the port in the Service.
		svcPort, err := findBackendRefPortInService(&bRef, svc)
		if err != nil {
			logger.Info("skipping backendRef with invalid port", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name, "error", err)
			continue
		}

		// Check ReferenceGrant permission for cross-namespace access.
		if referenceGrantEnabled && bRefNamespace != httpRoute.Namespace {
			permitted, found, err := route.CheckReferenceGrant(ctx, cl, &bRef, httpRoute.Namespace)
			if err != nil {
				return nil, fmt.Errorf("error checking ReferenceGrant for BackendRef %s: %w", bRef.Name, err)
			}
			if !permitted {
				if found {
					logger.Info("skipping backendRef not permitted by ReferenceGrant", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
				} else {
					logger.Info("skipping backendRef in different namespace without ReferenceGrant", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
				}
				continue
			}
		}

		// Resolve endpoints based on service type and mode.
		readyEndpoints, shouldSkip, err := resolveServiceEndpoints(ctx, logger, cl, svc, svcPort, fqdn, clusterDomain)
		if err != nil {
			return nil, err
		}
		if shouldSkip {
			continue
		}

		// Determine the target port based on service type and mode.
		targetPort := resolveTargetPort(svc, svcPort, fqdn)

		// If we reach here, the BackendRef is valid and has endpoints.
		validBackendRefs = append(validBackendRefs, ValidBackendRef{
			BackendRef:     &bRef,
			Service:        svc,
			ServicePort:    svcPort,
			ReadyEndpoints: readyEndpoints,
			TargetPort:     targetPort,
			// Will be calculated in recalculateWeightsAcrossBackendRefs.
			Weight: 0,
		})
	}

	return validBackendRefs, nil
}

// recalculateWeightsAcrossBackendRefs recalculates weights across all valid BackendRefs in a rule.
// This uses the weight calculation algorithm from weight.go to ensure mathematically
// correct proportional distribution based on the original BackendRef weights and endpoint counts.
func recalculateWeightsAcrossBackendRefs(validBackendRefs []ValidBackendRef) []ValidBackendRef {
	if len(validBackendRefs) == 0 {
		return validBackendRefs
	}

	// Convert ValidBackendRef to BackendRef for weight calculation.
	backends := make([]BackendRef, len(validBackendRefs))
	for i, vbRef := range validBackendRefs {
		// Generate a unique name for this backend (using service name and namespace).
		backendName := fmt.Sprintf("%s/%s", vbRef.Service.Namespace, vbRef.Service.Name)

		// Get the original weight (default to 1 if not specified).
		weight := uint32(1)
		if vbRef.BackendRef.Weight != nil {
			weight = uint32(*vbRef.BackendRef.Weight)
		}

		// Number of ready endpoints (could be 1 for FQDN/ExternalName).
		endpoints := uint32(len(vbRef.ReadyEndpoints))

		backends[i] = BackendRef{
			Name:      backendName,
			Weight:    weight,
			Endpoints: endpoints,
		}
	}

	// Calculate the endpoint weights.
	endpointWeights := CalculateEndpointWeights(backends)

	// Update the weights in our ValidBackendRef structs directly.
	for i, vbRef := range validBackendRefs {
		backendName := fmt.Sprintf("%s/%s", vbRef.Service.Namespace, vbRef.Service.Name)
		endpointWeight := endpointWeights[backendName]
		validBackendRefs[i].Weight = int32(endpointWeight)
	}

	return validBackendRefs
}

// createTargetsFromValidBackendRefs creates KongTargets from ValidBackendRef structs.
// This function handles all service types (ClusterIP, ExternalName, FQDN) using a unified approach.
func createTargetsFromValidBackendRefs(httpRoute *gwtypes.HTTPRoute, pRef *gwtypes.ParentReference, upstreamName string,
	validBackendRefs []ValidBackendRef) ([]configurationv1alpha1.KongTarget, error) {
	var targets []configurationv1alpha1.KongTarget

	for _, vbRef := range validBackendRefs {
		// Skip backends with no endpoints (they have weight 0 anyway).
		// This should not happen, but if it happens then we skip them.
		if len(vbRef.ReadyEndpoints) == 0 {
			continue
		}

		// After recalculateWeightsAcrossBackendRefs, the ValidBackendRef.Weight contains
		// the calculated weight per endpoint, so we use it directly for all endpoints.
		weight := vbRef.Weight

		for _, endpoint := range vbRef.ReadyEndpoints {
			// Use the pre-calculated target port (already resolved based on service type and mode).
			port := vbRef.TargetPort

			// Create target with explicit endpoint address (works for all cases: real endpoints, FQDN, external names).
			target, err := builder.NewKongTarget().
				WithName(fmt.Sprintf("%s.%s", upstreamName, utils.Hash32(utils.Hash32(vbRef.BackendRef)+endpoint))).
				WithNamespace(httpRoute.Namespace).
				WithLabels(httpRoute, pRef).
				WithAnnotations(httpRoute, pRef).
				WithUpstreamRef(upstreamName).
				WithTarget(endpoint, port).
				WithOwner(httpRoute).
				WithWeight(&weight).
				Build()
			if err != nil {
				return nil, err
			}
			targets = append(targets, target)
		}
	}

	return targets, nil
}
