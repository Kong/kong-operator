package service

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
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

// buildClientCertReferenceGrant creates a KongReferenceGrant in secretNamespace that permits the
// KongCertificate living in certNamespace to reference the named Secret.
// Returns nil when certNamespace == secretNamespace (same-namespace reference needs no grant).
func buildClientCertReferenceGrant[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	pRef *gwtypes.ParentReference,
	grantName string, // same as serviceName / KongCertificate name
	certNamespace string, // Gateway namespace — where KongCertificate lives
	secretName string, // referenced Secret name
	secretNamespace string, // ownerSvc.Namespace — where the Secret lives
) *configurationv1alpha1.KongReferenceGrant {
	if certNamespace == secretNamespace {
		// Same namespace: the KongCertificate reconciler does not check for a grant.
		return nil
	}

	secretObjectName := configurationv1alpha1.ObjectName(secretName)
	grant := &configurationv1alpha1.KongReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:        grantName,
			Namespace:   secretNamespace,
			Labels:      metadata.BuildLabels(parentRoute, pRef),
			Annotations: metadata.BuildAnnotations(parentRoute, pRef),
		},
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:      configurationv1alpha1.Kind("KongCertificate"),
					Namespace: configurationv1alpha1.Namespace(certNamespace),
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: configurationv1alpha1.Group("core"),
					Kind:  configurationv1alpha1.Kind("Secret"),
					Name:  &secretObjectName,
				},
			},
		},
	}

	if _, err := translator.VerifyAndUpdate(ctx, logger, cl, grant, parentRoute, false); err != nil {
		log.Error(logger, err, "Failed to verify/update KongReferenceGrant", "grant", grantName)
		return nil
	}

	return grant
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
