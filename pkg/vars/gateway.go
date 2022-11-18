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
	_controllerName     = "konghq.com/gateway-operator" // TODO: multi-tenancy: https://github.com/Kong/gateway-operator/issues/35
	_controllerNameLock sync.RWMutex
)

// -----------------------------------------------------------------------------
// Gateway - Environment Variables
// -----------------------------------------------------------------------------

const (
	// ControllerNameOverrideVar is the ENV var that can be used to override the
	// ControllerName at runtime.
	ControllerNameOverrideVar = "KONG_CONTROLLER_NAME"
)

func ControllerName() string {
	_controllerNameLock.RLock()
	defer _controllerNameLock.RUnlock()
	return _controllerName
}

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
