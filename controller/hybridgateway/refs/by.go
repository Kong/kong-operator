package refs

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	hybridgatewayerrors "github.com/kong/kong-operator/controller/hybridgateway/errors"
	gwtypes "github.com/kong/kong-operator/internal/types"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
)

// GatewaysByNamespacedRef associates a KonnectNamespacedRef with a list of Gateways.
type GatewaysByNamespacedRef struct {
	Ref      commonv1alpha1.KonnectNamespacedRef
	Gateways []gwtypes.Gateway
}

// GetNamespacedRefs returns a slice of KonnectNamespacedRef for the given runtime.Object, based on its type.
func GetNamespacedRefs(ctx context.Context, cl client.Client, obj runtime.Object) (map[string]GatewaysByNamespacedRef, error) {
	switch o := obj.(type) {
	// TODO: add other types here
	case *gwtypes.HTTPRoute:
		return byHTTPRoute(ctx, cl, *o)
	default:
		return nil, nil
	}
}

// GetControlPlaneRefByParentRef retrieves the control plane reference for a given parent reference.
func GetControlPlaneRefByParentRef(ctx context.Context, logger logr.Logger, cl client.Client, route *gwtypes.HTTPRoute,
	pRef gwtypes.ParentReference) (*commonv1alpha1.ControlPlaneRef, error) {

	// Validate and get the supported Gateway for the ParentReference.
	gw, err := GetSupportedGatewayForParentRef(ctx, logger, cl, pRef, route.Namespace)
	if err != nil {
		return nil, err
	}

	konnectNamespacedRef, err := byGateway(ctx, cl, *gw)
	if err != nil {
		return nil, fmt.Errorf("unable to get ControlPlaneRef for ParentRef %+v in route %q: %w", pRef, client.ObjectKeyFromObject(route), err)
	}

	// If the KonnectNamespacedRef is nil, it means the Gateway does not reference a Konnect ControlPlane.
	if konnectNamespacedRef == nil {
		return nil, nil
	}

	// Clear the namespace to indicate that the reference is within the same namespace as the Gateway.
	konnectNamespacedRef.Namespace = ""

	return &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: konnectNamespacedRef,
	}, nil
}

// GetControlPlaneRefByGateway retrieves the control plane reference for a given Gateway.
// Returns an error if the Gateway does not reference a ControlPlane.
func GetControlPlaneRefByGateway(ctx context.Context, cl client.Client, gateway *gwtypes.Gateway) (*commonv1alpha1.ControlPlaneRef, error) {
	konnectNamespacedRef, err := byGateway(ctx, cl, *gateway)
	if err != nil {
		return nil, fmt.Errorf("unable to get ControlPlaneRef for Gateway %q: %w", client.ObjectKeyFromObject(gateway), err)
	}

	// If the KonnectNamespacedRef is nil, it means the Gateway does not reference a ControlPlane.
	if konnectNamespacedRef == nil {
		return nil, hybridgatewayerrors.ErrGatewayNotReferencingControlPlane
	}

	return &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: konnectNamespacedRef,
	}, nil
}

// GetListenersByParentRef retrieves the listeners for a given parent reference.
func GetListenersByParentRef(ctx context.Context, cl client.Client, route *gwtypes.HTTPRoute, pRef gwtypes.ParentReference) ([]gwtypes.Listener, error) {
	var namespace string
	if pRef.Group == nil || *pRef.Group != gwtypes.GroupName {
		return nil, nil
	}
	if pRef.Kind == nil || *pRef.Kind != "Gateway" {
		return nil, nil
	}

	if pRef.Namespace == nil || *pRef.Namespace == "" {
		namespace = route.Namespace
	} else {
		namespace = string(*pRef.Namespace)
	}

	gw := &gwtypes.Gateway{}
	err := cl.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      string(pRef.Name),
	}, gw)
	if err != nil {
		return nil, fmt.Errorf("unable to get Listeners for ParentRef %+v in route %q: %w", pRef, client.ObjectKeyFromObject(route), err)
	}

	return gw.Spec.Listeners, nil
}

// byHTTPRoute returns a slice of KonnectNamespacedRef associated with the given HTTPRoute, or an error if retrieval fails.
func byHTTPRoute(ctx context.Context, cl client.Client, httpRoute gwtypes.HTTPRoute) (map[string]GatewaysByNamespacedRef, error) {
	namespacedRefs := map[string]GatewaysByNamespacedRef{}
	gateways := GetGatewaysByHTTPRoute(ctx, cl, httpRoute)
	for _, gw := range gateways {
		ref, err := byGateway(ctx, cl, gw)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			continue
		}
		key := ref.Namespace + "/" + ref.Name
		entry, exists := namespacedRefs[key]
		if exists {
			entry.Gateways = append(entry.Gateways, gw)
			namespacedRefs[key] = entry
			continue
		}
		entry = GatewaysByNamespacedRef{
			Ref:      *ref,
			Gateways: []gwtypes.Gateway{gw},
		}
		namespacedRefs[key] = entry
	}
	return namespacedRefs, nil
}

// byGateway returns the KonnectNamespacedRef associated with the given Gateway, or nil if not found.
func byGateway(ctx context.Context, cl client.Client, gateway gwtypes.Gateway) (*commonv1alpha1.KonnectNamespacedRef, error) {
	extensions, err := gatewayutils.ListKonnectExtensionsForGateway(ctx, cl, &gateway)
	if err != nil {
		return nil, err
	}

	switch l := len(extensions); l {
	case 0:
		return nil, nil
	case 1:
		return byKonnectExtension(ctx, cl, extensions[0])
	default:
		return nil, errors.New("multiple KonnectExtensions found for a single Gateway, which is not supported")
	}
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
		return nil, hybridgatewayerrors.ErrKonnectExtensionCrossNamespaceReference
	}

	konnectGatewayControlPlane := konnectv1alpha2.KonnectGatewayControlPlane{}
	err := cl.Get(ctx, client.ObjectKey{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: ns,
	}, &konnectGatewayControlPlane)
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &commonv1alpha1.KonnectNamespacedRef{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: ns,
	}, nil
}
