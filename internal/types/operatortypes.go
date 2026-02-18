package types

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
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

	// ControlPlaneDataPlaneStatus is an alias for the v2beta1 ControlPlaneDataPlaneStatus type.
	ControlPlaneDataPlaneStatus = operatorv2beta1.ControlPlaneDataPlaneStatus

	// ControlPlaneFeatureGate is an alias for the v2beta1 ControlPlaneFeatureGate type.
	ControlPlaneFeatureGate = operatorv2beta1.ControlPlaneFeatureGate

	// ControlPlaneController is an alias for the v2beta1 ControlPlaneController type.
	ControlPlaneController = operatorv2beta1.ControlPlaneController

	// ControllerState is an alias for the v2alpha1 ControllerState type.
	ControllerState = operatorv2beta1.ControllerState

	// ControlPlaneDataPlaneSync is an alias for the v2alpha1 ControlPlaneDataPlaneSync type.
	ControlPlaneDataPlaneSync = operatorv2beta1.ControlPlaneDataPlaneSync

	// ControlPlaneTranslationOptions is an alias for the v2alpha1 ControlPlaneTranslationOptions type.
	ControlPlaneTranslationOptions = operatorv2beta1.ControlPlaneTranslationOptions

	// ControlPlaneFallbackConfiguration is an alias for the v2alpha1 ControlPlaneFallbackConfiguration type.
	ControlPlaneFallbackConfiguration = operatorv2beta1.ControlPlaneFallbackConfiguration

	// ControlPlaneReverseSyncState is an alias for the v2alpha1 ControlPlaneReverseSyncState type.
	ControlPlaneReverseSyncState = operatorv2beta1.ControlPlaneReverseSyncState
)

type (
	// GatewayConfiguration is an alias for the v2beta1 GatewayConfiguration type.
	GatewayConfiguration = operatorv2beta1.GatewayConfiguration
	// GatewayConfigurationSpec is an alias for the v2beta1 GatewayConfigurationSpec type.
	GatewayConfigurationSpec = operatorv2beta1.GatewayConfigurationSpec

	// GatewayConfigDataPlaneOptions is an alias for the v2beta1 GatewayConfigDataPlaneOptions type.
	GatewayConfigDataPlaneOptions = operatorv2beta1.GatewayConfigDataPlaneOptions
)

const (
	// ControlPlaneDataPlaneTargetRefType is an alias for the v2beta1 ControlPlaneDataPlaneTargetRefType type.
	ControlPlaneDataPlaneTargetRefType = operatorv2beta1.ControlPlaneDataPlaneTargetRefType
	// ControlPlaneDataPlaneTargetManagedByType is an alias for the v2beta1 ControlPlaneDataPlaneTargetManagedByType type.
	ControlPlaneDataPlaneTargetManagedByType = operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType

	// FeatureGateStateEnabled is an alias for the v2beta1 FeatureGateStateEnabled type.
	FeatureGateStateEnabled = operatorv2beta1.FeatureGateStateEnabled
	// FeatureGateStateDisabled is an alias for the v2beta1 FeatureGateStateDisabled type.
	FeatureGateStateDisabled = operatorv2beta1.FeatureGateStateDisabled

	// ControlPlaneControllerStateEnabled is an alias for the v2alpha1 ControlPlaneControllerStateEnabled type.
	ControlPlaneControllerStateEnabled = operatorv2beta1.ControllerStateEnabled
	// ControlPlaneControllerStateDisabled is an alias for the v2alpha1 ControlPlaneControllerStateDisabled type.
	ControlPlaneControllerStateDisabled = operatorv2beta1.ControllerStateDisabled

	// ControlPlaneFallbackConfigurationStateEnabled is an alias for the v2alpha1 ControlPlaneFallbackConfigurationStateEnabled type.
	ControlPlaneFallbackConfigurationStateEnabled = operatorv2beta1.ControlPlaneFallbackConfigurationStateEnabled
	// ControlPlaneFallbackConfigurationStateDisabled is an alias for the v2alpha1 ControlPlaneFallbackConfigurationStateDisabled type.
	ControlPlaneFallbackConfigurationStateDisabled = operatorv2beta1.ControlPlaneFallbackConfigurationStateDisabled

	// ControlPlaneReverseSyncStateEnabled is an alias for the v2alpha1 ControlPlaneReverseSyncStateEnabled type.
	ControlPlaneReverseSyncStateEnabled = operatorv2beta1.ControlPlaneReverseSyncStateEnabled
	// ControlPlaneReverseSyncStateDisabled is an alias for the v2alpha1 ControlPlaneReverseSyncStateDisabled type.
	ControlPlaneReverseSyncStateDisabled = operatorv2beta1.ControlPlaneReverseSyncStateDisabled

	// ControlPlaneDrainSupportStateEnabled is an alias for the v2alpha1 ControlPlaneDrainSupportStateEnabled type.
	ControlPlaneDrainSupportStateEnabled = operatorv2beta1.ControlPlaneDrainSupportStateEnabled
	// ControlPlaneDrainSupportStateDisabled is an alias for the v2alpha1 ControlPlaneDrainSupportStateDisabled type.
	ControlPlaneDrainSupportStateDisabled = operatorv2beta1.ControlPlaneDrainSupportStateDisabled

	// ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled is an alias for the v2alpha1 ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled type.
	ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled = operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled
	// ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled is an alias for the v2alpha1 ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled type.
	ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled = operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled
)

// ControlPlaneGVR is an alias for the current ControlPlane GVR.
func ControlPlaneGVR() schema.GroupVersionResource {
	return operatorv2beta1.ControlPlaneGVR()
}

// GatewayConfigurationGVR is an alias for the current GatewayConfiguration GVR.
func GatewayConfigurationGVR() schema.GroupVersionResource {
	return operatorv2beta1.GatewayConfigurationGVR()
}
