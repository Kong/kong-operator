package health

import (
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	internal "github.com/kong/kong-operator/ingress-controller/internal/health"
)

type CheckServer = internal.CheckServer

func NewHealthCheckerFromFunc(check func() error) healthz.Checker {
	return internal.NewHealthCheckerFromFunc(check)
}

func NewHealthCheckServer(healthzCheck, readyzChecker healthz.Checker) *CheckServer {
	return internal.NewHealthCheckServer(healthzCheck, readyzChecker)
}
