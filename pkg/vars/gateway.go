package vars

import "os"

// -----------------------------------------------------------------------------
// Gateway - Vars & Consts
// -----------------------------------------------------------------------------

var (
	// ControllerName is a unique identifier which indicates this operator's name.
	// This value may be overwritten by ENV vars or via the manager.
	ControllerName = "konghq.com/gateway-operator" // TODO: multi-tenancy: https://github.com/Kong/gateway-operator/issues/35
)

// -----------------------------------------------------------------------------
// Gateway - Environment Variables
// -----------------------------------------------------------------------------

const (
	// ControllerNameOverrideVar is the ENV var that can be used to override the
	// ControllerName at runtime.
	ControllerNameOverrideVar = "KONG_CONTROLLER_NAME"
)

// -----------------------------------------------------------------------------
// Gateway - Private Functions - Env Var Init
// -----------------------------------------------------------------------------

func init() {
	if v := os.Getenv(ControllerNameOverrideVar); v != "" {
		ControllerName = v
	}
}
