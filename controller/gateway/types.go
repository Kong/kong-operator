package gateway

import (
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Aliases below allow to easily switch between different versions of the GatewayConfiguration API.
// This is to make it easier to refactor code.
// We can potentially get rid of this at some point when we change all
// references to the GatewayConfiguration type to use the internal/types.GatewayConfiguration type.

type (

	// GatewayConfiguration is an alias for the internally used GatewayConfiguration version type.
	GatewayConfiguration = gwtypes.GatewayConfiguration

	// GatewayConfigDataPlaneOptions is an alias for the internally used GatewayConfigDataPlaneOptions version type.
	GatewayConfigDataPlaneOptions = gwtypes.GatewayConfigDataPlaneOptions
)
