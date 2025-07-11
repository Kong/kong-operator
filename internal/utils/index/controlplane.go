package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"

	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"
)

const (
	// DataPlaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values.
	DataPlaneNameIndex = "dataplane"
)

// OptionsForControlPlane returns required Index options for ControlPlane.
func OptionsForControlPlane(konnectControllersEnabled bool) []Option {
	opts := []Option{
		{
			Object:         &gwtypes.ControlPlane{},
			Field:          DataPlaneNameIndex,
			ExtractValueFn: dataPlaneNameOnControlPlane,
		},
	}

	if konnectControllersEnabled {
		opts = append(opts, Option{
			Object:         &gwtypes.ControlPlane{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*gwtypes.ControlPlane](),
		})
	}

	return opts
}

// dataPlaneNameOnControlPlane indexes the ControlPlane .spec.dataplaneName field
// on the "dataplane" key.
func dataPlaneNameOnControlPlane(o client.Object) []string {
	controlPlane, ok := o.(*gwtypes.ControlPlane)
	if !ok {
		return []string{}
	}
	dp := controlPlane.Spec.DataPlane
	switch dp.Type {
	case operatorv2alpha1.ControlPlaneDataPlaneTargetRefType:
		// Note: .Name is a pointer, enforced to be non nil at the CRD level.
		return []string{controlPlane.Spec.DataPlane.Ref.Name}
	// TODO(pmalek): implement DataPlane external URL type
	// ref: https://github.com/kong/kong-operator/issues/1366
	default:
		return []string{}
	}
}
