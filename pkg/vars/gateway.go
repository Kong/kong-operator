package vars

import (
	"os"
	"sync"
)

// -----------------------------------------------------------------------------
// Gateway - Vars & Consts
// -----------------------------------------------------------------------------

var (
	// _controllerName is a unique identifier which indicates this operator's name.
	// This value may be overwritten by ENV vars or via the manager.
	_controllerName     = DefaultControllerName // TODO: multi-tenancy: https://github.com/kong/kong-operator-archive/issues/35
	_controllerNameLock sync.RWMutex
)

// -----------------------------------------------------------------------------
// Gateway - Environment Variables
// -----------------------------------------------------------------------------

const (
	// DefaultControllerName is the default value of controller name that is
	// used by the operator.
	//
	// This has to match the pattern as specified by the Gateway API GatewayController type.
	// ref: https://github.com/kubernetes-sigs/gateway-api/blob/v0.8.1/apis/v1beta1/shared_types.go#L537-L551
	DefaultControllerName = "konghq.com/gateway-operator"

	// ControllerNameOverrideVar is the ENV var that can be used to override the
	// ControllerName at runtime.
	ControllerNameOverrideVar = "KONG_CONTROLLER_NAME"
)

// ControllerName returns the currently set controller name.
func ControllerName() string {
	_controllerNameLock.RLock()
	defer _controllerNameLock.RUnlock()
	return _controllerName
}

// SetControllerName sets the controller name.
func SetControllerName(name string) {
	_controllerNameLock.Lock()
	defer _controllerNameLock.Unlock()
	_controllerName = name
}

// -----------------------------------------------------------------------------
// Gateway - Private Functions - Env Var Init
// -----------------------------------------------------------------------------

func init() {
	if v := os.Getenv(ControllerNameOverrideVar); v != "" {
		SetControllerName(v)
	}
}
