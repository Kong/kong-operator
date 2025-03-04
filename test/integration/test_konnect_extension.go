package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectExtensionKonnectGatewayControlPlaneNamespacedRef(t *testing.T) {
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect extensions test with ID: %s", testID)

	// Create an APIAuth for test.
	clientNamespaced := client.NewNamespacedClient(GetClients().MgrClient, ns.Name)

	authCfg := deploy.KonnectAPIAuthConfiguration(t, GetCtx(), clientNamespaced,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			authCfg := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
			authCfg.Spec.Type = konnectv1alpha1.KonnectAPIAuthTypeToken
			authCfg.Spec.Token = test.KonnectAccessToken()
			authCfg.Spec.ServerURL = test.KonnectServerURL()
		},
	)

	// Create a Konnect control plane for the KonnectExtension to attach to.
	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
	)

	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Creating a KonnectExtension")
	ke := deploy.KonnectExtensionRefencingKonnectGatewayControlPlane(
		t, ctx,
		clientNamespaced,
		cp,
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ke.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have ControlPlaneRefValid contition set to True", ke.Namespace, ke.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
		require.NoError(t, err)
		assert.Truef(t, lo.ContainsBy(
			ke.Status.Conditions, func(cond metav1.Condition) bool {
				return cond.Type == konnectv1alpha1.ControlPlaneRefValidConditionType &&
					cond.Status == metav1.ConditionTrue
			},
		), "ControlPlaneRefValid has not been set to True, conditions: %+v", ke.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// TODO: Create a DataPlane using this KonnectExtension:
	// https://github.com/Kong/gateway-operator/issues/726
	// Create a secret used as dataplane certificate for the KonnectExtension.
	s := deploy.Secret(
		t, ctx, clientNamespaced,
		// TODO: Fill real certificate data here after DP certifcates provisioning is done:
		// https://github.com/Kong/gateway-operator/issues/874
		map[string][]byte{},
	)
	// Create a KonnectExtension attaching to the CP by its ID.
	t.Logf("Creating a KonnectExtension and waiting for Konnect control plane ref resolved")
	ke = deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectConfiguration[*konnectv1alpha1.KonnectExtension](konnectv1alpha1.KonnectConfiguration{
			APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
				Name: authCfg.Name,
			},
		}),
		setKonnectExtensionKonnectIDControlPlaneRef(t, cp.GetKonnectID()),
		setKonnectExtensionDPCertSecretRef(t, s),
	)

	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ke.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", ke.Namespace, ke.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := checkKonnectExtensionConditions(t, ke)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, ke.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", ke.Namespace, ke.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
		require.NoError(t, err)
		// Check Konnect control plane ID
		assert.NotNil(t, ke.Status.Konnect, "status.konnect should be present")
		assert.Equal(t, cp.GetKonnectID(), ke.Status.Konnect.ControlPlaneID, "Konnect control plane ID should be set in status")
		// Check dataplane client auth
		assert.NotNil(t, ke.Status.DataPlaneClientAuth, "status.dataPlaneClientAuth should be present")
		assert.NotNil(t, ke.Status.DataPlaneClientAuth.CertificateSecretRef, "status.dataPlaneClientAuth.certiifcateSecretRef should be present")
		assert.Equal(t, s.Name, ke.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
			"status.dataPlaneClientAuth.certiifcateSecretRef should have the expected secret name")

	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// TODO: Create DataPlanes using the KonnectExtension after DP certifcates provisioning is done:
	// https://github.com/Kong/gateway-operator/issues/874

}

func setKonnectExtensionKonnectIDControlPlaneRef(t *testing.T, cpID string) deploy.ObjOption {
	return func(obj client.Object) {
		ke, ok := obj.(*konnectv1alpha1.KonnectExtension)
		require.True(t, ok)
		// TODO: use `WithKonnectIDControlPlaneRef` after KonnectExtension support `SetControlPlaneRef`:
		// https://github.com/Kong/kubernetes-configuration/issues/328
		ke.Spec.KonnectControlPlane.ControlPlaneRef = commonv1alpha1.ControlPlaneRef{
			Type:      commonv1alpha1.ControlPlaneRefKonnectID,
			KonnectID: lo.ToPtr(cpID),
		}
	}
}

func setKonnectExtensionDPCertSecretRef(t *testing.T, s *corev1.Secret) func(client.Object) {
	return func(obj client.Object) {
		ke, ok := obj.(*konnectv1alpha1.KonnectExtension)
		require.True(t, ok)
		ke.Spec.DataPlaneClientAuth = &konnectv1alpha1.DataPlaneClientAuth{
			CertificateSecret: konnectv1alpha1.CertificateSecret{
				Provisioning: lo.ToPtr(konnectv1alpha1.ManualSecretProvisioning),
				CertificateSecretRef: &konnectv1alpha1.SecretRef{
					Name: s.Name,
				},
			},
		}
	}
}

func checkKonnectExtensionConditions(t *assert.CollectT, ke *konnectv1alpha1.KonnectExtension) (bool, string) {
	err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
	require.NoError(t, err)

	checkConditionTypes := []consts.ConditionType{
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
		konnectv1alpha1.KonnectExtensionReadyConditionType,
	}
	return helpers.CheckAllConditionsTrue(ke, checkConditionTypes)
}
