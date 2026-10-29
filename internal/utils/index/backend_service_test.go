package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
)

func TestClientKongCertificateOnBackendService(t *testing.T) {
	for _, tt := range []struct {
		name        string
		annotations map[string]string
		want        []string
	}{
		{name: "absent"},
		{name: "legacy", annotations: map[string]string{"konghq.com/client-cert": "client"}},
		{name: "same namespace", annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "client"}, want: []string{"backends/client", "backends/*"}},
		{name: "explicit namespace", annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "certs/client"}, want: []string{"certs/client", "certs/*"}},
		{name: "malformed", annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "a/b/c"}},
		{name: "conflict", annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "client", "konghq.com/client-cert": "secret"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ClientKongCertificateOnBackendService(&corev1.Service{
				Namespace: "backends", Annotations: tt.annotations,
			}))
		})
	}
	require.Nil(t, ClientKongCertificateOnBackendService(&corev1.Secret{}))
}
