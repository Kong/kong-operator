package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestExtractClientCertificateRef(t *testing.T) {
	for _, tt := range []struct {
		name        string
		annotations map[string]string
		want        *commonv1alpha1.NamespacedRef
		invalid     bool
	}{
		{name: "absent"},
		{name: "legacy is still a Secret name", annotations: map[string]string{"konghq.com/client-cert": "a-secret"}},
		{name: "backend namespace", annotations: map[string]string{ClientCertificateRefAnnotation: "client"},
			want: &commonv1alpha1.NamespacedRef{Name: "client", Namespace: new("backends")}},
		{name: "explicit namespace", annotations: map[string]string{ClientCertificateRefAnnotation: "certificates/client"},
			want: &commonv1alpha1.NamespacedRef{Name: "client", Namespace: new("certificates")}},
		{name: "conflicting mechanisms", annotations: map[string]string{
			"konghq.com/client-cert": "secret", ClientCertificateRefAnnotation: "client",
		}, invalid: true},
		{name: "empty legacy still conflicts", annotations: map[string]string{
			"konghq.com/client-cert": "", ClientCertificateRefAnnotation: "client",
		}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ExtractClientCertificateRef(tt.annotations, "backends")
			if tt.invalid {
				require.Error(t, err)
				require.Nil(t, ref)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, ref)
			}
		})
	}
	for _, value := range []string{"", " ", "/client", "certificates/", "a/b/c", "client ", "Name", "bad_namespace/client", "a..b", "../client"} {
		t.Run("invalid "+value, func(t *testing.T) {
			ref, err := ExtractClientCertificateRef(map[string]string{ClientCertificateRefAnnotation: value}, "backends")
			require.Error(t, err)
			require.Nil(t, ref)
		})
	}
}
