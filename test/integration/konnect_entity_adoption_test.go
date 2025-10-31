package integration

import (
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/deploy"
)

func TestKonnectEntityAdoption(t *testing.T) {
	// A cleaner is created underneath anyway, and a whole namespace is deleted eventually.
	// We can't use a cleaner to delete objects because it handles deletes in FIFO order and that won't work in this
	// case: KonnectAPIAuthConfiguration shouldn't be deleted before any other object as that is required for others to
	// complete their finalizer which is deleting a reflecting entity in Konnect. That's why we're only cleaning up a
	// KonnectGatewayControlPlane and waiting for its deletion synchronously with deleteObjectAndWaitForDeletionFn to ensure it
	// was successfully deleted along with its children. The KonnectAPIAuthConfiguration is implicitly deleted along
	// with the namespace.
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect entities test with ID: %s", testID)

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

	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)

	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	cpKonnectID := cp.GetKonnectID()

	t.Logf("Create a service by SDK for adoption")
	server, err := server.NewServer[struct{}](test.KonnectServerURL())
	require.NoError(t, err, "Should create a server successfully")
	sdk := sdkops.NewSDKFactory().NewKonnectSDK(server, sdkops.SDKToken(test.KonnectAccessToken()))
	require.NotNil(t, sdk)

	resp, err := sdk.GetServicesSDK().CreateService(GetCtx(), cpKonnectID, sdkkonnectcomp.Service{
		Name: lo.ToPtr("test-adoption"),
		URL:  lo.ToPtr("http://example.com"),
		Host: "example.com",
	})
	require.NoError(t, err, "Should create service in Konnect successfully")
	serviceOutput := resp.GetService()
	require.NotNil(t, serviceOutput, "Should get a non-nil service in response")
	require.NotNil(t, serviceOutput.ID, "Should get a non-nil ID in the service")

	t.Logf("Create a KongService to adopt the service %s in Konnect", *serviceOutput.ID)
	kongService := deploy.KongService(t, GetCtx(), clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		func(obj client.Object) {
			svc, ok := obj.(*configurationv1alpha1.KongService)
			require.True(t, ok)
			svc.Spec.Adopt = &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: *serviceOutput.ID,
				},
			}
			svc.Spec.Name = lo.ToPtr("test-adoption")
		})
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongService.DeepCopy()))

	t.Logf("Waiting for the KongService to be programmed and set Konnect ID")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err = clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(kongService), kongService)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(collect, kongService)
		assert.Equalf(collect, *serviceOutput.ID, kongService.GetKonnectID(),
			"KongService should set Konnect ID %s as the adopted service in status", *serviceOutput.ID,
		)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		"Did not see KongService set Konnect ID and Programmed condition to True")

	t.Log("Updating the KongService to set a new path in its URL")
	oldKongService := kongService.DeepCopy()
	kongService.Spec.URL = lo.ToPtr("http://example.com/example")
	err = clientNamespaced.Patch(GetCtx(), kongService, client.MergeFrom(oldKongService))
	require.NoError(t, err)

	t.Log("Verifying that the service in Konnect is overridden by the KongService when KongService updated")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		resp, err := sdk.GetServicesSDK().GetService(GetCtx(), *serviceOutput.ID, cp.GetKonnectID())
		require.NoError(collect, err, "Should get service from Konnect successfully")

		serviceOutput := resp.GetService()
		require.NotNil(collect, serviceOutput, "Should get a non-nil service in response")
		assert.NotNil(collect, serviceOutput.Path, "Should get a non-nil path in the service")
		if serviceOutput.Path != nil {
			assert.Equal(collect, "/example", *serviceOutput.Path, "path of the service should be updated to match the spec in KongService")
		}

		err = clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(kongService), kongService)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kongService)
	}, testutils.ObjectUpdateTimeout, time.Second,
		"Did not see service in Konnect updated")
}
