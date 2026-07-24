package health

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/health"
)

type CheckServer = internal.CheckServer

func NewHealthCheckerFromFunc(check func() error) healthz.Checker {
	return internal.NewHealthCheckerFromFunc(check)
}

func NewHealthCheckServer(healthzCheck, readyzChecker healthz.Checker, logger logr.Logger) *CheckServer {
	return internal.NewHealthCheckServer(healthzCheck, readyzChecker, logger)
}
