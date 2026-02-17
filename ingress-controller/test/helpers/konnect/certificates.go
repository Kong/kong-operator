package konnect

import (
	"context"
	"io"
	"net/http"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/Kong/sdk-konnect-go/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/konnect/sdk"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// CreateClientCertificate creates a TLS client certificate and POSTs it to Konnect Control Plane configuration API
// so that KIC can use the certificates to authenticate against Konnect Admin API.
// It the token is provided, it will be used to create the client certificate.
// Otherwise, the default access token will be used.
func CreateClientCertificate(ctx context.Context, t *testing.T, cpID string, token ...string) (certPEM string, keyPEM string) {
	t.Helper()

	retryConfig := sdkkonnectgo.WithRetryConfig(retry.Config{
		Backoff: &retry.BackoffStrategy{
			InitialInterval: 100,
			MaxInterval:     2000,
			Exponent:        1.2,
			MaxElapsedTime:  10000,
		},
	})

	var s *sdkkonnectgo.SDK
	if len(token) == 0 {
		s = sdk.New(accessToken(), serverURLOpt(), retryConfig)
	} else {
		s = sdk.New(token[0], serverURLOpt(), retryConfig)
	}

	cert, key := certificate.MustGenerateCertPEMFormat()

	t.Log("creating client certificate in Konnect")
	resp, err := s.DPCertificates.CreateDataplaneCertificate(ctx, cpID, &sdkkonnectcomp.DataPlaneClientCertificateRequest{
		Cert: string(cert),
	})
	require.NoError(t, err)

	if !assert.Equal(t, http.StatusCreated, resp.GetStatusCode()) {
		body, err := io.ReadAll(resp.RawResponse.Body)
		if err != nil {
			body = []byte(err.Error())
		}
		require.Failf(t, "failed creating client certificate", "body %s", body)
		return "", ""
	}

	return string(cert), string(key)
}
