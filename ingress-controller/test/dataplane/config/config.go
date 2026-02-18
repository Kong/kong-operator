package config

import internal "github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/config"

type (
	DBMode       = internal.DBMode
	RouterFlavor = internal.RouterFlavor
)

const (
	DBModeOff      = internal.DBModeOff
	DBModePostgres = internal.DBModePostgres

	RouterFlavorTraditional           = internal.RouterFlavorTraditional
	RouterFlavorTraditionalCompatible = internal.RouterFlavorTraditionalCompatible
	RouterFlavorExpressions           = internal.RouterFlavorExpressions
)

func NewDBMode(mode string) (DBMode, error) {
	return internal.NewDBMode(mode)
}

func ShouldEnableExpressionRoutes(rf RouterFlavor) bool {
	return internal.ShouldEnableExpressionRoutes(rf)
}
