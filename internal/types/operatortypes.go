package types

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
)

// Aliases below allow to easily switch between different versions of the ControlPlane API
// without changing the rest of the codebase.

type (
	// ControlPlane is an alias for the v2beta1 ControlPlane type.
	ControlPlane = operatorv2beta1.ControlPlane

	// ControlPlaneSpec is an alias for the v2beta1 ControlPlaneSpec type.
	ControlPlaneSpec = operatorv2beta1.ControlPlaneSpec

	// ControlPlaneOptions is an alias for the v2beta1 ControlPlaneOptions type.
	ControlPlaneOptions = operatorv2beta1.ControlPlaneOptions

	// ControlPlaneDataPlaneTarget is an alias for the v2beta1 ControlPlaneDataPlaneTarget type.
	ControlPlaneDataPlaneTarget = operatorv2beta1.ControlPlaneDataPlaneTarget

	// ControlPlaneDataPlaneTargetRef is an alias for the v2beta1 ControlPlaneDataPlaneTargetRef type.
	ControlPlaneDataPlaneTargetRef = operatorv2beta1.ControlPlaneDataPlaneTargetRef

	// ControlPlaneList is an alias for the v2beta1 ControlPlaneList type.
	ControlPlaneList = operatorv2beta1.ControlPlaneList

	// ControlPlaneStatus is an alias for the v2beta1 ControlPlaneStatus type.
	ControlPlaneStatus = operatorv2beta1.ControlPlaneStatus

	// ControlPlaneFeatureGate is an alias for the v2beta1 ControlPlaneFeatureGate type.
	ControlPlaneFeatureGate = operatorv2beta1.ControlPlaneFeatureGate

	// ControlPlaneController is an alias for the v2beta1 ControlPlaneController type.
	ControlPlaneController = operatorv2beta1.ControlPlaneController

	// ControllerState is an alias for the v2beta1 ControllerState type.
	ControllerState = operatorv2beta1.ControllerState
)

type (
	// GatewayConfiguration is an alias for the v2beta1 GatewayConfiguration type.
	GatewayConfiguration = operatorv2beta1.GatewayConfiguration
	// GatewayConfigDataPlaneOptions is an alias for the v2beta1 GatewayConfigDataPlaneOptions type.
	GatewayConfigDataPlaneOptions = operatorv2beta1.GatewayConfigDataPlaneOptions
)

const (
	// ControlPlaneDataPlaneTargetRefType is an alias for the v2beta1 ControlPlaneDataPlaneTargetRefType type.
	ControlPlaneDataPlaneTargetRefType = operatorv2beta1.ControlPlaneDataPlaneTargetRefType

	// FeatureGateStateEnabled is an alias for the v2beta1 FeatureGateStateEnabled type.
	FeatureGateStateEnabled = operatorv2beta1.FeatureGateStateEnabled
	// FeatureGateStateDisabled is an alias for the v2beta1 FeatureGateStateDisabled type.
	FeatureGateStateDisabled = operatorv2beta1.FeatureGateStateDisabled

	// ControlPlaneControllerStateEnabled is an alias for the v2beta1 ControlPlaneControllerStateEnabled type.
	ControlPlaneControllerStateEnabled = operatorv2beta1.ControllerStateEnabled
	// ControlPlaneControllerStateDisabled is an alias for the v2beta1 ControlPlaneControllerStateDisabled type.
	ControlPlaneControllerStateDisabled = operatorv2beta1.ControllerStateDisabled
)

func ControlPlaneGVR() schema.GroupVersionResource {
	return operatorv2beta1.ControlPlaneGVR()
}

func GatewayConfigurationGVR() schema.GroupVersionResource {
	return operatorv2alpha1.GatewayConfigurationGVR()
}
