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

	// ErrUnsupportedKind is returned when a ParentRef references an unsupported resource kind.
	ErrUnsupportedKind = fmt.Errorf("unsupported ParentRef kind, only Gateway is supported")

	// ErrUnsupportedGroup is returned when a ParentRef references an unsupported API group.
	ErrUnsupportedGroup = fmt.Errorf("unsupported ParentRef group, only gateway.networking.k8s.io is supported")

	// ErrKonnectExtensionCrossNamespaceReference is returned when a KonnectExtension references a ControlPlane in a different namespace.
	ErrKonnectExtensionCrossNamespaceReference = fmt.Errorf("cross-namespace references between KonnectExtension and ControlPlane are not supported")

	// ErrGatewayNotReferencingControlPlane is returned when a Gateway does not reference a ControlPlane.
	ErrGatewayNotReferencingControlPlane = fmt.Errorf("gateway does not reference a ControlPlane")
)
