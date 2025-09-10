package refs

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// GetNamespacedRefs returns a slice of KonnectNamespacedRef for the given runtime.Object, based on its type.
func GetNamespacedRefs(ctx context.Context, cl client.Client, obj runtime.Object) ([]commonv1alpha1.KonnectNamespacedRef, error) {
	switch o := obj.(type) {
	// TODO: add other types here
	case *gwtypes.HTTPRoute:
		return byHTTPRoute(ctx, cl, *o)
	default:
		return nil, nil
	}
}

// byHTTPRoute returns a slice of KonnectNamespacedRef associated with the given HTTPRoute, or an error if retrieval fails.
func byHTTPRoute(ctx context.Context, cl client.Client, httpRoute gwtypes.HTTPRoute) ([]commonv1alpha1.KonnectNamespacedRef, error) {
	namespacedRefs := []commonv1alpha1.KonnectNamespacedRef{}
	gateways := GetGatewaysByHTTPRoute(ctx, cl, httpRoute)
	// TODO: remove duplicates
	for _, gw := range gateways {
		ref, err := byGateway(ctx, cl, gw)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			continue
		}
		namespacedRefs = append(namespacedRefs, *ref)
	}
	return namespacedRefs, nil
}

// byGateway returns the KonnectNamespacedRef associated with the given Gateway, or nil if not found.
func byGateway(ctx context.Context, cl client.Client, gateway gwtypes.Gateway) (*commonv1alpha1.KonnectNamespacedRef, error) {
	gatewayClass := getGatewayClassByGateway(ctx, cl, gateway)
	if gatewayClass == nil {
		return nil, nil
	}
	return byGatewayClass(ctx, cl, *gatewayClass)
}

// byGatewayClass returns the KonnectNamespacedRef associated with the given GatewayClass, or nil if not found.
func byGatewayClass(ctx context.Context, cl client.Client, gatewayClass gwtypes.GatewayClass) (*commonv1alpha1.KonnectNamespacedRef, error) {
	gatewayConfiguration := getGatewayConfigurationByGatewayClass(ctx, cl, gatewayClass)
	if gatewayConfiguration == nil {
		return nil, nil
	}
	return byGatewayConfiguration(ctx, cl, *gatewayConfiguration)
}

// byGatewayConfiguration returns the KonnectNamespacedRef associated with the given GatewayConfiguration, or an error if retrieval fails.
func byGatewayConfiguration(ctx context.Context, cl client.Client, gatewayConfiguration gwtypes.GatewayConfiguration) (*commonv1alpha1.KonnectNamespacedRef, error) {
	konnectExtension, err := getKonnectExtensionByGatewayConfiguration(ctx, cl, gatewayConfiguration)
	if err != nil {
		return nil, err
	}
	if konnectExtension == nil {
		return nil, nil
	}
	return byKonnectExtension(ctx, cl, *konnectExtension)
}

// byKonnectExtension returns the KonnectNamespacedRef from the given KonnectExtension, or empty if not present.
func byKonnectExtension(ctx context.Context, cl client.Client, konnectExtension konnectv1alpha2.KonnectExtension) (*commonv1alpha1.KonnectNamespacedRef, error) {
	cpRef := konnectExtension.GetControlPlaneRef()
	// This should not happen, as the CEL validation ensures that a KonnectExtension must always reference a Konnect ControlPlane.
	if cpRef == nil || cpRef.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef {
		return nil, nil
	}

	ns := konnectExtension.Namespace
	if cpRef.KonnectNamespacedRef.Namespace != "" {
		ns = cpRef.KonnectNamespacedRef.Namespace
	}

	if ns != konnectExtension.Namespace {
		// cross-namespace references are not supported
		return nil, errors.New("cross-namespace references between KonnectExtension and ControlPlane are not supported")
	}

	konnectGatewayControlPlane := konnectv1alpha2.KonnectGatewayControlPlane{}
	if err := cl.Get(ctx, client.ObjectKey{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: ns,
	}, &konnectGatewayControlPlane); err != nil {
		return nil, err
	}

	return &commonv1alpha1.KonnectNamespacedRef{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: ns,
	}, nil
}
