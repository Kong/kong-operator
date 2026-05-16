package service

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// isNonTLSProtocol returns true when the protocol is incompatible with client certificates.
// Ported from ingress-controller/internal/dataplane/translator/ingressrules.go.
func isNonTLSProtocol(proto string) bool {
	nonTLSProtocols := []string{"http", "grpc", "tcp", "tls_passthrough", "udp", "ws"}
	return slices.Contains(nonTLSProtocols, proto)
}

// resolveClientCertFromBackendRefs iterates over backendRefs and returns the first Secret name
// found in the konghq.com/client-cert annotation together with the Service that owned it.
// Returns empty string + nil when no backend Service carries the annotation.
func resolveClientCertFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) (secretName string, ownerSvc *corev1.Service) {
	for _, ref := range backendRefs {
		if name, svc, ok := extractClientCertFromBackendRef(ctx, cl, logger, namespace, ref); ok {
			return name, svc
		}
	}
	return "", nil
}

// extractClientCertFromBackendRef returns the client-cert secret name and owning Service for a single
// BackendRef. Returns ("", nil, false) when the annotation is absent or the Service cannot be fetched.
func extractClientCertFromBackendRef(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	namespace string,
	backendRef gwtypes.BackendRef,
) (secretName string, svc *corev1.Service, ok bool) {
	if !route.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
		return "", nil, false
	}

	refNamespace := namespace
	if backendRef.Namespace != nil && *backendRef.Namespace != "" {
		refNamespace = string(*backendRef.Namespace)
	}

	svcObj := &corev1.Service{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: refNamespace, Name: string(backendRef.Name)}, svcObj); err != nil {
		log.Debug(logger, "Failed to fetch backend Service for client-cert annotation check",
			"service", refNamespace+"/"+string(backendRef.Name), "error", err)
		return "", nil, false
	}

	name := metadata.ExtractClientCertificate(svcObj.GetAnnotations())
	if name == "" {
		return "", nil, false
	}

	return name, svcObj, true
}
