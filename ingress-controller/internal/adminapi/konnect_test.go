//go:build integration_tests

package adminapi_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/ingress-controller/internal/adminapi"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
)

func TestNewKongClientForKonnectControlPlane(t *testing.T) {
	t.Skip("There's no infrastructure for Konnect tests yet")

	ctx := t.Context()
	const controlPlaneID = "adf78c28-5763-4394-a9a4-a9436a1bea7d"

	c, err := adminapi.NewKongClientForKonnectControlPlane(managercfg.KonnectConfig{
		ConfigSynchronizationEnabled: true,
		ControlPlaneID:               controlPlaneID,
		Address:                      "https://us.kic.api.konghq.tech",
		TLSClient: managercfg.TLSClientConfig{
			Cert: os.Getenv("KONG_TEST_KONNECT_TLS_CLIENT_CERT"),
			Key:  os.Getenv("KONG_TEST_KONNECT_TLS_CLIENT_KEY"),
		},
	})
	require.NoError(t, err)

	require.True(t, c.IsKonnect())
	require.Equal(t, controlPlaneID, c.KonnectControlPlane())

	_, err = c.AdminAPIClient().Services.ListAll(ctx)
	require.NoError(t, err)
}
