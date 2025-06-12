package types

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
)

// Aliases below allow to easily switch between different versions of the ControlPlane API
// without changing the rest of the codebase.

type (
	// ControlPlane is an alias for the v2alpha1 ControlPlane type.
	ControlPlane = operatorv2alpha1.ControlPlane

	// ControlPlaneSpec is an alias for the v2alpha1 ControlPlaneSpec type.
	ControlPlaneSpec = operatorv2alpha1.ControlPlaneSpec

	// ControlPlaneOptions is an alias for the v2alpha1 ControlPlaneOptions type.
	ControlPlaneOptions = operatorv2alpha1.ControlPlaneOptions

	// ControlPlaneDataPlaneTarget is an alias for the v2alpha1 ControlPlaneDataPlaneTarget type.
	ControlPlaneDataPlaneTarget = operatorv2alpha1.ControlPlaneDataPlaneTarget

	// ControlPlaneDataPlaneTargetRef is an alias for the v2alpha1 ControlPlaneDataPlaneTargetRef type.
	ControlPlaneDataPlaneTargetRef = operatorv2alpha1.ControlPlaneDataPlaneTargetRef

	// ControlPlaneList is an alias for the v2alpha1 ControlPlaneList type.
	ControlPlaneList = operatorv2alpha1.ControlPlaneList

	// ControlPlaneStatus is an alias for the v2alpha1 ControlPlaneStatus type.
	ControlPlaneStatus = operatorv2alpha1.ControlPlaneStatus
)

const (
	// ControlPlaneDataPlaneTargetRefType is an alias for the v2alpha1 ControlPlaneDataPlaneTargetRefType type.
	ControlPlaneDataPlaneTargetRefType = operatorv2alpha1.ControlPlaneDataPlaneTargetRefType
)

func ControlPlaneGVR() schema.GroupVersionResource {
	return operatorv2alpha1.ControlPlaneGVR()
}
