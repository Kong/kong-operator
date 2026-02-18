package route

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// RouteStatusUpdater computes and enforces the status of a route object.
type RouteStatusUpdater interface {
	// ComputeStatus computes the status of the route object.
	ComputeStatus()
	// EnforceStatus enforces the computed status on the route object.
	EnforceStatus(ctx context.Context) (op.Result, error)
}

// RouteObject is a constraint for supported route types.
type RouteObject interface {
	gwtypes.HTTPRoute | gwtypes.GRPCRoute
}

// RouteObjectPtr is a constraint for pointers to supported route types.
type RouteObjectPtr[T RouteObject] interface {
	*T
	client.Object
}

// NewRouteStatusUpdater returns a RouteStatusUpdater for the given route object.
func NewRouteStatusUpdater[t RouteObject](obj t, cl client.Client, logger logr.Logger, sharedStatusMap *SharedRouteStatusMap) (RouteStatusUpdater, error) {
	switch v := any(obj).(type) {
	case gwtypes.HTTPRoute:
		return newHTTPRouteStatusUpdater(v, cl, logger, sharedStatusMap), nil
	default:
		return nil, fmt.Errorf("unsupported route type: %T", v)
	}
}
