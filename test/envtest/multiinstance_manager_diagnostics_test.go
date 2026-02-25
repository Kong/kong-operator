package envtest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"testing"
	"time"

	"github.com/google/pprof/profile"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/multiinstance"
	"github.com/kong/kong-operator/v2/ingress-controller/test/helpers"
	"github.com/kong/kong-operator/v2/pkg/clientset/scheme"
)

func TestMultiInstanceManagerDiagnostics(t *testing.T) {
	t.Parallel()

	const (
		waitTime = 10 * time.Second
		tickTime = 10 * time.Millisecond
	)
	ctx := t.Context()

	envcfg, _ := Setup(t, ctx, scheme.Scheme)
	diagPort := helpers.GetFreePort(t)
	healthPort1 := helpers.GetFreePort(t)
	healthPort2 := helpers.GetFreePort(t)

	multimgr := setupMultiInstanceDiagnosticsManager(ctx, t, diagPort, multiinstance.WithPprofHandler())

	t.Log("Setting up two instances of the manager and scheduling them in the multi-instance manager")
	mgrInstance1 := SetupManager(ctx, t, manager.NewRandomID(), envcfg, AdminAPIOptFns(), WithDiagnosticsWithoutServer(), WithHealthProbePort(healthPort1))
	mgrInstance2 := SetupManager(ctx, t, manager.NewRandomID(), envcfg, AdminAPIOptFns(), WithDiagnosticsWithoutServer(), WithHealthProbePort(healthPort2))
	require.NoError(t, multimgr.ScheduleInstance(mgrInstance1))
	require.NoError(t, multimgr.ScheduleInstance(mgrInstance2))

	t.Log("Waiting for diagnostics and health endpoints to become available")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/%s/debug/config/successful", diagPort, mgrInstance1.ID()))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/%s/debug/config/failed", diagPort, mgrInstance1.ID()))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/%s/debug/config/successful", diagPort, mgrInstance2.ID()))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/%s/debug/config/failed", diagPort, mgrInstance2.ID()))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/debug/pprof/", diagPort))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/healthz", healthPort1))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/readyz", healthPort1))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/healthz", healthPort2))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}

		resp, err = http.Get(fmt.Sprintf("http://localhost:%d/readyz", healthPort2))
		if assert.NoError(t, err) {
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}
	}, waitTime, tickTime, "diagnostics and health endpoints should be exposed for both instances")

	t.Log("Stopping the first instance and waiting for its diagnostics endpoints to be removed from the server")
	require.NoError(t, multimgr.StopInstance(mgrInstance1.ID()))
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/%s/debug/config/successful", diagPort, mgrInstance1.ID()))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	}, waitTime, tickTime, "diagnostics should no longer be available after stopping the instance")
}

func TestMultiInstanceManager_Profiling(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	envcfg, _ := Setup(t, ctx, scheme.Scheme)
	diagPort := helpers.GetFreePort(t)
	t.Logf("Diagnostics port: %d", diagPort)

	multimgr := setupMultiInstanceDiagnosticsManager(ctx, t, diagPort, multiinstance.WithPprofHandler())

	m1 := SetupManager(ctx, t, lo.Must(manager.NewID("cp-1")), envcfg, AdminAPIOptFns(), WithDiagnosticsWithoutServer())
	m2 := SetupManager(ctx, t, lo.Must(manager.NewID("cp-2")), envcfg, AdminAPIOptFns(), WithDiagnosticsWithoutServer())

	require.NoError(t, multimgr.ScheduleInstance(m1))
	require.NoError(t, multimgr.ScheduleInstance(m2))

	const profilingDuration = 2 * time.Second

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		t.Logf("Profiling CPU usage for %s", profilingDuration)
		url := fmt.Sprintf("http://localhost:%d/debug/pprof/profile?seconds=%d", diagPort, int(profilingDuration.Seconds()))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		require.NoError(c, err, "failed to create profile request")
		profileResp, err := http.DefaultClient.Do(req)
		require.NoError(c, err, "failed to get profile")
		defer profileResp.Body.Close()

		var buff bytes.Buffer
		body := io.TeeReader(profileResp.Body, &buff)
		p, err := profile.Parse(body)
		if !assert.NoError(c, err, "failed to parse profile") {
			profileResp.Body = io.NopCloser(&buff)
			respDump, err := httputil.DumpResponse(profileResp, true)
			require.NoError(c, err, "failed to dump response")
			t.Logf("Profile response dump:\n%s", respDump)
			return
		}

		requireProfileHasInstanceIDLabelSamples := func(
			t require.TestingT, p *profile.Profile, expectedInstanceID manager.ID,
		) {
			samples := lo.Filter(p.Sample, func(s *profile.Sample, _ int) bool {
				return s.HasLabel("instanceID", expectedInstanceID.String())
			})
			require.NotEmpty(t, samples, "profile does not contain samples with instanceID label %q", expectedInstanceID)
		}
		requireProfileHasInstanceIDLabelSamples(c, p, m1.ID())
		requireProfileHasInstanceIDLabelSamples(c, p, m2.ID())
	}, 10*time.Second, 100*time.Millisecond)
}
