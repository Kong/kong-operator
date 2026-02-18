package reference

import (
	"github.com/go-logr/logr"

	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/reference"
)

type CacheIndexers = internal.CacheIndexers

func NewCacheIndexers(logger logr.Logger) CacheIndexers {
	return internal.NewCacheIndexers(logger)
}
