package sdk

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

func TestNewKonnectSDKRejectsHostOutsideAllowedDomains(t *testing.T) {
	f := NewSDKFactory(WithAllowedServerURLDomains(server.DefaultDomains))

	for _, url := range []string{
		"https://us.attacker.example",
		"aa.evil.example",
		"https://eu.burp-collaborator.net",
	} {
		t.Run("rejects "+url, func(t *testing.T) {
			srv := lo.Must(server.NewServer[any](url))

			sdk, err := f.NewKonnectSDK(srv, SDKToken("t"))

			require.Error(t, err)
			require.Nil(t, sdk)
			require.ErrorContains(t, err, "konghq.com")
		})
	}

	for _, url := range []string{
		"https://us.api.konghq.com",
		"us.svc.konghq.tech/api",
		"https://global.svc.konghq.com/api",
		"https://us.api.konghq.com:443",
	} {
		t.Run("accepts "+url, func(t *testing.T) {
			srv := lo.Must(server.NewServer[any](url))

			sdk, err := f.NewKonnectSDK(srv, SDKToken("t"))

			require.NoError(t, err)
			require.NotNil(t, sdk)
		})
	}

	t.Run("a factory built without the option still refuses", func(t *testing.T) {
		srv := lo.Must(server.NewServer[any]("https://us.attacker.example"))

		_, err := NewSDKFactory().NewKonnectSDK(srv, SDKToken("t"))

		require.Error(t, err)
	})
}
