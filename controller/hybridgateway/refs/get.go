package refs

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hybridgatewayerrors "github.com/kong/kong-operator/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

// GetGatewaysByHTTPRoute returns Gateways referenced by the given HTTPRoute.
func GetGatewaysByHTTPRoute(ctx context.Context, cl client.Client, r gwtypes.HTTPRoute) []gwtypes.Gateway {
	gatewayRefs := []gwtypes.Gateway{}
	for _, ref := range r.Spec.ParentRefs {
		var namespace string
		if ref.Group == nil || *ref.Group != "gateway.networking.k8s.io" {
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
// - hybridgatewayerrors.ErrNoGatewayFound: The referenced Gateway doesn't exist.
// - hybridgatewayerrors.ErrNoGatewayClassFound: The Gateway's GatewayClass doesn't exist.
// - hybridgatewayerrors.ErrNoGatewayController: The GatewayClass is not controlled by this controller.
// - nil error with nil Gateway: The ParentRef is valid but not supported (wrong kind/group).
func GetSupportedGatewayForParentRef(ctx context.Context, logger logr.Logger, cl client.Client, pRef gwtypes.ParentReference,
	routeNamespace string) (*gwtypes.Gateway, error) {
	// Only support Gateway kind.
	if pRef.Kind != nil && *pRef.Kind != "Gateway" {
		log.Debug(logger, "Ignoring ParentRef, unsupported kind", "pRef", pRef, "kind", *pRef.Kind)
		return nil, nil
	}

	// Only support gateway.networking.k8s.io group (or empty group which defaults to this).
	if pRef.Group != nil && *pRef.Group != "gateway.networking.k8s.io" {
		log.Debug(logger, "Ignoring ParentRef, unsupported group", "pRef", pRef, "group", *pRef.Group)
		return nil, nil
	}

	// Determine the namespace - use route's namespace if not specified.
	namespace := routeNamespace
	if pRef.Namespace != nil {
		namespace = string(*pRef.Namespace)
	}

	// Get the Gateway object.
	gateway := gwtypes.Gateway{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: string(pRef.Name)}, &gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			// Gateway doesn't exist, not supported.
			return nil, hybridgatewayerrors.ErrNoGatewayFound
		}
		return nil, fmt.Errorf("failed to get gateway for ParentRef %v: %w", pRef, err)
	}

	// Check if the gatewayClass exists.
	gatewayClass := gwtypes.GatewayClass{}
	if err := cl.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &gatewayClass); err != nil {
		if k8serrors.IsNotFound(err) {
			// GatewayClass doesn't exist, not supported.
			return nil, hybridgatewayerrors.ErrNoGatewayClassFound
		}
		return nil, fmt.Errorf("failed to get gatewayClass for ParentRef %v: %w", pRef, err)
	}

	// Check if the gatewayClass is controlled by us.
	// If not, we just ignore it and return nil.
	if string(gatewayClass.Spec.ControllerName) != vars.ControllerName() {
		return nil, hybridgatewayerrors.ErrNoGatewayController
	}

	// All checks passed, this ParentRef is supported.
	return &gateway, nil
}
