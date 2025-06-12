package controlplane

import (
	"reflect"

	gwtypes "github.com/kong/gateway-operator/internal/types"
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
func SpecDeepEqual(spec1, spec2 *gwtypes.ControlPlaneOptions, envVarsToIgnore ...string) bool {
	if !reflect.DeepEqual(spec1.DataPlane, spec2.DataPlane) {
		return false
	}

	if !reflect.DeepEqual(spec1.Extensions, spec2.Extensions) {
		return false
	}

	if !reflect.DeepEqual(spec1.WatchNamespaces, spec2.WatchNamespaces) {
		return false
	}

	return true
}
