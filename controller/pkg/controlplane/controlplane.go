package controlplane

import (
	"reflect"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// DefaultsArgs contains the parameters to pass to setControlPlaneDefaults
type DefaultsArgs struct {
	Namespace                   string
	ControlPlaneName            string
	DataPlaneIngressServiceName string
	DataPlaneAdminServiceName   string
	OwnedByGateway              string
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

// SpecDeepEqual returns true if the two ControlPlaneOptions are equal.
func SpecDeepEqual(spec1, spec2 *gwtypes.ControlPlaneOptions) bool {
	return reflect.DeepEqual(spec1, spec2)
}
