package gatewayclass

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/pkg/vars"
)

// Get returns a decorated GatewayClass object for the provided GatewayClass name. If the GatewayClass is
// not found or is not supported, an `ErrUnsupportedGatewayClass` error is returned with a descriptive message.
func Get(ctx context.Context, cl client.Client, gatewayClassName string) (*Decorator, error) {
	if gatewayClassName == "" {
		return nil, operatorerrors.NewErrUnsupportedGateway("no GatewayClassName provided")
	}

	gwc := NewDecorator()
	if err := cl.Get(ctx, client.ObjectKey{Name: gatewayClassName}, gwc.GatewayClass); err != nil {
		return nil, fmt.Errorf("error while fetching GatewayClass %q: %w", gatewayClassName, err)
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName() {
		return nil, operatorerrors.NewErrUnsupportedGateway(fmt.Sprintf(
			"GatewayClass %q with %q ControllerName does not match expected %q",
			gatewayClassName,
			gwc.Spec.ControllerName,
			vars.ControllerName(),
		))
	}

	return gwc, nil
}
