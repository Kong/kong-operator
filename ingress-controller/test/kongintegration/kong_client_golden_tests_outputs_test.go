package kongintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	"github.com/kong/go-database-reconciler/pkg/dump"
	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/ingress-controller/internal/adminapi"
	"github.com/kong/kong-operator/ingress-controller/internal/dataplane/sendconfig"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/ingress-controller/test/helpers/konnect"
	"github.com/kong/kong-operator/ingress-controller/test/kongintegration/containers"
	"github.com/kong/kong-operator/ingress-controller/test/testenv"
)

const (
	timeout = 5 * time.Second
	tick    = 250 * time.Millisecond
)

// TestKongClientGoldenTestsOutputs ensures that the KongClient's golden tests outputs are accepted by Kong.
func TestKongClientGoldenTestsOutputs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// By default, run only non-EE tests.
	goldenTestsOutputsPaths := lo.Filter(allGoldenTestsOutputsPaths(t), func(path string, _ int) bool {
		return !strings.Contains(path, "-ee/") // Skip Enterprise tests.
	})
	// If the Kong Enterprise is enabled, run all tests.
	if testenv.KongEnterpriseEnabled() {
		if testenv.KongLicenseData() == "" {
			t.Skip("Kong Enterprise enabled, but no license data provided")
		}
		goldenTestsOutputsPaths = allGoldenTestsOutputsPaths(t)
	}

	expressionRoutesOutputsPaths := lo.Filter(goldenTestsOutputsPaths, func(path string, _ int) bool {
		return strings.Contains(path, "expression-routes-on_")
	})
	defaultOutputsPaths := lo.Filter(goldenTestsOutputsPaths, func(path string, _ int) bool {
		return strings.Contains(path, "default_")
	})

	t.Logf("will test %d expression routes outputs and %d default ones", len(goldenTestsOutputsPaths), len(defaultOutputsPaths))

	t.Run("expressions router", func(t *testing.T) {
		t.Parallel()

		kongC := containers.NewKong(ctx, t, containers.KongWithRouterFlavor("expressions"))

		kongClient, err := adminapi.NewKongAPIClient(kongC.AdminURL(ctx, t), managercfg.AdminAPIClientConfig{}, "")
		require.NoError(t, err)

		for _, goldenTestOutputPath := range expressionRoutesOutputsPaths {
			t.Run(goldenTestOutputPath, func(t *testing.T) {
				ensureGoldenTestOutputIsAccepted(ctx, t, goldenTestOutputPath, kongClient)
			})
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Parallel()

		kongC := containers.NewKong(ctx, t, containers.KongWithRouterFlavor("traditional"))
		kongClient, err := adminapi.NewKongAPIClient(kongC.AdminURL(ctx, t), managercfg.AdminAPIClientConfig{}, "")
		require.NoError(t, err)

		for _, goldenTestOutputPath := range defaultOutputsPaths {
			t.Run(goldenTestOutputPath, func(t *testing.T) {
				ensureGoldenTestOutputIsAccepted(ctx, t, goldenTestOutputPath, kongClient)
			})
		}
	})
}

// TestKongClientGoldenTestsOutputs ensures that the KongClient's golden tests outputs are accepted by Konnect Control Plane
// Admin API.
func TestKongClientGoldenTestsOutputs_Konnect(t *testing.T) {
	const (
		// Use a longer timeout to account for potential Konnect Control Plane throttling
		// and backoffs in case of too many requests.
		timeout = 90 * time.Second
	)

	konnect.SkipIfMissingRequiredKonnectEnvVariables(t)
	t.Parallel()

	ctx := t.Context()

	gatewayTag, err := testenv.GetDependencyVersion("kongintegration.kong-ee")
	require.NoError(t, err)
	gatewayTag = trimEnterpriseTagToSemver(gatewayTag)

	token := konnect.CreateTestPersonalAccessToken(ctx, t)
	cpID := konnect.CreateTestControlPlane(ctx, t, token)
	cert, key := konnect.CreateClientCertificate(ctx, t, cpID, token)
	adminAPIClient := konnect.CreateKonnectAdminAPIClient(t, cpID, cert, key)
	updateStrategy := sendconfig.NewUpdateStrategyDBModeKonnect(
		adminAPIClient.AdminAPIClient(),
		dump.Config{
			SkipCACerts:         true,
			KonnectControlPlane: cpID,
		},
		semver.MustParse(gatewayTag),
		concurrency,
		logr.Discard(),
	)

	for _, goldenTestOutputPath := range allGoldenTestsOutputsPaths(t) {
		t.Run(goldenTestOutputPath, func(t *testing.T) {
			goldenTestOutput, err := os.ReadFile(goldenTestOutputPath)
			require.NoError(t, err)

			content := &file.Content{}
			require.NoError(t, yaml.Unmarshal(goldenTestOutput, content))

			require.EventuallyWithT(t, func(t *assert.CollectT) {
				configSize, err := updateStrategy.Update(ctx, sendconfig.ContentWithHash{Content: content})
				if !assert.NoError(t, err) {
					var (
						apiErr        = &kong.APIError{}
						sendconfigErr sendconfig.UpdateError
					)
					switch {
					case errors.As(err, &apiErr):
						if apiErr.Code() == http.StatusTooManyRequests {
							details, ok := apiErr.Details().(kong.ErrTooManyRequestsDetails)
							if !ok {
								t.Errorf("failed to extract details from 429 error: %v", err)
								return
							}
							timer := time.NewTimer(details.RetryAfter)
							defer timer.Stop()
							select {
							case <-timer.C:
								t.Errorf("rate limited (429), retrying after %s", details.RetryAfter)
								return
							case <-ctx.Done():
								t.Errorf("context done while waiting to retry after 429: %v", ctx.Err())
								return
							}
						}
					case errors.As(err, &sendconfigErr):
						t.Errorf("sendconfig error: %v", sendconfigErr.Error())
						t.Errorf("sendconfig error failures: %v", sendconfigErr.ResourceFailures())
						return
					}
					return
				}

				assert.Equal(t, mo.None[int](), configSize)
			}, timeout, tick)
		})
	}
}

func ensureGoldenTestOutputIsAccepted(ctx context.Context, t *testing.T, goldenTestOutputPath string, kongClient *kong.Client) {
	goldenTestOutput, err := os.ReadFile(goldenTestOutputPath)
	require.NoError(t, err)

	cfg := map[string]any{}
	err = yaml.Unmarshal(goldenTestOutput, &cfg)
	require.NoError(t, err)

	cfgAsJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := kongClient.ReloadDeclarativeRawConfig(ctx, bytes.NewReader(cfgAsJSON), true, true)
		if !assert.NoErrorf(t, err, "failed to reload declarative config") {
			apiErr := &kong.APIError{}
			if errors.As(err, &apiErr) {
				t.Errorf("Kong Admin API response: %s", apiErr.Raw())
			}
		}
	}, timeout, tick)
}

func allGoldenTestsOutputsPaths(t *testing.T) []string {
	const goldenTestsOutputsGlob = "../../internal/dataplane/testdata/golden/*/*_golden.yaml"
	goldenTestsOutputsPaths, err := filepath.Glob(goldenTestsOutputsGlob)
	require.NoError(t, err)
	require.NotEmpty(t, goldenTestsOutputsPaths, "no golden tests outputs found")
	return goldenTestsOutputsPaths
}
