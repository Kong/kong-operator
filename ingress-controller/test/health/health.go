package health

import (
	internal "github.com/kong/kong-operator/ingress-controller/internal/health"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

type CheckServer = internal.CheckServer

func NewHealthCheckerFromFunc(check func() error) healthz.Checker {
	return internal.NewHealthCheckerFromFunc(check)
}

func NewHealthCheckServer(healthzCheck, readyzChecker healthz.Checker) *CheckServer {
	return internal.NewHealthCheckServer(healthzCheck, readyzChecker)
}
