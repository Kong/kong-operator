package conditions

import (
	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/api/common/consts"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/test/helpers"
)

// CheckKonnectExtensionConditions checks the specified conditions on the given
// KonnectExtension and returns whether they are all true and a message if not.
func CheckKonnectExtensionConditions(
	ctx context.Context,
	t *assert.CollectT,
	cl client.Client,
	ke *konnectv1alpha2.KonnectExtension,
	checker helpers.ConditionsChecker,
	conditions ...consts.ConditionType,
) (bool, string) {
	t.Helper()
	nn := k8stypes.NamespacedName{
		Name:      ke.Name,
		Namespace: ke.Namespace,
	}
	require.NoError(t, cl.Get(ctx, nn, ke))

	return checker(ke, conditions...)
}

// CheckKonnectExtensionStatus checks the status fields of the given KonnectExtension
// against the expected values.
func CheckKonnectExtensionStatus(
	ctx context.Context,
	cl client.Client,
	ke *konnectv1alpha2.KonnectExtension,
	expectedKonnectCPID string,
	expectedDPCertificateSecretName string, //nolint:unparam
) func(t *assert.CollectT) {
	return func(t *assert.CollectT) {
		nn := k8stypes.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}
		require.NoError(t, cl.Get(ctx, nn, ke))
		// Check Konnect control plane ID
		require.NotNil(t, ke.Status.Konnect, "status.konnect should be present")
		require.NotEmpty(t, ke.Status.Konnect.ControlPlaneID, "status.konnect.controlPlaneID should be present")
		require.NotEmpty(t, ke.Status.Konnect.Endpoints.ControlPlaneEndpoint, "status.konnect.endpoints.controlPlaneEndpoint should be present")
		require.NotEmpty(t, ke.Status.Konnect.Endpoints.TelemetryEndpoint, "status.konnect.endpoints.telemetryEndpoint should be present")
		assert.Equal(t, expectedKonnectCPID, ke.Status.Konnect.ControlPlaneID, "Konnect control plane ID should be set in status")
		// Check dataplane client auth
		require.NotNil(t, ke.Status.DataPlaneClientAuth, "status.dataPlaneClientAuth should be present")
		require.NotNil(t, ke.Status.DataPlaneClientAuth.CertificateSecretRef, "status.dataPlaneClientAuth.certiifcateSecretRef should be present")
		if expectedDPCertificateSecretName != "" {
			assert.Equal(t, expectedDPCertificateSecretName, ke.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
				"status.dataPlaneClientAuth.certiifcateSecretRef should have the expected secret name")
		}
	}
}
