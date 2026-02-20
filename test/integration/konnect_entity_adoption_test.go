package integration

import (
	"encoding/json"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/conditions"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
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
		Name: new("test-adoption"),
		URL:  new("http://example.com"),
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
			svc.Spec.Name = new("test-adoption")
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
	kongService.Spec.URL = new("http://example.com/example")
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
			Name: new("route-test-adopt"),
			Paths: []string{
				"/example",
			},
			Service: &sdkkonnectcomp.RouteJSONService{
				ID: new(serviceKonnectID),
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
			kr.Spec.Name = new("route-test-adopt")
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
	cp = eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, cp)

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
	globalPluginID := *globalRequestTransformerPlugin.ID

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
			Scope: configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane,
		},
	},
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongPluginBinding](commonv1alpha1.AdoptModeOverride, globalPluginID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kpbGlobal.DeepCopy()))

	t.Log("Waiting for KongPluginBinding to be programmed and set Konnect ID")
	eventually.KonnectEntityGetsProgrammed(
		t, ctx, clientNamespaced, kpbGlobal,
		func(t *assert.CollectT, kpb *configurationv1alpha1.KongPluginBinding) {
			require.Equalf(t, globalPluginID, kpb.GetKonnectID(),
				"KongPluginBinding %s should set Konnect ID %s as the adopted plugin in status",
				client.ObjectKeyFromObject(kpb), globalPluginID,
			)
		},
	)

	t.Log("Creating a KongService to attach plugins to")
	ks := deploy.KongService(t, GetCtx(), clientNamespaced, deploy.WithKonnectNamespacedRefControlPlaneRef(cp))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ks.DeepCopy()))

	t.Log("Waiting for the KongService to get a Konnect ID")
	ks = eventually.KonnectEntityGetsProgrammed(
		t, ctx, clientNamespaced, ks,
	)
	require.NotEmpty(t, ks.GetKonnectID(), "KongService should get Konnect ID when programmed")
	serviceKonnectID := ks.GetKonnectID()

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
				ID: new(serviceKonnectID),
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, resp, "Should get a non-nil response for creating a plugin")
	require.NotNil(t, resp.Plugin, "Should get a non-nil plugin in the response")
	serviceResponseTransformerPlugin := resp.Plugin
	require.NotNil(t, serviceResponseTransformerPlugin.ID, "Should receive a non-nil plugin ID")
	pluginServiceID := *serviceResponseTransformerPlugin.ID

	t.Log("Creating a KongPlugin and a KongPluginBinding for adopting the plugin")
	kongPluginResponseTransformer := deploy.ResponseTransformerPlugin(t, ctx, clientNamespaced)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongPluginResponseTransformer.DeepCopy()))

	kpbService := deploy.KongPluginBinding(t, GetCtx(), clientNamespaced, &configurationv1alpha1.KongPluginBinding{
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: kongPluginResponseTransformer.Name,
			},
		},
	},
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongPluginBinding](commonv1alpha1.AdoptModeOverride, pluginServiceID),
		deploy.WithKongPluginBindingTarget(ks),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kpbService.DeepCopy()))

	t.Log("Waiting for KongPluginBinding to be programmed and set Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, kpbService,
		func(t *assert.CollectT, kpb *configurationv1alpha1.KongPluginBinding) {
			require.Equalf(t, pluginServiceID, kpb.GetKonnectID(),
				"KongPluginBinding %s should set Konnect ID %s as the adopted plugin in status",
				client.ObjectKeyFromObject(kpbService), pluginServiceID,
			)
		},
	)
}

func TestKonnectEntityAdoption_ConsumerWithCredentials(t *testing.T) {
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect entity adoption test for consumer with credentials with ID: %s", testID)

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
	cp = eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, cp)

	cpKonnectID := cp.GetKonnectID()

	t.Log("Create a consumer by SDK for adoption")
	server, err := server.NewServer[struct{}](test.KonnectServerURL())
	require.NoError(t, err, "Should create a server successfully")
	sdk := sdkops.NewSDKFactory().NewKonnectSDK(server, sdkops.SDKToken(test.KonnectAccessToken()))
	require.NotNil(t, sdk)

	consumerUsername := "test-adoption-consumer-" + testID
	consumerResp, err := sdk.GetConsumersSDK().CreateConsumer(GetCtx(), cpKonnectID, sdkkonnectcomp.Consumer{
		Username: new(consumerUsername),
	})
	require.NoError(t, err, "Should create consumer in Konnect successfully")
	consumerOutput := consumerResp.GetConsumer()
	require.NotNil(t, consumerOutput, "Should get a non-nil consumer in response")
	require.NotNil(t, consumerOutput.ID, "Should get a non-nil ID in the consumer")
	consumerKonnectID := *consumerOutput.ID
	t.Logf("Created consumer %s in Konnect with ID: %s", consumerUsername, consumerKonnectID)

	t.Log("Create a BasicAuth credential attached to the consumer by SDK for adoption")
	basicAuthResp, err := sdk.GetBasicAuthCredentialsSDK().CreateBasicAuthWithConsumer(
		GetCtx(),
		sdkkonnectops.CreateBasicAuthWithConsumerRequest{
			ControlPlaneID:              cpKonnectID,
			ConsumerIDForNestedEntities: consumerKonnectID,
			BasicAuthWithoutParents: sdkkonnectcomp.BasicAuthWithoutParents{
				Username: "basic-auth-user-" + testID,
				Password: "basic-auth-password",
			},
		},
	)
	require.NoError(t, err, "Should create BasicAuth credential in Konnect successfully")
	basicAuthOutput := basicAuthResp.GetBasicAuth()
	require.NotNil(t, basicAuthOutput, "Should get a non-nil BasicAuth in response")
	require.NotNil(t, basicAuthOutput.ID, "Should get a non-nil ID in the BasicAuth")
	basicAuthKonnectID := *basicAuthOutput.ID
	t.Logf("Created BasicAuth credential in Konnect with ID: %s", basicAuthKonnectID)

	t.Log("Create an APIKey credential attached to the consumer by SDK for adoption")
	apiKeyResp, err := sdk.GetAPIKeyCredentialsSDK().CreateKeyAuthWithConsumer(
		GetCtx(),
		sdkkonnectops.CreateKeyAuthWithConsumerRequest{
			ControlPlaneID:              cpKonnectID,
			ConsumerIDForNestedEntities: consumerKonnectID,
			KeyAuthWithoutParents: &sdkkonnectcomp.KeyAuthWithoutParents{
				Key: new("api-key-" + testID),
			},
		},
	)
	require.NoError(t, err, "Should create APIKey credential in Konnect successfully")
	apiKeyOutput := apiKeyResp.GetKeyAuth()
	require.NotNil(t, apiKeyOutput, "Should get a non-nil APIKey in response")
	require.NotNil(t, apiKeyOutput.ID, "Should get a non-nil ID in the APIKey")
	apiKeyKonnectID := *apiKeyOutput.ID
	t.Logf("Created APIKey credential in Konnect with ID: %s", apiKeyKonnectID)

	t.Log("Create a KongConsumer to adopt the consumer in Konnect")
	kongConsumer := deploy.KongConsumer(t, GetCtx(), clientNamespaced, consumerUsername,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithKonnectAdoptOptions[*configurationv1.KongConsumer](commonv1alpha1.AdoptModeOverride, consumerKonnectID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongConsumer.DeepCopy()))

	t.Log("Waiting for the KongConsumer to be programmed and set Konnect ID")
	kongConsumer = eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, kongConsumer,
		func(collect *assert.CollectT, kc *configurationv1.KongConsumer) {
			assert.Equalf(collect, consumerKonnectID, kc.GetKonnectID(),
				"KongConsumer should set Konnect ID %s as the adopted consumer in status", consumerKonnectID,
			)
		},
	)

	t.Log("Create a KongCredentialBasicAuth to adopt the BasicAuth credential in Konnect")
	kongCredentialBasicAuth := &configurationv1alpha1.KongCredentialBasicAuth{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "basic-auth-adopt-",
			Namespace:    ns.Name,
		},
		Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: kongConsumer.Name,
			},
			KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
				Username: "basic-auth-user-" + testID,
				Password: "basic-auth-password",
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: basicAuthKonnectID,
				},
			},
		},
	}
	require.NoError(t, clientNamespaced.Create(GetCtx(), kongCredentialBasicAuth))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongCredentialBasicAuth.DeepCopy()))

	t.Log("Waiting for the KongCredentialBasicAuth to be programmed and set Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, kongCredentialBasicAuth,
		func(collect *assert.CollectT, cred *configurationv1alpha1.KongCredentialBasicAuth) {
			assert.Equalf(collect, basicAuthKonnectID, cred.GetKonnectID(),
				"KongCredentialBasicAuth should set Konnect ID %s as the adopted credential in status", basicAuthKonnectID,
			)
		},
	)

	t.Log("Create a KongCredentialAPIKey to adopt the APIKey credential in Konnect")
	kongCredentialAPIKey := &configurationv1alpha1.KongCredentialAPIKey{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "api-key-adopt-",
			Namespace:    ns.Name,
		},
		Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: kongConsumer.Name,
			},
			KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
				Key: "api-key-" + testID,
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: apiKeyKonnectID,
				},
			},
		},
	}
	require.NoError(t, clientNamespaced.Create(GetCtx(), kongCredentialAPIKey))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kongCredentialAPIKey.DeepCopy()))

	t.Log("Waiting for the KongCredentialAPIKey to be programmed and set Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, clientNamespaced, kongCredentialAPIKey,
		func(collect *assert.CollectT, cred *configurationv1alpha1.KongCredentialAPIKey) {
			assert.Equalf(collect, apiKeyKonnectID, cred.GetKonnectID(),
				"KongCredentialAPIKey should set Konnect ID %s as the adopted credential in status", apiKeyKonnectID,
			)
		},
	)

	t.Log("Updating the KongConsumer to verify the adopted consumer can be updated")
	oldKongConsumer := kongConsumer.DeepCopy()
	kongConsumer.CustomID = "custom-id-" + testID
	err = clientNamespaced.Patch(GetCtx(), kongConsumer, client.MergeFrom(oldKongConsumer))
	require.NoError(t, err)

	t.Log("Verifying that the consumer in Konnect is updated")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		resp, err := sdk.GetConsumersSDK().GetConsumer(GetCtx(), consumerKonnectID, cpKonnectID)
		require.NoError(collect, err, "Should get consumer from Konnect successfully")

		consumerOutput := resp.GetConsumer()
		require.NotNil(collect, consumerOutput, "Should get a non-nil consumer in response")
		assert.NotNil(collect, consumerOutput.CustomID, "Should get a non-nil CustomID in the consumer")
		if consumerOutput.CustomID != nil {
			assert.Equal(collect, "custom-id-"+testID, *consumerOutput.CustomID,
				"CustomID of the consumer should be updated to match the spec in KongConsumer")
		}
	}, testutils.ObjectUpdateTimeout, checkKonnectAPITick,
		"Did not see consumer in Konnect updated to match spec of KongConsumer")
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
