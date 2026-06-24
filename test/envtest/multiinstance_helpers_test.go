package envtest

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/multiinstance"
)

func setupMultiInstanceDiagnosticsManager(
	ctx context.Context,
	t *testing.T,
	diagPort int,
	opts ...multiinstance.DiagnosticsServerOption,
) *multiinstance.Manager {
	t.Helper()

	t.Log("Starting the diagnostics server and the multi-instance manager")
	diagServer := multiinstance.NewDiagnosticsServer(diagPort, opts...)
	go func() {
		assert.NoError(t, diagServer.Start(ctx))
	}()

	multimgr := multiinstance.NewManager(testr.New(t), multiinstance.WithDiagnosticsExposer(diagServer))
	go func() {
		assert.NoError(t, multimgr.Start(ctx))
	}()

	return multimgr
}
