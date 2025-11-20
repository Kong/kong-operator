package integration

import (
	"encoding/json"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/conditions"
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
		conditions.KonnectEntityIsProgrammed(t, cp)
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

		conditions.KonnectEntityIsProgrammed(collect, kongService)
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
		conditions.KonnectEntityIsProgrammed(t, kongService)
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

		conditions.KonnectEntityIsProgrammed(collect, kongRoute)
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

func TestKonnectEntityAdoption_Plugin(t *testing.T) {
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect entity adoption test for plugins with ID: %s", testID)

	clientNamespaced := client.NewNamespacedClient(GetClients().MgrClient, ns.Name)

	t.Log("Creating Konnect API auth configuration and Konnect control plane")
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

	t.Logf("Create a global plugin by SDK for adoption")
	server, err := server.NewServer[struct{}](test.KonnectServerURL())
	require.NoError(t, err, "Should create a server successfully")
	sdk := sdkops.NewSDKFactory().NewKonnectSDK(server, sdkops.SDKToken(test.KonnectAccessToken()))
	require.NotNil(t, sdk)

	resp, err := sdk.GetPluginSDK().CreatePlugin(
		t.Context(),
		cpKonnectID,
		sdkkonnectcomp.Plugin{
			Name: "request-transformer",
			Config: map[string]any{
				"add": map[string][]string{
					"headers": {
						"X-Kong-Test:test",
					},
				},
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, resp, "Should get a non-nil response for creating a plugin")
	require.NotNil(t, resp.Plugin, "Should get a non-nil plugin in the response")
	globalRequestTransformerPlugin := resp.Plugin
	require.NotNil(t, globalRequestTransformerPlugin.ID, "Should receive a non-nil plugin ID")

	buf, err := json.Marshal(globalRequestTransformerPlugin.Config)
	require.NoError(t, err, "Should marshal plugin configuration in JSON successfully")

	t.Log("Creating a KongPlugin and a KongPluginBinding to adopt the plugin")
	kongPluginReqTransformer := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "kongplugin-global-request-transformer",
		},
		PluginName: "request-transformer",
		Config: apiextensionsv1.JSON{
			Raw: buf,
		},
	}
	require.NoError(t, clientNamespaced.Create(GetCtx(), kongPluginReqTransformer))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongPluginReqTransformer.DeepCopy()))

	kpbGlobal := deploy.KongPluginBinding(t, GetCtx(), clientNamespaced, &configurationv1alpha1.KongPluginBinding{
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: kongPluginReqTransformer.Name,
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: *globalRequestTransformerPlugin.ID,
				},
			},
			Scope: configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane,
		},
	}, deploy.WithKonnectNamespacedRefControlPlaneRef(cp))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kpbGlobal.DeepCopy()))

	t.Log("Waiting for KongPluginBinding to be programmed and set Konnect ID")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err := clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(kpbGlobal), kpbGlobal)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(collect, kpbGlobal)
		assert.Equalf(collect, *globalRequestTransformerPlugin.ID, kpbGlobal.GetKonnectID(),
			"KongPluginBinding should set Konnect ID %s as the adopted plugin in status",
			*globalRequestTransformerPlugin.ID,
		)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		"Did not see KongPluginBinding set Konnect ID and Programmed condition to True",
	)

	t.Log("Creating a KongService to attach plugins to")
	ks := deploy.KongService(t, GetCtx(), clientNamespaced, deploy.WithKonnectNamespacedRefControlPlaneRef(cp))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ks.DeepCopy()))

	t.Log("Waiting for the KongService to get a Konnect ID")
	var serviceKonnectID string
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err := clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(ks), ks)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(collect, ks)
		assert.NotEmpty(collect, ks.GetKonnectID())
		serviceKonnectID = ks.GetKonnectID()
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		"Did not see KongService set Konnect ID and Programmed condition to True",
	)

	t.Log("Creating a plugin by SDK attached to the service for adopting")
	resp, err = sdk.GetPluginSDK().CreatePlugin(
		GetCtx(),
		cpKonnectID,
		sdkkonnectcomp.Plugin{
			Name: "response-transformer",
			Config: map[string]any{
				"add": map[string][]string{
					"headers": {"X-Kong-Test:test"},
				},
			},
			Service: &sdkkonnectcomp.PluginService{
				ID: lo.ToPtr(serviceKonnectID),
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, resp, "Should get a non-nil response for creating a plugin")
	require.NotNil(t, resp.Plugin, "Should get a non-nil plugin in the response")
	serviceResponseTransformerPlugin := resp.Plugin
	require.NotNil(t, serviceResponseTransformerPlugin.ID, "Should receive a non-nil plugin ID")

	buf, err = json.Marshal(serviceResponseTransformerPlugin.Config)
	require.NoError(t, err, "Should marshal plugin configuration in JSON successfully")

	t.Log("Creating a KongPlugin and a KongPluginBinding for adopting the plugin")
	kongPluginResponseTransformer := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "kongplugin-service-response-transformer",
		},
		PluginName: "response-transformer",
		Config: apiextensionsv1.JSON{
			Raw: buf,
		},
	}
	require.NoError(t, clientNamespaced.Create(GetCtx(), kongPluginResponseTransformer))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongPluginResponseTransformer.DeepCopy()))

	kpbService := deploy.KongPluginBinding(t, GetCtx(), clientNamespaced, &configurationv1alpha1.KongPluginBinding{
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: kongPluginResponseTransformer.Name,
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: *serviceResponseTransformerPlugin.ID,
				},
			},
			Scope: configurationv1alpha1.KongPluginBindingScopeOnlyTargets,
			Targets: &configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name:  ks.Name,
					Kind:  "KongService",
					Group: configurationv1alpha1.GroupVersion.Group,
				},
			},
		},
	}, deploy.WithKonnectNamespacedRefControlPlaneRef(cp))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kpbService.DeepCopy()))

	t.Log("Waiting for KongPluginBinding to be programmed and set Konnect ID")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err := clientNamespaced.Get(GetCtx(), client.ObjectKeyFromObject(kpbService), kpbService)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(collect, kpbService)
		assert.Equalf(collect, *serviceResponseTransformerPlugin.ID, kpbService.GetKonnectID(),
			"KongPluginBinding should set Konnect ID %s as the adopted plugin in status",
			*serviceResponseTransformerPlugin.ID,
		)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		"Did not see KongPluginBinding set Konnect ID and Programmed condition to True",
	)
}
