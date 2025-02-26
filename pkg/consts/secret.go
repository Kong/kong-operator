package consts

// -----------------------------------------------------------------------------
// Consts - Secret Generic Parameters
// -----------------------------------------------------------------------------

const (
	// DataPlanePrefix is used as a name prefix to generate secret-owned objects' name
	SecretPrefix = "secret"
)

// -----------------------------------------------------------------------------
// Consts - Secret Labels
// -----------------------------------------------------------------------------

const (
	// DataPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the dataplane controller.
	SecretManagedLabelValue = "secret"
)
