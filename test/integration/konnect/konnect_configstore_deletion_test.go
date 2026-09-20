package konnect

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectops "github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/helpers/object"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestKonnectConfigStoreDeletionBlockedBySecretEntries(t *testing.T) {
	ctx := t.Context()
	cl := integration.GetClients().MgrClient
	ns, _ := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	clientNamespaced := client.NewNamespacedClient(cl, ns.Name)
	testID := uuid.NewString()[:8]

	authCfg := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectAPIAuthConfigurationWithTestToken(test.KonnectAccessToken(), test.KonnectServerURL()),
	)
	cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)
	cp = eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, cp)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy()))

	configStore := deploy.KonnectConfigStore(t, ctx, clientNamespaced, cp,
		deploy.WithTestIDLabel(testID),
	)
	configStore = eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, configStore)
	configStoreKey := client.ObjectKeyFromObject(configStore)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		var current konnectv1alpha1.KonnectConfigStore
		if err := cl.Get(cleanupCtx, configStoreKey, &current); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Logf("failed to get KonnectConfigStore during cleanup: %v", err)
			}
			return
		}
		if err := cl.Delete(cleanupCtx, &current); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("failed to delete KonnectConfigStore during cleanup: %v", err)
		}
		eventually.WaitForObjectToNotExist(
			t, cleanupCtx, cl, &current,
			testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		)
	})

	sdkServer, err := server.NewServer[struct{}](test.KonnectServerURL())
	require.NoError(t, err)
	konnectSDK := sdkops.NewSDKFactory().NewKonnectSDK(
		sdkServer,
		sdkops.SDKToken(test.KonnectAccessToken()),
	)
	secretKey := "deletion-guard-" + testID
	_, err = konnectSDK.GetConfigStoreSecretsSDK().CreateConfigStoreSecret(
		ctx,
		sdkkonnectops.CreateConfigStoreSecretRequest{
			ControlPlaneID: cp.GetKonnectID(),
			ConfigStoreID:  configStore.GetKonnectID(),
			CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
				Key:   secretKey,
				Value: "test-value",
			},
		},
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, cleanupErr := konnectSDK.GetConfigStoreSecretsSDK().DeleteConfigStoreSecret(
			context.Background(),
			sdkkonnectops.DeleteConfigStoreSecretRequest{
				ControlPlaneID: cp.GetKonnectID(),
				ConfigStoreID:  configStore.GetKonnectID(),
				Key:            secretKey,
			},
		)
		if cleanupErr != nil && !konnectops.ErrIsNotFound(cleanupErr) {
			t.Logf("failed to delete config store secret during cleanup: %v", cleanupErr)
		}
		if err := triggerConfigStoreReconciliation(
			context.Background(), cl, configStoreKey, uuid.NewString(),
		); err != nil {
			t.Logf("failed to trigger KonnectConfigStore reconciliation during cleanup: %v", err)
		}
	})

	require.NoError(t, cl.Delete(ctx, configStore))

	var current konnectv1alpha1.KonnectConfigStore
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := cl.Get(ctx, configStoreKey, &current)
		require.NoError(t, err)
		require.False(t, current.DeletionTimestamp.IsZero())

		programmed := apimeta.FindStatusCondition(
			current.Status.Conditions,
			konnectv1alpha1.KonnectEntityProgrammedConditionType,
		)
		require.NotNil(t, programmed)
		assert.Equal(t, metav1.ConditionFalse, programmed.Status)
		assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked, programmed.Reason)
		assert.Contains(t, programmed.Message, "still holds secret entries")
		assert.NotContains(t, programmed.Message, secretKey)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	_, err = konnectSDK.GetConfigStoreSecretsSDK().GetConfigStoreSecret(
		ctx,
		sdkkonnectops.GetConfigStoreSecretRequest{
			ControlPlaneID: cp.GetKonnectID(),
			ConfigStoreID:  configStore.GetKonnectID(),
			Key:            secretKey,
		},
	)
	require.NoError(t, err, "blocked config store deletion must not cascade-delete its secret entries")

	_, err = konnectSDK.GetConfigStoreSecretsSDK().DeleteConfigStoreSecret(
		ctx,
		sdkkonnectops.DeleteConfigStoreSecretRequest{
			ControlPlaneID: cp.GetKonnectID(),
			ConfigStoreID:  configStore.GetKonnectID(),
			Key:            secretKey,
		},
	)
	require.NoError(t, err)

	require.NoError(t, triggerConfigStoreReconciliation(ctx, cl, configStoreKey, testID))
	require.True(t, eventually.WaitForObjectToNotExist(
		t, ctx, cl, &current,
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
	))

	var deleted konnectv1alpha1.KonnectConfigStore
	err = cl.Get(ctx, configStoreKey, &deleted)
	require.Error(t, err)
	require.True(t, apierrors.IsNotFound(err))
}

func triggerConfigStoreReconciliation(
	ctx context.Context,
	cl client.Client,
	key client.ObjectKey,
	marker string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var configStore konnectv1alpha1.KonnectConfigStore
		if err := cl.Get(ctx, key, &configStore); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if configStore.Annotations == nil {
			configStore.Annotations = make(map[string]string)
		}
		configStore.Annotations["gateway-operator.konghq.com/reconcile-after-secret-removal"] = marker
		return cl.Update(ctx, &configStore)
	})
}
