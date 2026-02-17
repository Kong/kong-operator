package refs

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// GetGatewaysByHTTPRoute returns Gateways referenced by the given HTTPRoute.
func GetGatewaysByHTTPRoute(ctx context.Context, cl client.Client, r gwtypes.HTTPRoute) []gwtypes.Gateway {
	gatewayRefs := []gwtypes.Gateway{}
	for _, ref := range r.Spec.ParentRefs {
		var namespace string
		if ref.Group == nil || *ref.Group != gwtypes.GroupName {
			continue
		}
		if ref.Kind == nil || *ref.Kind != "Gateway" {
			continue
		}
		if ref.Namespace != nil && *ref.Namespace != "" {
			namespace = string(*ref.Namespace)
		} else {
			namespace = r.Namespace
		}
		gw := &gwtypes.Gateway{}
		err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      string(ref.Name),
		}, gw)
		if err != nil {
			continue
		}
		gatewayRefs = append(gatewayRefs, *gw)
	}
	return gatewayRefs
}

// IsGatewayInKonnect checks if the given Gateway is controlled by this controller.
// It returns true if the Gateway's GatewayClass exists and is controlled by this controller,
// false if the GatewayClass is not controlled by this controller.
// Returns an error if there's a problem accessing the GatewayClass.
func IsGatewayInKonnect(ctx context.Context, cl client.Client, gateway *gwtypes.Gateway) (bool, error) {
	_, found, err := byGateway(ctx, cl, *gateway)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to determine if Gateway %q is in Konnect: %w", client.ObjectKeyFromObject(gateway), err)
	}
	if !found {
		return false, nil
	}

	return true, nil
}

// GetSupportedGatewayForParentRef checks if the given ParentRef is supported by this controller
// and returns the associated Gateway if it is supported.
//
// The function validates that:
// - The ParentRef refers to a Gateway kind (not other resource types).
// - The ParentRef uses the gateway.networking.k8s.io group (or defaults to it).
// - The referenced Gateway exists in the cluster.
// - The Gateway's GatewayClass exists and is controlled by this controller.
//
// Parameters:
//   - ctx: The context for API calls.
//   - logger: Logger for debugging information.
//   - cl: The Kubernetes client for API operations.
//   - pRef: The ParentRef to validate.
//   - routeNamespace: The namespace of the route (used as default if ParentRef namespace is unspecified).
//
// Returns:
//   - *gwtypes.Gateway: The Gateway object if the ParentRef is supported, nil if not supported.
//   - error: Specific error indicating why the ParentRef is not supported, or nil if validation passes.
//
// The function returns specific errors to help callers understand why a ParentRef is not supported:
// - hybridgatewayerrors.ErrUnsupportedKind: The ParentRef references a non-Gateway resource kind.
// - hybridgatewayerrors.ErrUnsupportedGroup: The ParentRef references an unsupported API group.
// - hybridgatewayerrors.ErrNoGatewayFound: The referenced Gateway doesn't exist.
// - hybridgatewayerrors.ErrNoGatewayClassFound: The Gateway's GatewayClass doesn't exist.
// - hybridgatewayerrors.ErrNoGatewayController: The GatewayClass is not controlled by this controller.
func GetSupportedGatewayForParentRef(ctx context.Context, logger logr.Logger, cl client.Client, pRef gwtypes.ParentReference,
	routeNamespace string) (*gwtypes.Gateway, bool, error) {
	// Only support Gateway kind.
	if pRef.Kind != nil && *pRef.Kind != "Gateway" {
		log.Debug(logger, "Ignoring ParentRef, unsupported kind", "pRef", pRef, "kind", *pRef.Kind)
		return nil, false, hybridgatewayerrors.ErrUnsupportedKind
	}

	// Only support gateway.networking.k8s.io group (or empty group which defaults to this).
	if pRef.Group != nil && *pRef.Group != gwtypes.GroupName {
		log.Debug(logger, "Ignoring ParentRef, unsupported group", "pRef", pRef, "group", *pRef.Group)
		return nil, false, hybridgatewayerrors.ErrUnsupportedGroup
	}

	// Determine the namespace - use route's namespace if not specified.
	namespace := routeNamespace
	if pRef.Namespace != nil {
		namespace = string(*pRef.Namespace)
	}

	// Get the Gateway object.
	gateway := gwtypes.Gateway{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: string(pRef.Name)}, &gateway); err != nil {
		if apierrors.IsNotFound(err) {
			// Gateway doesn't exist, not supported.
			return nil, false, hybridgatewayerrors.ErrNoGatewayFound
		}
		return nil, false, fmt.Errorf("failed to get gateway for ParentRef %v: %w", pRef, err)
	}

	_, found, err := byGateway(ctx, cl, gateway)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to determine if Gateway %q is in Konnect: %w", client.ObjectKeyFromObject(&gateway), err)
	}
	if !found {
		return nil, false, nil
	}

	// All checks passed, this ParentRef is supported.
	return &gateway, true, nil
}
