package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

const (
	// DataPlaneNameIndex is the key to be used to access the .spec.dataplaneName indexed values.
	DataPlaneNameIndex = "dataplane"
)

// OptionsForControlPlane returns required Index options for ControlPlane.
func OptionsForControlPlane(konnectControllersEnabled bool) []Option {
	opts := []Option{
		{
			Object:         &operatorv1beta1.ControlPlane{},
			Field:          DataPlaneNameIndex,
			ExtractValueFn: dataPlaneNameOnControlPlane,
		},
	}

	if konnectControllersEnabled {
		opts = append(opts, Option{

			Object:         &operatorv1beta1.ControlPlane{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*operatorv1beta1.ControlPlane](),
		})
	}

	return opts
}

// dataPlaneNameOnControlPlane indexes the ControlPlane .spec.dataplaneName field
// on the "dataplane" key.
func dataPlaneNameOnControlPlane(o client.Object) []string {
	controlPlane, ok := o.(*operatorv1beta1.ControlPlane)
	if !ok {
		return []string{}
	}
	if controlPlane.Spec.DataPlane != nil {
		return []string{*controlPlane.Spec.DataPlane}
	}
	return []string{}
}
