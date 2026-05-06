package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// ServiceForRule creates or updates a KongService for the given route rule.
// This function handles the creation of services with proper annotations that track
// which routes reference the KongService. If the KongService already exists, it appends
// the current route kind and name to the existing hybrid-routes annotation.
//
// The function performs the following operations:
// 1. Generates the KongService name using the namegen package
// 2. Checks if a KongService with that name already exists in the cluster
// 3. If it exists, appends the current route kind and name to the existing hybrid-routes annotation
// 4. If it doesn't exist, creates a new KongService
// 5. Returns the KongService resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - route: The route resource that needs the service
//   - rule: The specific rule within the route
//   - pRef: The parent reference (Gateway) for the route
//   - cp: The control plane reference for the service
//   - upstreamName: The name of the KongUpstream this service should point to
//
// Returns:
//   - kongService: The created or updated service resource
//   - err: Any error that occurred during the process
func ServiceForRule[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedRouteRule,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	rule R,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	upstreamName string,
) (kongService *configurationv1alpha1.KongService, err error) {

	var serviceName string
	var namespace string
	var backendRefs []gwtypes.BackendRef
	var defaultProtocol string

	switch r := any(parentRoute).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongService : unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		serviceName = namegen.NewKongServiceNameForHTTPRouteRule(r, cp, httpRule)
		namespace = r.Namespace
		backendRefs = httpBackendRefsToBackendRefs(httpRule.BackendRefs)
		defaultProtocol = "http"

	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongService : unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		serviceName = namegen.NewKongServiceNameForTLSRouteRule(r, cp, tlsRule)
		namespace = r.Namespace
		backendRefs = tlsRule.BackendRefs
		defaultProtocol = "tcp"

	// TODO: add other types of routes and rules when we support them.

	// Should be unreachable.
	default:
		return nil, fmt.Errorf("failed to build KongService: unsupported route type: %T", parentRoute)
	}

	// Resolve service attributes once, outside the switch — future route types only add a case above.
	protocol := resolveProtocolFromBackendRefs(ctx, cl, namespace, backendRefs, defaultProtocol, logger)
	readTimeout := resolveReadTimeoutFromBackendRefs(ctx, cl, namespace, backendRefs, logger)

	logger = logger.WithValues("kongservice", serviceName)
	log.Debug(logger, fmt.Sprintf("Generating KongService for %s rule", parentRoute.GetObjectKind().GroupVersionKind().Kind))

	service, err := builder.NewKongService().
		WithName(serviceName).
		WithNamespace(metadata.NamespaceFromParentRef(parentRoute, pRef)).
		WithLabels(parentRoute, pRef).
		WithAnnotations(parentRoute, pRef).
		WithSpecName(serviceName).
		WithSpecHost(upstreamName).
		WithProtocol(protocol).
		WithReadTimeout(readTimeout).
		WithControlPlaneRef(*cp).Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongService resource")
		return nil, fmt.Errorf("failed to build KongService %s: %w", serviceName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &service, parentRoute, false); err != nil {
		return nil, err
	}

	return &service, nil
}

// httpBackendRefsToBackendRefs unwraps []HTTPBackendRef to []BackendRef.
func httpBackendRefsToBackendRefs(refs []gwtypes.HTTPBackendRef) []gwtypes.BackendRef {
	out := make([]gwtypes.BackendRef, len(refs))
	for i, r := range refs {
		out[i] = r.BackendRef
	}
	return out
}

// resolveProtocolFromBackendRefs inspects the Kubernetes Service annotations of the
// backend references to determine the upstream protocol. If any backend Service has a
// valid konghq.com/protocol annotation, that protocol is returned. Otherwise,
// defaultProtocol is returned.
func resolveProtocolFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	defaultProtocol string,
	logger logr.Logger,
) string {
	for _, backendRef := range backendRefs {
		if protocol, ok := extractProtocolFromBackendRef(ctx, cl, logger, namespace, backendRef); ok {
			return protocol
		}
	}
	return defaultProtocol
}

// extractProtocolFromBackendRef returns the protocol in the annotation konghq.com/protocol
// of the backend service referenced in the BackendRef if the annotation value is a valid Kong service protocol.
// Also returns a boolean value that is true when a valid protocol is in the annotation.
// See: https://github.com/Kong/kong-operator/pull/3750
func extractProtocolFromBackendRef(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	namespace string,
	backendRef gwtypes.BackendRef,
) (string, bool) {

	if !route.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
		return "", false
	}

	bRefNamespace := namespace
	if backendRef.Namespace != nil && *backendRef.Namespace != "" {
		bRefNamespace = string(*backendRef.Namespace)
	}

	svc := &corev1.Service{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(backendRef.Name)}, svc); err != nil {
		log.Debug(logger, "Failed to fetch backend Service for protocol annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return "", false
	}

	protocol := strings.ToLower(metadata.ExtractProtocol(svc.GetAnnotations()))
	if protocol == "" {
		return "", false
	}

	if !metadata.IsValidProtocol(protocol) {
		log.Info(logger, "Ignoring invalid konghq.com/protocol annotation value on backend Service",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "protocol", protocol)
		return "", false
	}

	log.Debug(logger, "Using protocol from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "protocol", protocol)
	return protocol, true
}

// resolveReadTimeoutFromHTTPRouteBackendRefs returns the read-timeout value taken from
// the first HTTPRoute backend Service that carries the konghq.com/read-timeout annotation.
// resolveReadTimeoutFromBackendRefs returns the read-timeout value taken from
// the first backend Service that carries the konghq.com/read-timeout annotation.
func resolveReadTimeoutFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *int64 {
	for _, backendRef := range backendRefs {
		if v := extractReadTimeoutFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractReadTimeoutFromBackendRef returns the read-timeout value from the
// konghq.com/read-timeout annotation on the backend Service referenced by the BackendRef.
func extractReadTimeoutFromBackendRef(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	namespace string,
	backendRef gwtypes.BackendRef,
) *int64 {
	if !route.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
		return nil
	}

	bRefNamespace := namespace
	if backendRef.Namespace != nil && *backendRef.Namespace != "" {
		bRefNamespace = string(*backendRef.Namespace)
	}

	svc := &corev1.Service{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(backendRef.Name)}, svc); err != nil {
		log.Debug(logger, "Failed to fetch backend Service for read-timeout annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v := metadata.ExtractReadTimeout(svc.GetAnnotations())
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using read-timeout from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "read-timeout", *v)
	return v
}
