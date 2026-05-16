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
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
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
) (kongService *configurationv1alpha1.KongService, kongCertificate *configurationv1alpha1.KongCertificate, err error) {

	var serviceName string
	var namespace string
	var backendRefs []gwtypes.BackendRef
	var defaultProtocol string

	switch r := any(parentRoute).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, nil, fmt.Errorf("failed to build KongService : unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		serviceName = namegen.NewKongServiceNameForHTTPRouteRule(r, cp, httpRule)
		namespace = r.Namespace
		backendRefs = utils.HTTPBackendRefsToBackendRefs(httpRule.BackendRefs)
		defaultProtocol = "http"

	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, nil, fmt.Errorf("failed to build KongService : unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		serviceName = namegen.NewKongServiceNameForTLSRouteRule(r, cp, tlsRule)
		namespace = r.Namespace
		backendRefs = tlsRule.BackendRefs
		defaultProtocol = "tcp"

	// TODO: add other types of routes and rules when we support them.

	// Should be unreachable.
	default:
		return nil, nil, fmt.Errorf("failed to build KongService: unsupported route type: %T", parentRoute)
	}

	// Resolve service attributes once, outside the switch — future route types only add a case above.
	protocol := resolveProtocolFromBackendRefs(ctx, cl, namespace, backendRefs, defaultProtocol, logger)
	path := resolvePathFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	tlsVerify := resolveTLSVerifyFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	tlsVerifyDepth := resolveTLSVerifyDepthFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	connectTimeout := resolveConnectTimeoutFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	readTimeout := resolveReadTimeoutFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	writeTimeout := resolveWriteTimeoutFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	retries := resolveRetriesFromBackendRefs(ctx, cl, namespace, backendRefs, logger)

	logger = logger.WithValues("kongservice", serviceName)
	log.Debug(logger, fmt.Sprintf("Generating KongService for %s rule", parentRoute.GetObjectKind().GroupVersionKind().Kind))

	// Resolve client certificate from the backend Service annotations (first-wins).
	certSecretName, certOwnerSvc := resolveClientCertFromBackendRefs(ctx, cl, namespace, backendRefs, logger)
	kongCertificate = buildClientCertificate(
		ctx, logger, cl, parentRoute, pRef, cp,
		serviceName, namespace, certSecretName, certOwnerSvc, protocol,
	)

	service, err := builder.NewKongService().
		WithName(serviceName).
		WithNamespace(metadata.NamespaceFromParentRef(parentRoute, pRef)).
		WithLabels(parentRoute, pRef).
		WithAnnotations(parentRoute, pRef).
		WithSpecName(serviceName).
		WithSpecHost(upstreamName).
		WithProtocol(protocol).
		WithPath(path).
		WithTLSVerify(tlsVerify).
		WithTLSVerifyDepth(tlsVerifyDepth).
		WithConnectTimeout(connectTimeout).
		WithReadTimeout(readTimeout).
		WithWriteTimeout(writeTimeout).
		WithRetries(retries).
		WithClientCertificateRef(clientCertRefName(kongCertificate)).
		WithControlPlaneRef(*cp).Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongService resource")
		return nil, nil, fmt.Errorf("failed to build KongService %s: %w", serviceName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &service, parentRoute, false); err != nil {
		return nil, nil, err
	}

	return &service, kongCertificate, nil
}

// clientCertRefName returns the cert's metadata.name or "" when cert is nil.
func clientCertRefName(cert *configurationv1alpha1.KongCertificate) string {
	if cert == nil {
		return ""
	}
	return cert.Name
}

// buildClientCertificate creates or updates a KongCertificate for the given service's client-cert annotation.
// Returns the KongCertificate when built, or nil when skipped (no annotation, protocol mismatch, Secret missing).
func buildClientCertificate[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	serviceName string,
	serviceNamespace string,
	secretName string,
	ownerSvc *corev1.Service,
	effectiveProtocol string,
) *configurationv1alpha1.KongCertificate {
	if secretName == "" {
		return nil
	}

	if isNonTLSProtocol(strings.ToLower(effectiveProtocol)) {
		log.Info(logger, "Skipping client certificate for non-TLS service protocol",
			"protocol", effectiveProtocol,
			"secretName", secretName)
		return nil
	}

	secretNamespace := serviceNamespace
	if ownerSvc != nil {
		secretNamespace = ownerSvc.Namespace
	}

	// Verify the Secret exists; skip on failure (KongService still built without ref).
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret); err != nil {
		log.Error(logger, err, "Failed to fetch client-cert Secret; skipping certificate",
			"secret", secretNamespace+"/"+secretName)
		return nil
	}

	cert, err := builder.NewKongCertificate().
		WithName(serviceName).
		WithNamespace(serviceNamespace).
		WithSecretRef(secretName, secretNamespace).
		WithControlPlaneRef(*cp).
		WithLabelsForRoute(parentRoute, pRef).
		WithAnnotationsForRoute(parentRoute, pRef).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongCertificate resource", "cert", serviceName)
		return nil
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &cert, parentRoute, false); err != nil {
		log.Error(logger, err, "Failed to verify/update KongCertificate", "cert", serviceName)
		return nil
	}

	return &cert
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

// resolvePathFromBackendRefs returns the path taken from the first backend Service
// that carries the konghq.com/path annotation. Empty string if none.
func resolvePathFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) string {
	for _, backendRef := range backendRefs {
		if path, ok := extractPathFromBackendRef(ctx, cl, logger, namespace, backendRef); ok {
			return path
		}
	}
	return ""
}

// extractPathFromBackendRef returns the path from the konghq.com/path annotation on the
// backend Service referenced by the BackendRef.
func extractPathFromBackendRef(
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
		log.Debug(logger, "Failed to fetch backend Service for path annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return "", false
	}

	path := metadata.ExtractPath(svc.GetAnnotations())
	if path == "" {
		return "", false
	}

	log.Debug(logger, "Using path from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "path", path)
	return path, true
}

// resolveTLSVerifyFromBackendRefs returns the tls-verify value taken from
// the first backend Service that carries the konghq.com/tls-verify annotation.
func resolveTLSVerifyFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *bool {
	for _, backendRef := range backendRefs {
		if v := extractTLSVerifyFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractTLSVerifyFromBackendRef returns the tls-verify value from the konghq.com/tls-verify
// annotation on the backend Service referenced by the BackendRef.
func extractTLSVerifyFromBackendRef(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	namespace string,
	backendRef gwtypes.BackendRef,
) *bool {
	if !route.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
		return nil
	}

	bRefNamespace := namespace
	if backendRef.Namespace != nil && *backendRef.Namespace != "" {
		bRefNamespace = string(*backendRef.Namespace)
	}

	svc := &corev1.Service{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(backendRef.Name)}, svc); err != nil {
		log.Debug(logger, "Failed to fetch backend Service for tls-verify annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v, err := metadata.ExtractTLSVerify(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse tls-verify annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a valid boolean")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using tls-verify from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "tls-verify", *v)
	return v
}

// resolveTLSVerifyDepthFromBackendRefs returns the tls-verify-depth value taken from
// the first backend Service that carries the konghq.com/tls-verify-depth annotation.
func resolveTLSVerifyDepthFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *int64 {
	for _, backendRef := range backendRefs {
		if v := extractTLSVerifyDepthFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractTLSVerifyDepthFromBackendRef returns the tls-verify-depth value from the
// konghq.com/tls-verify-depth annotation on the backend Service referenced by the BackendRef.
func extractTLSVerifyDepthFromBackendRef(
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
		log.Debug(logger, "Failed to fetch backend Service for tls-verify-depth annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v, err := metadata.ExtractTLSVerifyDepth(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse tls-verify-depth annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a non-negative integer")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using tls-verify-depth from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "tls-verify-depth", *v)
	return v
}

// resolveConnectTimeoutFromHTTPRouteBackendRefs returns the connect-timeout value taken from
// the first HTTPRoute backend Service that carries the konghq.com/connect-timeout annotation.
// resolveConnectTimeoutFromBackendRefs returns the connect-timeout value taken from
// the first backend Service that carries the konghq.com/connect-timeout annotation.
func resolveConnectTimeoutFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *int64 {
	for _, backendRef := range backendRefs {
		if v := extractConnectTimeoutFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractConnectTimeoutFromBackendRef returns the connect-timeout value from the
// konghq.com/connect-timeout annotation on the backend Service referenced by the BackendRef.
func extractConnectTimeoutFromBackendRef(
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
		log.Debug(logger, "Failed to fetch backend Service for connect-timeout annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v, err := metadata.ExtractConnectTimeout(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse connect-timeout annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a non-negative integer")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using connect-timeout from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "connect-timeout", *v)
	return v
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

	v, err := metadata.ExtractReadTimeout(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse read-timeout annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a non-negative integer")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using read-timeout from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "read-timeout", *v)
	return v
}

// resolveWriteTimeoutFromBackendRefs returns the write-timeout value taken from
// the first backend Service that carries the konghq.com/write-timeout annotation.
func resolveWriteTimeoutFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *int64 {
	for _, backendRef := range backendRefs {
		if v := extractWriteTimeoutFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractWriteTimeoutFromBackendRef returns the write-timeout value from the
// konghq.com/write-timeout annotation on the backend Service referenced by the BackendRef.
func extractWriteTimeoutFromBackendRef(
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
		log.Debug(logger, "Failed to fetch backend Service for write-timeout annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v, err := metadata.ExtractWriteTimeout(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse write-timeout annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a non-negative integer")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using write-timeout from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "write-timeout", *v)
	return v
}

// resolveRetriesFromBackendRefs returns the retries value taken from
// the first backend Service that carries the konghq.com/retries annotation.
func resolveRetriesFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) *int64 {
	for _, backendRef := range backendRefs {
		if v := extractRetriesFromBackendRef(ctx, cl, logger, namespace, backendRef); v != nil {
			return v
		}
	}
	return nil
}

// extractRetriesFromBackendRef returns the retries value from the konghq.com/retries
// annotation on the backend Service referenced by the BackendRef.
func extractRetriesFromBackendRef(
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
		log.Debug(logger, "Failed to fetch backend Service for retries annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return nil
	}

	v, err := metadata.ExtractRetries(svc.GetAnnotations())
	if err != nil {
		log.Error(logger, err, "Failed to parse retries annotation, ignoring annotation value",
			"service", fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName()),
			"WARNING", "The malformed annotations will be treated as errors in versions from KO 2.3, please fix the annotation value to be a non-negative integer")
	}
	if v == nil {
		return nil
	}

	log.Debug(logger, "Using retries from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "retries", *v)
	return v
}
