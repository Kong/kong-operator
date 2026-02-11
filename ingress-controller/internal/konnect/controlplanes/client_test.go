package controlplanes_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/ingress-controller/internal/konnect/sdk"
	"github.com/kong/kong-operator/modules/manager/metadata"
)

type mockControlPlanesServer struct {
	t *testing.T
}

func newMockControlPlanesServer(t *testing.T) *mockControlPlanesServer {
	return &mockControlPlanesServer{
		t: t,
	}
}

func (m *mockControlPlanesServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		assert.Equal(m.t, metadata.Metadata().UserAgent(), r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		assert.Equal(m.t, metadata.Metadata().UserAgent(), r.Header.Get("User-Agent"))
	}
}

func TestControlPlanesClientUserAgent(t *testing.T) {
	ts := httptest.NewServer(newMockControlPlanesServer(t))
	t.Cleanup(ts.Close)

	ctx := t.Context()
	sdk := sdk.New("kpat_xxx", sdkkonnectgo.WithServerURL(ts.URL))

	_, err := sdk.ControlPlanes.CreateControlPlane(ctx, sdkkonnectcomp.CreateControlPlaneRequest{
		Name: "test",
	})
	// NOTE: just check the user agent and do not attempt to mock out the entire response.
	require.ErrorContains(t, err, "unknown content-type received: : Status 201")

	_, err = sdk.ControlPlanes.DeleteControlPlane(ctx, "id")
	// NOTE: just check the user agent and do not attempt to mock out the entire response.
	require.ErrorContains(t, err, "unknown status code returned: Status 200")
}
