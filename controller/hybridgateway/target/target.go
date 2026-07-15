package target

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
)

// validBackendRef represents a BackendRef that has passed all validation checks.
type validBackendRef[T gwtypes.SupportedBackendRef] struct {
	backendRef  *T
	service     *corev1.Service
	servicePort *corev1.ServicePort
	// readyEndpoints contains merged endpoint addresses from all EndpointSlices for this service.
	readyEndpoints []string
	// targetPort is the actual port to use in Kong targets (already resolved based on service type).
	targetPort int
	// weight is the calculated weight per endpoint for this backend (after weight recalculation).
	weight int32
}

// TargetsForBackendRefs creates KongTargets for all BackendRefs in a rule.
// This function processes all BackendRefs together, enabling better weight distribution and optimization.
func TargetsForBackendRefs[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedBackendRef,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	backendRefs []R,
	pRef *gwtypes.ParentReference,
	upstreamName string,
	fqdn bool,
	clusterDomain string,
) ([]configurationv1alpha1.KongTarget, error) {

	// Step 0: Check if type of parentRoute matches the type of BackendRefs
	switch any(parentRoute).(type) {
	case *gwtypes.HTTPRoute:
		if _, ok := any(backendRefs).([]gwtypes.HTTPBackendRef); !ok {
			return nil, fmt.Errorf("failed to build KongTarget: unmatched route and backendRefs type: %T and  %T", parentRoute, backendRefs)
		}
	case *gwtypes.TLSRoute:
		if _, ok := any(backendRefs).([]gwtypes.BackendRef); !ok {
			return nil, fmt.Errorf("failed to build KongTarget: unmatched route and backendRefs type: %T and  %T", parentRoute, backendRefs)
		}
		// TODO: add other types of routes when we support them.

		// Should be unreachable.
	default:
		return nil, fmt.Errorf("failed to build KongTarget: unsupported route type %T", parentRoute)
	}

	// Step 1: Filter and validate all BackendRefs, extracting endpoints.
	validBackendRefs, err := filterValidBackendRefs(ctx, logger, cl, parentRoute, backendRefs, fqdn, clusterDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to filter valid BackendRefs: %w", err)
	}

	if len(validBackendRefs) == 0 {
		log.Debug(logger, "no valid BackendRefs found for rule")
		return []configurationv1alpha1.KongTarget{}, nil
	}

	// Step 2: Recalculate weights across all valid BackendRefs.
	validBackendRefs = recalculateWeightsAcrossBackendRefs(validBackendRefs)

	// Step 3: Create KongTargets from the processed ValidBackendRef structs.
	targets, err := createTargetsFromValidBackendRefs(ctx, logger, cl, parentRoute, pRef, upstreamName, validBackendRefs)
	if err != nil {
		return nil, fmt.Errorf("failed to create targets from valid BackendRefs: %w", err)
	}

	log.Debug(logger, "created targets for BackendRefs",
		"totalBackendRefs", len(backendRefs),
		"validBackendRefs", len(validBackendRefs),
		"createdTargets", len(targets))

	return targets, nil
}

// findBackendRefPortInService returns the ServicePort from svc that matches the port specified in bRef.
// If bRef.Port is nil or no matching port is found in svc.Spec.Ports, an error is returned.
// This function is used to validate and resolve the actual ServicePort for a given BackendRef.
func findBackendRefPortInService(bRef *gwtypes.BackendRef, svc *corev1.Service) (*corev1.ServicePort, error) {
	// Check if the port is specified in the BackendRef. The port is required.
	if bRef.Port == nil {
		// If the port is not specified, return an error.
		return nil, fmt.Errorf("port not specified in BackendRef")
	}

	// Find the port in the service that matches the port in the BackendRef.
	svcPort, svcPortFound := lo.Find(svc.Spec.Ports, func(p corev1.ServicePort) bool {
		return p.Port == *bRef.Port
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
	case shouldUseServiceFQDNTarget(svc, fqdn):
		// For service-upstream or FQDN mode, use the service FQDN as the single endpoint.
		return resolveFQDNEndpoints(svc, clusterDomain), false, nil

	case svc.Spec.Type == corev1.ServiceTypeExternalName:
		// For ExternalName services, use the external name as the endpoint.
		return resolveExternalNameEndpoints(logger, svc)

	default:
		// For all other cases (headless services, regular services without FQDN mode).
		return resolveEndpointSliceEndpoints(ctx, logger, cl, svc, svcPort)
	}
}

func shouldUseServiceFQDNTarget(svc *corev1.Service, fqdn bool) bool {
	if metadata.IsServiceUpstream(svc) {
		return true
	}

	return fqdn && svc.Spec.ClusterIP != "None"
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
	log.Debug(logger, "skipping ExternalName service with empty externalName", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
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
		log.Debug(logger, "skipping service with no EndpointSlices found", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
		return nil, true, nil
	}

	// Extract ready endpoint addresses.
	readyEndpoints := extractReadyEndpointAddresses(endpointSlices, svcPort)

	// Skip services with no ready endpoints.
	if len(readyEndpoints) == 0 {
		log.Debug(logger, "skipping service with no ready endpoints", "service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
		return nil, true, nil
	}

	return readyEndpoints, false, nil
}

// resolveTargetPort determines the appropriate target port based on service type and mode.
func resolveTargetPort(ctx context.Context, cl client.Client, svc *corev1.Service, svcPort *corev1.ServicePort, fqdn bool) (int, error) {
	switch {
	case shouldUseServiceFQDNTarget(svc, fqdn):
		// For service-upstream or FQDN mode, use the service port (Kong will resolve via DNS).
		return int(svcPort.Port), nil

	case svc.Spec.Type == corev1.ServiceTypeExternalName:
		// For ExternalName services, use service port (external service expectation).
		return int(svcPort.Port), nil

	default:
		// For all other cases (headless services, regular services with endpoint discovery).
		// Use target port since we're connecting directly to pod endpoints.
		if svcPort.TargetPort.IntVal > 0 {
			// TargetPort is explicitly set as a numeric value.
			return int(svcPort.TargetPort.IntVal), nil
		}

		// TargetPort is either named or not specified.
		// Look it up in EndpointSlices to get the actual port number.
		endpointSlices, err := getEndpointSlicesForService(ctx, cl, svc)

		if err != nil {
			return 0, fmt.Errorf("error fetching EndpointSlices for service %s/%s: %w", svc.Namespace, svc.Name, err)
		}

		for _, endpointSlice := range endpointSlices.Items {
			for _, p := range endpointSlice.Ports {
				if p.Port != nil && *p.Port > 0 && *p.Protocol == svcPort.Protocol && *p.Name == svcPort.Name {
					return int(*p.Port), nil
				}
			}
		}

		// Fallback to service port if we couldn't resolve from EndpointSlices.
		return int(svcPort.Port), nil
	}
}

// filterValidBackendRefs filters a list of BackendRefs and returns only the valid ones.
// It performs the following checks for each BackendRef:
// 1. Check if the BackendRef is supported (currently only Service).
// 2. Check if the referenced Service exists.
// 3. Check if the specified port exists in the Service.
// 4. Check if ReferenceGrant permits cross-namespace access (if needed).
// 5. Fetch EndpointSlices and extract ready endpoints (for headless services, regular services, or when FQDN is disabled).
func filterValidBackendRefs[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedBackendRef,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	backendRefs []R,
	fqdn bool,
	clusterDomain string,
) ([]validBackendRef[R], error) {
	var validBackendRefs []validBackendRef[R]

	for _, backendRef := range backendRefs {
		// Extract the `gwtypes.BackendRef` for checking the validity of the backendRef itself
		// since we do not support `filters` in `HTTPBackendRef` yet.
		bRef := gwtypes.GetBackendRef(backendRef)
		// Check if the backendRef is supported.
		if !utils.IsBackendRefSupported(bRef.Group, bRef.Kind) {
			log.Info(logger, "skipping unsupported backendRef", "group", bRef.Group, "kind", bRef.Kind)
			continue
		}

		// Determine the namespace for the referenced Service.
		bRefNamespace := parentRoute.GetNamespace()
		if bRef.Namespace != nil && *bRef.Namespace != "" {
			bRefNamespace = string(*bRef.Namespace)
		}

		// Check if the referenced Service exists.
		svc := &corev1.Service{}
		err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(bRef.Name)}, svc)
		if err != nil {
			log.Info(logger, "skipping nonexistent Service", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
			continue
		}

		// Find and validate the port in the Service.
		svcPort, err := findBackendRefPortInService(&bRef, svc)
		if err != nil {
			log.Info(logger, "skipping backendRef with invalid port", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name, "error", err)
			continue
		}

		// Check ReferenceGrant permission for cross-namespace access.
		if bRefNamespace != parentRoute.GetNamespace() {
			permitted, found, err := route.CheckReferenceGrant(ctx, cl, &bRef, parentRoute.GetObjectKind().GroupVersionKind().Kind, parentRoute.GetNamespace())
			if err != nil {
				return nil, fmt.Errorf("error checking ReferenceGrant for BackendRef %s: %w", bRef.Name, err)
			}
			if !permitted {
				if found {
					log.Info(logger, "skipping backendRef not permitted by ReferenceGrant", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
				} else {
					log.Info(logger, "skipping backendRef in different namespace without ReferenceGrant", "group", bRef.Group, "kind", bRef.Kind, "name", bRef.Name)
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
		targetPort, err := resolveTargetPort(ctx, cl, svc, svcPort, fqdn)
		if err != nil {
			return nil, err
		}

		// If we reach here, the BackendRef is valid and has endpoints.
		validBackendRefs = append(validBackendRefs, validBackendRef[R]{
			backendRef:     &backendRef,
			service:        svc,
			servicePort:    svcPort,
			readyEndpoints: readyEndpoints,
			targetPort:     targetPort,
			// Will be calculated in recalculateWeightsAcrossBackendRefs.
			weight: 0,
		})
	}

	return validBackendRefs, nil
}

// recalculateWeightsAcrossBackendRefs recalculates weights across all valid BackendRefs in a rule.
// This uses the weight calculation algorithm from weight.go to ensure mathematically
// correct proportional distribution based on the original BackendRef weights and endpoint counts.
func recalculateWeightsAcrossBackendRefs[T gwtypes.SupportedBackendRef](validBackendRefs []validBackendRef[T]) []validBackendRef[T] {
	if len(validBackendRefs) == 0 {
		return validBackendRefs
	}

	// Convert ValidBackendRef to BackendRef for weight calculation.
	backends := make([]BackendRef, len(validBackendRefs))
	for i, vbRef := range validBackendRefs {
		// Generate a unique name for this backend (using service name and namespace).
		backendName := fmt.Sprintf("%s/%s", vbRef.service.Namespace, vbRef.service.Name)

		// Get the original weight (default to 1 if not specified).
		weight := uint32(1)
		backendRef := gwtypes.GetBackendRef(*vbRef.backendRef)
		if backendRef.Weight != nil {
			weight = uint32(*backendRef.Weight)
		}

		// Number of ready endpoints (could be 1 for FQDN/ExternalName).
		endpoints := uint32(len(vbRef.readyEndpoints))

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
		backendName := fmt.Sprintf("%s/%s", vbRef.service.Namespace, vbRef.service.Name)
		endpointWeight := endpointWeights[backendName]
		validBackendRefs[i].weight = int32(endpointWeight)
	}

	return validBackendRefs
}

// existingTargetNamesByAddress lists the KongTargets already managed for the given upstream and returns a
// map from target address (host:port) to the name of an existing KongTarget to reuse for that address.
//
// Reusing an existing name keeps the "one KongTarget per (upstream, address)" invariant stable across
// reconciles WITHOUT changing the naming scheme: a brand-new address still gets a freshly minted name, but
// once a target exists for an address its name is reused. This means a changing contributing backendRef
// (e.g. a rollout where two Services resolve to the same pods, with EndpointSlices updating at different
// times) or an upgrade never mints a second KongTarget with a duplicate Spec.Target — which Konnect
// rejects via the per-upstream target uniqueness constraint.
//
// When more than one target already exists for an address, the name of an already-Programmed target is preferred.
// That way the desired target equals the one that is actually live in Konnect, so applying it is an in-place
// UPDATE (which also corrects its weight), and it avoids a deadlock where the desired target could never become
// Programmed because a non-programmed duplicate still holds the address.
func existingTargetNamesByAddress[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
](
	ctx context.Context,
	cl client.Client,
	parentRoute TPtr,
	pRef *gwtypes.ParentReference,
	upstreamName string,
) (map[string]string, error) {
	namespace := metadata.NamespaceFromParentRef(parentRoute, pRef)

	list := &configurationv1alpha1.KongTargetList{}
	if err := cl.List(ctx, list,
		client.InNamespace(namespace),
		metadata.LabelSelectorForOwnedResources(parentRoute, pRef),
	); err != nil {
		return nil, fmt.Errorf("failed to list existing KongTargets in namespace %s: %w", namespace, err)
	}

	type candidate struct {
		name       string
		programmed bool
	}
	chosen := make(map[string]candidate, len(list.Items))
	for i := range list.Items {
		t := &list.Items[i]
		// Scope to the upstream we are reconciling: the same address under a different upstream is a
		// distinct target and must keep its own name.
		if t.Spec.UpstreamRef.Name != upstreamName {
			continue
		}
		programmed := meta.IsStatusConditionTrue(t.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType)
		cur, ok := chosen[t.Spec.Target]
		switch {
		case !ok:
			fallthrough
		case programmed && !cur.programmed:
			// Prefer a Programmed target over a non-programmed one for the same address.
			fallthrough
		case programmed == cur.programmed && t.Name < cur.name:
			// Deterministic tie-break when both have the same Programmed status.
			chosen[t.Spec.Target] = candidate{name: t.Name, programmed: programmed}
		}
	}

	byAddr := make(map[string]string, len(chosen))
	for addr, c := range chosen {
		byAddr[addr] = c.name
	}
	return byAddr, nil
}

// createTargetsFromValidBackendRefs creates KongTargets from validBackendRef structs.
// This function handles all service types (ClusterIP, ExternalName, FQDN) using a unified approach.
// Endpoints that appear in more than one BackendRef are merged into a single KongTarget whose
// weight is the sum of the per-endpoint weights from all contributing BackendRefs. This prevents
// Konnect unique-constraint failures when two different BackendRef Services select the same pods.
func createTargetsFromValidBackendRefs[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedBackendRef,
](ctx context.Context, logger logr.Logger, cl client.Client, parentRoute TPtr, pRef *gwtypes.ParentReference, upstreamName string,
	validBackendRefs []validBackendRef[R],
) ([]configurationv1alpha1.KongTarget, error) {

	// Reuse the names of any KongTargets that already exist for this upstream/address so a changing
	// contributing backendRef (rollout) or an upgrade does not create a second target with a duplicate
	// Spec.Target, which Konnect rejects. See existingTargetNamesByAddress for details.
	existingByAddr, err := existingTargetNamesByAddress(ctx, cl, parentRoute, pRef, upstreamName)
	if err != nil {
		return nil, err
	}

	// seenIdx maps a target address (host:port) to its index in targets, enabling O(1) duplicate
	// detection and in-place weight accumulation without a second pass.
	seenIdx := make(map[string]int)
	var targets []configurationv1alpha1.KongTarget

	for _, vbRef := range validBackendRefs {
		// Skip backends with no endpoints (they have weight 0 anyway).
		// This should not happen, but if it happens then we skip them.
		if len(vbRef.readyEndpoints) == 0 {
			continue
		}

		// After recalculateWeightsAcrossBackendRefs, the ValidBackendRef.weight contains
		// the calculated weight per endpoint, so we use it directly for all endpoints.
		weight := vbRef.weight

		for _, endpoint := range vbRef.readyEndpoints {
			// Use the pre-calculated target port (already resolved based on service type and mode).
			port := vbRef.targetPort
			targetAddr := net.JoinHostPort(endpoint, strconv.Itoa(port))

			if idx, dup := seenIdx[targetAddr]; dup {
				// Same host:port already produced by a previous BackendRef. Sum the weights so
				// Konnect receives a single target entry with the combined traffic share.
				targets[idx].Spec.Weight += int(weight)
				log.Debug(logger, "Merged duplicate KongTarget endpoint, summing weight",
					"target", targetAddr, "addedWeight", weight)
				continue
			}

			// Prefer the name of an existing KongTarget for this address so we UPDATE it (and correct its
			// weight) rather than creating a duplicate Spec.Target under the same upstream. Only when no
			// target exists yet do we mint a fresh name from the upstream, endpoint, port and backendRef.
			targetName := existingByAddr[targetAddr]
			if targetName == "" {
				targetName = namegen.NewKongTargetName(upstreamName, endpoint, port, vbRef.backendRef)
			}
			logger := logger.WithValues("kongtarget", targetName)
			log.Debug(logger, "Creating KongTarget for BackendRef")

			tags := pkgmetadata.ExtractTags(vbRef.service)

			target, err := builder.NewKongTarget().
				WithName(targetName).
				WithNamespace(metadata.NamespaceFromParentRef(parentRoute, pRef)).
				WithLabels(parentRoute, pRef).
				WithAnnotations(parentRoute, pRef).
				WithUpstreamRef(upstreamName).
				WithTarget(endpoint, port).
				WithWeight(&weight).
				WithSpecTags(tags).
				Build()
			if err != nil {
				log.Error(logger, err, "Failed to build KongTarget resource")
				return nil, fmt.Errorf("failed to build KongTarget %s: %w", targetName, err)
			}

			_, err = translator.VerifyAndUpdate(ctx, logger, cl, &target, parentRoute, false)
			if err != nil {
				return nil, err
			}

			seenIdx[targetAddr] = len(targets)
			targets = append(targets, target)
		}
	}

	return targets, nil
}
