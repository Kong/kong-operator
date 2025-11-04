package errors

import (
	"fmt"
)

var (
	// ErrNoGatewayFound is returned when a referenced Gateway does not exist in the cluster.
	ErrNoGatewayFound = fmt.Errorf("no supported gateway found")

	// ErrNoGatewayClassFound is returned when a GatewayClass referenced by a Gateway does not exist in the cluster.
	ErrNoGatewayClassFound = fmt.Errorf("no gatewayClass found for gateway")

	// ErrNoGatewayController is returned when a GatewayClass exists but is not managed by this controller.
	ErrNoGatewayController = fmt.Errorf("gatewayClass is not managed by this controller")

	// ErrKonnectExtensionCrossNamespaceReference is returned when a KonnectExtension references a ControlPlane in a different namespace.
	ErrKonnectExtensionCrossNamespaceReference = fmt.Errorf("cross-namespace references between KonnectExtension and ControlPlane are not supported")
)
