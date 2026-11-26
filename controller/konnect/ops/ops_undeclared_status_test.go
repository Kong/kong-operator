package ops

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

// handlerRoundTripper serves requests with an in-process handler.
type handlerRoundTripper struct {
	handler http.Handler
}

func (rt handlerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.handler.ServeHTTP(rec, r)
	res := rec.Result()
	res.Request = r
	return res, nil
}

// newSDKWithStatus returns an SDK whose requests all get the given status and
// a problem+json body with a new instance for each request.
func newSDKWithStatus(t *testing.T, status int) (sdkops.SDKWrapper, *atomic.Int64) {
	t.Helper()

	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := requests.Add(1)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w,
			`{"status":%d,"title":"Not Found","instance":"konnect:trace:%032x","detail":"not found"}`,
			status, n,
		)
	})

	srv, err := server.NewServer[struct{}]("us.api.konghq.com")
	require.NoError(t, err)
	factory := sdkops.NewSDKFactory(sdkops.WithHTTPClient(&http.Client{
		Transport: handlerRoundTripper{handler: handler},
	}))
	sdk, err := factory.NewKonnectSDK(srv, "kpat_test")
	require.NoError(t, err)
	return sdk, &requests
}

func TestUpdateProgrammedConditionMessageIsStableOnUndeclaredStatus(t *testing.T) {
	t.Parallel()

	sdk, requests := newSDKWithStatus(t, http.StatusNotFound)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
	ent := testGeneratedAIGatewayAuthStrategyForSDKOps()
	ent.SetGatewayID("gateway-id")
	ent.SetKonnectID("auth-strategy-id")

	programmedMessage := func() string {
		_, _ = Update(t.Context(), sdk, 0, cl, &metricsmocks.MockRecorder{}, ent)
		cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
		require.True(t, ok)
		return cond.Message
	}

	first := programmedMessage()
	second := programmedMessage()

	require.Equal(t, int64(4), requests.Load(), "each update sends a PUT and a POST")
	assert.Equal(t, first, second)
	assert.NotContains(t, first, "konnect:trace:")
}
