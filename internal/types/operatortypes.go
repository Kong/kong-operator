package types

import (
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
)

type (
	// ControlPlane is an alias for the v2alpha1 ControlPlane type.
	// It allows to easily switch between different versions of the ControlPlane API
	// without changing the rest of the codebase.
	ControlPlane = operatorv2alpha1.ControlPlane

	// ControlPlaneList is an alias for the v2alpha1 ControlPlaneList type.
	// It allows to easily switch between different versions of the ControlPlaneList API
	// without changing the rest of the codebase.
	ControlPlaneList = operatorv2alpha1.ControlPlaneList
)
