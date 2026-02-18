package controlplane

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

type (
	// ControlPlane is an alias for the internally used ControlPlane version type.
	// This is to make it easier to refactor code.
	// We can potentially get rid of this at some point when we change all
	// references to the ControlPlane type to use the internal/types.ControlPlane type.
	ControlPlane = gwtypes.ControlPlane
)
