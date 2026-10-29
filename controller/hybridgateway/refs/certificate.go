package refs

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ListenerCertificateRefError describes an invalid Listener certificate reference.
type ListenerCertificateRefError struct {
	Reason  gatewayv1.ListenerConditionReason
	Message string
}

func (e *ListenerCertificateRefError) Error() string {
	return e.Message
}

// IsTLSPassthroughListener reports whether a Listener ignores certificate references.
func IsTLSPassthroughListener(listener gatewayv1.Listener) bool {
	return listener.Protocol == gatewayv1.TLSProtocolType && listener.TLS != nil &&
		listener.TLS.Mode != nil && *listener.TLS.Mode == gatewayv1.TLSModePassthrough
}

// IsKongCertificateRef reports whether a Listener reference explicitly selects a KongCertificate.
func IsKongCertificateRef(ref gatewayv1.SecretObjectReference) bool {
	return ref.Group != nil && *ref.Group == gatewayv1.Group(configurationv1alpha1.GroupVersion.Group) &&
		ref.Kind != nil && *ref.Kind == "KongCertificate"
}

// ResolveListenerKongCertificate validates both permission layers and the effective Konnect ControlPlane.
// Invalid references return a ListenerCertificateRefError; Kubernetes API failures return ordinary errors.
func ResolveListenerKongCertificate(
	ctx context.Context,
	cl client.Client,
	referenceGrantVersion schema.GroupVersion,
	gateway *gwtypes.Gateway,
	ref gatewayv1.SecretObjectReference,
) (*configurationv1alpha1.KongCertificate, error) {
	invalid := func(message string) (*configurationv1alpha1.KongCertificate, error) {
		return nil, &ListenerCertificateRefError{
			Reason:  gatewayv1.ListenerReasonInvalidCertificateRef,
			Message: message,
		}
	}
	namespace := gateway.Namespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		namespace = string(*ref.Namespace)
	}
	key := client.ObjectKey{Namespace: namespace, Name: string(ref.Name)}

	if namespace != gateway.Namespace {
		allowed, err := k8sutils.AllowedByReferenceGrants(ctx, cl, referenceGrantVersion,
			gwtypes.ReferenceGrantFrom{
				Group: gatewayv1.GroupName, Kind: "Gateway", Namespace: gatewayv1.Namespace(gateway.Namespace),
			}, namespace, gwtypes.ReferenceGrantTo{
				Group: gatewayv1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongCertificate", Name: new(ref.Name),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to check ReferenceGrants for KongCertificate %s: %w", key, err)
		}
		if !allowed {
			return nil, &ListenerCertificateRefError{
				Reason:  gatewayv1.ListenerReasonRefNotPermitted,
				Message: fmt.Sprintf("KongCertificate %s reference not allowed by any ReferenceGrant", key),
			}
		}
		if err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, gateway.Namespace, namespace, key.Name,
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongSNI")),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongCertificate")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				return nil, &ListenerCertificateRefError{
					Reason:  gatewayv1.ListenerReasonRefNotPermitted,
					Message: fmt.Sprintf("KongSNI reference to KongCertificate %s not allowed by any KongReferenceGrant", key),
				}
			}
			return nil, err
		}
	}

	gatewayCPRef, err := GetControlPlaneRefByGateway(ctx, cl, gateway)
	if err != nil {
		if errors.Is(err, hybridgatewayerrors.ErrGatewayNotReferencingControlPlane) {
			return invalid("KongCertificate references are only supported for Konnect Gateways")
		}
		return nil, err
	}
	cert, err := resolveCertificateForControlPlane(ctx, cl, key, *gatewayCPRef, gateway.Namespace)
	if refErr, ok := errors.AsType[*hybridgatewayerrors.ClientCertificateRefError](err); ok {
		return invalid(refErr.Message)
	}
	return cert, err
}

// ResolveServiceKongCertificate checks authorization from the actual KongService
// namespace, then readiness and binding to its explicitly selected remote ControlPlane.
// No Gateway -> certificate grant is needed: the Gateway API reference is Route -> Service.
func ResolveServiceKongCertificate(
	ctx context.Context,
	cl client.Client,
	service *configurationv1alpha1.KongService,
) (*configurationv1alpha1.KongCertificate, error) {
	ref := service.Spec.ClientCertificateRef
	if ref == nil {
		return nil, nil
	}
	namespace := service.Namespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		namespace = *ref.Namespace
	}
	key := client.ObjectKey{Namespace: namespace, Name: ref.Name}
	if namespace != service.Namespace {
		if err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, service.Namespace, namespace, ref.Name,
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongService")),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongCertificate")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				return nil, &hybridgatewayerrors.ClientCertificateRefError{
					Reason: string(gatewayv1.RouteReasonRefNotPermitted),
					Message: fmt.Sprintf("KongService in namespace %s reference to KongCertificate %s not allowed by any KongReferenceGrant",
						service.Namespace, key),
				}
			}
			return nil, err
		}
	}
	if service.Spec.ControlPlaneRef == nil {
		return nil, &hybridgatewayerrors.ClientCertificateRefError{
			Reason: "InvalidCertificateRef", Message: "KongService does not reference a Konnect ControlPlane",
		}
	}
	return resolveCertificateForControlPlane(ctx, cl, key, *service.Spec.ControlPlaneRef, service.Namespace)
}

func resolveCertificateForControlPlane(
	ctx context.Context,
	cl client.Client,
	key client.ObjectKey,
	cpRef commonv1alpha1.ControlPlaneRef,
	namespace string,
) (*configurationv1alpha1.KongCertificate, error) {
	invalid := func(message string) (*configurationv1alpha1.KongCertificate, error) {
		return nil, &hybridgatewayerrors.ClientCertificateRefError{Reason: "InvalidCertificateRef", Message: message}
	}
	var cert configurationv1alpha1.KongCertificate
	if err := cl.Get(ctx, key, &cert); err != nil {
		if apierrors.IsNotFound(err) {
			return invalid(fmt.Sprintf("Referenced KongCertificate %s does not exist", key))
		}
		return nil, fmt.Errorf("failed to get KongCertificate %s: %w", key, err)
	}
	if !cert.DeletionTimestamp.IsZero() {
		return invalid(fmt.Sprintf("Referenced KongCertificate %s is being deleted", key))
	}
	condition, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &cert)
	if !ok || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != cert.Generation ||
		cert.GetKonnectID() == "" || cert.Status.Konnect == nil {
		return invalid(fmt.Sprintf("Referenced KongCertificate %s is not programmed in Konnect yet", key))
	}
	if cpRef.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef || cpRef.KonnectNamespacedRef == nil {
		return invalid("Certificate bindings require a Konnect ControlPlane reference")
	}
	gatewayCP, err := controlplane.GetCPForRef(ctx, cl, cpRef, namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return invalid("Referenced Konnect ControlPlane does not exist")
		}
		return nil, err
	}
	certCPRef := cert.GetControlPlaneRef()
	if certCPRef == nil || certCPRef.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef || certCPRef.KonnectNamespacedRef == nil {
		return invalid(fmt.Sprintf("Referenced KongCertificate %s does not reference a Konnect ControlPlane", key))
	}
	certCP, err := controlplane.GetCPForRef(ctx, cl, *certCPRef, cert.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return invalid(fmt.Sprintf("Referenced KongCertificate %s ControlPlane does not exist", key))
		}
		return nil, err
	}
	if gatewayCP.Status.ID == "" || certCP.Status.ID == "" {
		return invalid("Binding and certificate Konnect ControlPlanes must be programmed")
	}
	for _, cp := range []*konnectv1alpha2.KonnectGatewayControlPlane{gatewayCP, certCP} {
		condition, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
		if !ok || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != cp.GetGeneration() {
			return invalid("Binding and certificate Konnect ControlPlanes must be programmed")
		}
	}
	if gatewayCP.Status.ID != certCP.Status.ID || cert.Status.Konnect.ControlPlaneID != gatewayCP.Status.ID {
		return invalid(fmt.Sprintf("Referenced KongCertificate %s belongs to a different Konnect ControlPlane", key))
	}
	return &cert, nil
}
