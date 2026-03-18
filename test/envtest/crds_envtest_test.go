package envtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest/observer"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
)

// TestGatewayAPIControllersMayBeDynamicallyStarted ensures that in case of missing CRDs installation in the
// cluster, specific controllers are not started until the CRDs are installed.
func TestGatewayAPIControllersMayBeDynamicallyStarted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheme := Scheme(t, WithKong)
	envcfg, _ := Setup(t, ctx, scheme, WithInstallGatewayCRDs(false))
	loggerHook := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithGatewayFeatureEnabled,
		WithGatewayAPIControllers(),
		WithPublishService("ns"),
	)

	controllers := []string{
		"Gateway",
		"HTTPRoute",
		"ReferenceGrant",
		"UDPRoute",
		"TCPRoute",
		"TLSRoute",
		"GRPCRoute",
	}

	requireLogForAllControllers := func(expectedLog string) {
		require.Eventually(t, func() bool {
			for _, controller := range controllers {
				if !lo.ContainsBy(loggerHook.All(), func(entry observer.LoggedEntry) bool {
					return strings.Contains(entry.LoggerName, controller) && strings.Contains(entry.Message, expectedLog)
				}) {
					t.Logf("expected log %q not found for %s controller", expectedLog, controller)
					return false
				}
			}
			return true
		}, 30*time.Second, time.Millisecond*500)
	}

	const (
		expectedLogOnStartup      = "Required CustomResourceDefinitions are not installed, setting up a watch for them in case they are installed afterward"
		expectedLogOnCRDInstalled = "All required CustomResourceDefinitions are installed, setting up the controller"
	)

	t.Log("waiting for all controllers to not start due to missing CRDs")
	requireLogForAllControllers(expectedLogOnStartup)

	t.Log("installing missing CRDs")
	installGatewayCRDs(t, scheme, envcfg)

	t.Log("waiting for all controllers to start after CRDs installation")
	requireLogForAllControllers(expectedLogOnCRDInstalled)
}

// TestNoKongCRDsInstalledIsFatal ensures that in case of missing Kong CRDs installation, the manager Run() eventually
// returns an error due to cache synchronisation timeout.
func TestNoKongCRDsInstalledIsFatal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheme := Scheme(t)
	envcfg, _ := Setup(t, ctx, scheme, WithInstallKongCRDs(false))
	ctx, logger, _ := CreateTestLogger(ctx)
	adminAPIServerURL := StartAdminAPIServerMock(t).URL

	cfg := managercfg.NewConfig(
		WithDefaultEnvTestsConfig(envcfg),
		WithKongAdminURLs(adminAPIServerURL),
		// Reducing the cache sync timeout to speed up the test.
		WithCacheSyncTimeout(500*time.Millisecond),
	)

	id, err := manager.NewID(t.Name())
	require.NoError(t, err)

	m, err := manager.NewManager(ctx, id, logger, cfg)
	require.NoError(t, err)

	require.ErrorContains(t, m.Run(ctx), "timed out waiting for cache to be synced")
}
