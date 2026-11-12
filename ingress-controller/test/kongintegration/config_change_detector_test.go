package kongintegration

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/kongintegration/containers"
)

// TestKongGatewayConfigurationChangeDetector_DetectsDrift verifies against a real
// Kong Gateway that a matching reported hash skips the push, while an out-of-band
// configuration change forces one.
func TestKongGatewayConfigurationChangeDetector_DetectsDrift(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	kongC := containers.NewKong(ctx, t)
	kongClient, err := adminapi.NewKongAPIClient(kongC.AdminURL(ctx, t), managercfg.AdminAPIClientConfig{}, "")
	require.NoError(t, err)

	logbase, err := zap.NewDevelopment()
	require.NoError(t, err)
	detector := dataplanepush.NewKongGatewayConfigurationChangeDetector(zapr.NewLogger(logbase))

	pushConfig := func(t *testing.T, body []byte) {
		t.Helper()
		err := kongClient.ReloadDeclarativeRawConfig(ctx, bytes.NewReader(body), true, true)
		if apiErr, ok := errors.AsType[*kong.APIError](err); ok {
			t.Fatalf("failed pushing config: %v: %s", err, apiErr.Raw())
		}
		require.NoError(t, err)
	}

	config := []byte(`{"_format_version":"3.0","services":[{"name":"service-a","url":"http://example.com"}]}`)
	pushConfig(t, config)

	status, err := kongClient.Status(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, status.ConfigurationHash)
	lastPushedHash := status.ConfigurationHash

	// The desired configuration did not change and Kong reports the hash observed
	// after the last successful push.
	sameSHA := []byte("same-desired-configuration")
	changed, err := detector.HasConfigurationChanged(ctx, sameSHA, sameSHA, false, lastPushedHash, true, kongClient)
	require.NoError(t, err)
	require.False(t, changed)

	// An out-of-band configuration change makes Kong report another non-empty hash.
	pushConfig(t, []byte(`{"_format_version":"3.0","services":[{"name":"service-b","url":"http://example.com"}]}`))
	changed, err = detector.HasConfigurationChanged(ctx, sameSHA, sameSHA, false, lastPushedHash, true, kongClient)
	require.NoError(t, err)
	require.True(t, changed, "out-of-band configuration drift must force a push")

	// Kong re-stamps created_at on every push, so the hash never reverts to
	// lastPushedHash even when the same content is pushed again. Recording the
	// newly reported hash (as the operator does after its own push) stops the
	// detector from reporting drift.
	status, err = kongClient.Status(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, status.ConfigurationHash)
	changed, err = detector.HasConfigurationChanged(ctx, sameSHA, sameSHA, false, status.ConfigurationHash, true, kongClient)
	require.NoError(t, err)
	require.False(t, changed, "a recorded hash that matches the reported hash must not force a push")
}
