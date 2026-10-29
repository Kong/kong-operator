package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// isNonTLSProtocol returns true when the protocol is incompatible with client certificates.
// Ported from ingress-controller/internal/dataplane/translator/ingressrules.go.
func isNonTLSProtocol(proto string) bool {
	nonTLSProtocols := []string{"http", "grpc", "tcp", "tls_passthrough", "udp", "ws"}
	return slices.Contains(nonTLSProtocols, proto)
}

type clientCertificateSelection struct {
	secretName     string
	ownerSvc       *corev1.Service
	certificateRef *commonv1alpha1.NamespacedRef
}

// resolveClientCertFromBackendRefs requires direct references to agree across all
// backends. Legacy-only selections retain their first-annotated-backend behavior.
func resolveClientCertFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
) (clientCertificateSelection, error) {
	var selection clientCertificateSelection
	var selectedKey string
	var unannotated bool
	invalid := func(message string) (clientCertificateSelection, error) {
		return clientCertificateSelection{}, &hgerrors.ClientCertificateRefError{
			Reason: "InvalidCertificateRef", Message: message,
		}
	}
	for _, backendRef := range backendRefs {
		if !utils.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
			continue
		}
		refNamespace := namespace
		if backendRef.Namespace != nil && *backendRef.Namespace != "" {
			refNamespace = string(*backendRef.Namespace)
		}
		var svc corev1.Service
		key := client.ObjectKey{Namespace: refNamespace, Name: string(backendRef.Name)}
		if err := cl.Get(ctx, key, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				continue // Backend resolution reports missing Services separately.
			}
			return clientCertificateSelection{}, fmt.Errorf("failed to get backend Service %s for client certificate selection: %w", key, err)
		}
		ref, err := metadata.ExtractClientCertificateRef(svc.Annotations, svc.Namespace)
		if err != nil {
			return invalid(fmt.Sprintf("Service %s: %s", key, err))
		}
		secretName := metadata.ExtractClientCertificate(svc.Annotations)
		var certKey string
		switch {
		case ref != nil:
			certKey = "KongCertificate " + *ref.Namespace + "/" + ref.Name
		case secretName != "":
			certKey = "Secret " + svc.Namespace + "/" + secretName
		default:
			unannotated = true
			continue
		}
		if selectedKey != "" {
			if selectedKey != certKey && (ref != nil || selection.certificateRef != nil) {
				return invalid(fmt.Sprintf("Backend Services sharing a KongService select different client certificates: %s and %s", selectedKey, certKey))
			}
			continue
		}
		selectedKey = certKey
		selection = clientCertificateSelection{secretName: secretName, ownerSvc: &svc, certificateRef: ref}
	}
	if selection.certificateRef != nil && unannotated {
		return invalid("All backend Services sharing a KongService must select the same konghq.com/client-cert-ref")
	}
	return selection, nil
}

// ValidateClientCertificateRefs performs the same side-effect-free checks as translation,
// using the Gateway namespace where the generated KongService actually lives.
func ValidateClientCertificateRefs(
	ctx context.Context,
	cl client.Client,
	routeNamespace string,
	backendRefs []gwtypes.BackendRef,
	gateway *gwtypes.Gateway,
	defaultProtocol string,
	logger logr.Logger,
) error {
	selection, err := resolveClientCertFromBackendRefs(ctx, cl, routeNamespace, backendRefs)
	if err != nil {
		return err
	}
	if selection.certificateRef == nil {
		return nil
	}
	cp, err := refs.GetControlPlaneRefByGateway(ctx, cl, gateway)
	if err != nil {
		if errors.Is(err, hgerrors.ErrGatewayNotReferencingControlPlane) {
			return &hgerrors.ClientCertificateRefError{
				Reason: "InvalidCertificateRef", Message: "KongCertificate references require a Konnect-backed Gateway",
			}
		}
		return err
	}
	protocol := resolveProtocolFromBackendRefs(ctx, cl, routeNamespace, backendRefs, defaultProtocol, logger)
	return validateDirectClientCertificate(ctx, cl, selection.certificateRef, gateway.Namespace, cp, protocol)
}

func validateDirectClientCertificate(
	ctx context.Context,
	cl client.Client,
	ref *commonv1alpha1.NamespacedRef,
	serviceNamespace string,
	cp *commonv1alpha1.ControlPlaneRef,
	protocol string,
) error {
	if ref == nil {
		return nil
	}
	if isNonTLSProtocol(protocol) {
		return &hgerrors.ClientCertificateRefError{
			Reason: "InvalidCertificateRef",
			Message: fmt.Sprintf("%s requires a TLS-capable upstream protocol, got %s",
				metadata.ClientCertificateRefAnnotation, protocol),
		}
	}
	_, err := refs.ResolveServiceKongCertificate(ctx, cl, &configurationv1alpha1.KongService{
		Namespace: serviceNamespace,
		Spec: configurationv1alpha1.KongServiceSpec{
			ClientCertificateRef: ref, ControlPlaneRef: cp,
		},
	})
	return err
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
		Name:        grantName,
		Namespace:   secretNamespace,
		Labels:      metadata.BuildLabels(parentRoute, pRef),
		Annotations: metadata.BuildAnnotations(parentRoute, pRef),
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
	if !utils.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
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
