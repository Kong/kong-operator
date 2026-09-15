package util

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/labels"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
)

func TestPopulateTypeMeta(t *testing.T) {
	credential := &corev1.Secret{
		Name: "corn",
		Labels: map[string]string{
			labels.CredentialTypeLabel: "basic-auth",
		},
		StringData: map[string]string{
			"username": "corn",
			"password": "corn",
		},
	}

	require.Empty(t, credential.GetObjectKind().GroupVersionKind().Kind)

	err := PopulateTypeMeta(credential, scheme.Get())

	require.NoError(t, err)
	require.NotEmpty(t, credential.GetObjectKind().GroupVersionKind().Kind)
}
