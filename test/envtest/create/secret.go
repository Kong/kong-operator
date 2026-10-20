package create

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// ClusterCASecret creates a self-signed CA Secret to be used as a cluster CA.
func ClusterCASecret(t *testing.T, ctx context.Context, cl client.Client, namespace, name string) *corev1.Secret {
	t.Helper()

	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName(fmt.Sprintf("%s-ca", name)),
		certificate.WithCATrue(),
	)

	secret := &corev1.Secret{
		Namespace: namespace,
		Name:      name,
		Labels: map[string]string{
			"konghq.com/secret": "internal",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		},
	}
	require.NoError(t, cl.Create(ctx, secret))

	return secret
}
