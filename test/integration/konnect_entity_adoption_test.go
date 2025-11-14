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

const (
	// checkKonnectAPITick is the interval to check entity status in Konnect by Konnect APIs.
	checkKonnectAPITick = time.Second
)

func TestKonnectEntityAdoption_ServiceAndRoute(t *testing.T) {
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
	serviceKonnectID := *serviceOutput.ID

	t.Logf("Create a KongService to adopt the service %s in Konnect", serviceKonnectID)
	kongService := deploy.KongService(t, GetCtx(), clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		func(obj client.Object) {
			svc, ok := obj.(*configurationv1alpha1.KongService)
			require.True(t, ok)
			svc.Spec.Adopt = &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: serviceKonnectID,
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
		assert.Equalf(collect, serviceKonnectID, kongService.GetKonnectID(),
			"KongService should set Konnect ID %s as the adopted service in status", serviceKonnectID,
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
		resp, err := sdk.GetServicesSDK().GetService(GetCtx(), serviceKonnectID, cp.GetKonnectID())
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
	}, testutils.ObjectUpdateTimeout, checkKonnectAPITick,
		"Did not see service in Konnect updated to match spec of KongService")

	t.Logf("Creating a route attached to service %s by SDK for adoption", serviceKonnectID)
	routeResp, err := sdk.GetRoutesSDK().CreateRoute(GetCtx(), cpKonnectID, sdkkonnectcomp.Route{
		Type: sdkkonnectcomp.RouteTypeRouteJSON,
		RouteJSON: &sdkkonnectcomp.RouteJSON{
			Name: lo.ToPtr("route-test-adopt"),
			Paths: []string{
				"/example",
			},
			Service: &sdkkonnectcomp.RouteJSONService{
				ID: lo.ToPtr(serviceKonnectID),
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, routeResp, "Should get a non-nil response for creating route")
	require.NotNil(t, routeResp.Route, "Should get a non-nil route in response of creating the route")
	require.NotNil(t, routeResp.Route.RouteJSON, "Should get a non-nil JSON formatted route")
	require.NotNil(t, routeResp.Route.RouteJSON.ID, "Should get a non-nil route ID")
	routeKonnectID := *routeResp.Route.RouteJSON.ID

	t.Logf("Adopting the route %s in override mode", routeKonnectID)
	kongRoute := deploy.KongRoute(t, GetCtx(), clientNamespaced,
		deploy.WithNamespacedKongServiceRef(kongService),
		func(obj client.Object) {
			kr, ok := obj.(*configurationv1alpha1.KongRoute)
			require.True(t, ok)
			kr.Spec.Name = lo.ToPtr("route-test-adopt")
			kr.Spec.Paths = []string{"/example-2"}
			kr.Spec.Adopt = &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: routeKonnectID,
				},
			}
		})
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongRoute.DeepCopy()))

	t.Logf("Waiting for the KongRoute to be programmed and set Konnect ID")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err = clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(kongRoute), kongRoute)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(collect, kongRoute)
		assert.Equalf(collect, routeKonnectID, kongRoute.GetKonnectID(),
			"KongRoute should set Konnect ID %s as the adopted route in status", routeKonnectID,
		)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		"Did not see KongRoute set Konnect ID and Programmed condition to True")

	// When the KongRoute is marked Programmed, the route in Konnect should be updated
	// to match the spec of the KongRoute.
	t.Log("Verifying that the route in Konnect is modified as the spec of the KongRoute")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		getRouteResp, err := sdk.GetRoutesSDK().GetRoute(GetCtx(), routeKonnectID, cpKonnectID)
		assert.NoError(collect, err)
		assert.NotNil(collect, getRouteResp, "Should get a non-nil response for creating route")
		if getRouteResp.Route != nil && getRouteResp.Route.RouteJSON != nil {
			assert.True(collect, lo.ElementsMatch([]string{"/example-2"}, getRouteResp.Route.RouteJSON.Paths),
				"Should have the same paths as in the spec of the KongRoute, actual:", getRouteResp.Route.RouteJSON.Paths)
		} else {
			assert.Fail(collect, "route in response is empty")
		}
	}, testutils.ObjectUpdateTimeout, checkKonnectAPITick,
		"Did not see route in Konnect to be updated to match the spec of KongRoute")

}

func TestKonnectEntityAdoption_KonnectCloudGatewayNetwork(t *testing.T) {
	// TODO: enable it when our testing account in dev env has the permission to use cloud gateway.
	t.Skip("Skipped the test because the testing account does not have the permission to use cloud gateway")

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

	t.Logf("Create a network by SDK for adoption")
	server, err := server.NewServer[struct{}](test.KonnectServerURL())
	require.NoError(t, err, "Should create a server successfully")
	sdk := sdkops.NewSDKFactory().NewKonnectSDK(server, sdkops.SDKToken(test.KonnectAccessToken()))
	require.NotNil(t, sdk)

	networkName := "k8s-test-ko-adoption" + uuid.NewString()[:8]
	cloudGatewayProviderID := "" // TODO: Use the real cloud gateway provider ID here
	resp, err := sdk.GetCloudGatewaysSDK().CreateNetwork(t.Context(), sdkkonnectcomp.CreateNetworkRequest{
		Name:                          networkName,
		CloudGatewayProviderAccountID: cloudGatewayProviderID,
		Region:                        "us-east-1",
		CidrBlock:                     "10.0.0.0/16",
		AvailabilityZones:             []string{"use1-az1", "use1-az2", "use1-az4", "use1-az5", "use1-az6"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.GetNetwork())

	t.Log("Creating a KonnectCloudGatewayNetwork to adopt a network in Konnect cloud gateway")
	networkKonnectID := resp.GetNetwork().ID
	n := deploy.KonnectCloudGatewayNetwork(t, t.Context(), clientNamespaced, authCfg, func(obj client.Object) {
		n, ok := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
		require.True(t, ok)
		n.Spec.Adopt = &commonv1alpha1.AdoptOptions{
			From: commonv1alpha1.AdoptSourceKonnect,
			Mode: commonv1alpha1.AdoptModeMatch,
			Konnect: &commonv1alpha1.AdoptKonnectOptions{
				ID: networkKonnectID,
			},
		}
		n.Spec.Name = networkName
		n.Spec.Region = "us-east-1"
		n.Spec.CidrBlock = "10.0.0.0/16"
		n.Spec.CloudGatewayProviderAccountID = cloudGatewayProviderID
		n.Spec.AvailabilityZones = []string{"use1-az1", "use1-az2", "use1-az4", "use1-az5", "use1-az6"}
	})
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, n.DeepCopy()))

	t.Log("Waiting for the KonnectCloudGatewayNetwork to get Programmed")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err := clientNamespaced.Get(t.Context(), client.ObjectKeyFromObject(n), n)
		require.NoError(t, err)
		t.Logf("Current status: %+v", n.Status)
		assertKonnectEntityProgrammed(collect, n)
	}, 5*time.Minute, 30*time.Second)

}
