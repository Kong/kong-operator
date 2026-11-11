package konnectother

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// Konnect refuses to delete a schema registry policies use: the operator keeps
// the cleanup finalizer and reports DeletionBlocked naming the policy objects
// referencing it, and retries once they stop, through the watch on them.
func TestEventGatewaySchemaRegistryDeletionBlockedWhileInUse(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	// Only the schema registry is reconciled: the policy object exists in the
	// cluster with the Konnect ID of the Konnect policy using the registry,
	// which is enough to name it.
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewaySchemaRegistry](&metricsmocks.MockRecorder{}),
		),
	)
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	gateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)
	const (
		eventGatewayID   = "event-gateway-12345"
		registryID       = "registry-12345"
		virtualClusterID = "virtual-cluster-12345"
		policyID         = "policy-12345"
	)
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, gateway, eventGatewayID)

	w := envtest.SetupWatch[configurationv1alpha1.EventGatewaySchemaRegistryList](t, ctx, cl, client.InNamespace(ns.Name))

	sdk.EventGatewaySchemaRegistriesSDK.EXPECT().
		CreateEventGatewaySchemaRegistry(mock.Anything, eventGatewayID, mock.Anything).
		Return(&sdkkonnectops.CreateEventGatewaySchemaRegistryResponse{
			SchemaRegistry: &sdkkonnectcomp.SchemaRegistry{ID: registryID, Name: "orders"},
		}, nil)
	sdk.EventGatewaySchemaRegistriesSDK.EXPECT().
		UpdateEventGatewaySchemaRegistry(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewaySchemaRegistryResponse{}, nil).
		Maybe()
	sdk.EventGatewaySchemaRegistriesSDK.EXPECT().
		GetEventGatewaySchemaRegistry(mock.Anything, eventGatewayID, registryID).
		Return(&sdkkonnectops.GetEventGatewaySchemaRegistryResponse{
			SchemaRegistry: &sdkkonnectcomp.SchemaRegistry{ID: registryID, Name: "orders"},
		}, nil).
		Maybe()

	registry := &configurationv1alpha1.EventGatewaySchemaRegistry{}
	require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {generateName: registry-}
spec:
  gatewayRef: {type: namespacedRef, namespacedRef: {name: `+gateway.Name+`}}
  apiSpec:
    type: confluent
    confluent:
      name: orders
      config: {endpoint: "https://registry.example.com", schemaType: avro}
`), registry))
	require.NoError(t, clientNamespaced.Create(ctx, registry))
	envtest.WatchFor(t, ctx, w, apiwatch.Modified,
		envtest.AssertsAnd(
			envtest.ObjectMatchesName(registry),
			envtest.ObjectMatchesKonnectID[*configurationv1alpha1.EventGatewaySchemaRegistry](registryID),
			envtest.ObjectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewaySchemaRegistry](),
		),
		"EventGatewaySchemaRegistry didn't get programmed",
	)

	policy := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
	require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {generateName: validate-}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: validate
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: `+registry.Name+`}}, valueValidationAction: reject}}
`), policy))
	require.NoError(t, clientNamespaced.Create(ctx, policy))
	policy.SetKonnectID(policyID)
	require.NoError(t, clientNamespaced.Status().Update(ctx, policy))

	// Konnect lists the policy as using the registry, by its Konnect ID: its
	// config is only in the raw response body.
	sdk.EventGatewayVirtualClustersSDK.EXPECT().
		ListEventGatewayVirtualClusters(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayVirtualClustersRequest) bool {
			return req.GatewayID == eventGatewayID
		})).
		Return(&sdkkonnectops.ListEventGatewayVirtualClustersResponse{
			ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{
				Data: []sdkkonnectcomp.VirtualCluster{{ID: virtualClusterID, Name: "vc"}},
			},
		}, nil).
		Maybe()
	sdk.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
		ListEventGatewayVirtualClusterProducePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
			GatewayID: eventGatewayID, VirtualClusterID: virtualClusterID,
		}).
		RunAndReturn(func(context.Context, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest, ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse, error) {
			body := `[{"id":"` + policyID + `","name":"validate","type":"schema_validation","config":{"type":"confluent_schema_registry",` +
				`"schema_registry":{"id":"` + registryID + `"},"value_validation_action":"reject"}}]`
			return &sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse{
				RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))},
			}, nil
		}).
		Maybe()
	sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
		ListEventGatewayVirtualClusterConsumePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesRequest{
			GatewayID: eventGatewayID, VirtualClusterID: virtualClusterID,
		}).
		RunAndReturn(func(context.Context, sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesRequest, ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesResponse, error) {
			return &sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesResponse{
				RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(`[]`))},
			}, nil
		}).
		Maybe()

	// Konnect refuses the deletion while the policy exists.
	var policyGone atomic.Bool
	sdk.EventGatewaySchemaRegistriesSDK.EXPECT().
		DeleteEventGatewaySchemaRegistry(mock.Anything, eventGatewayID, registryID).
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse, error) {
			if !policyGone.Load() {
				return nil, &sdkkonnecterrs.BadRequestError{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "entity is still in use by at least one policy"}
			}
			return &sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse{}, nil
		})

	require.NoError(t, clientNamespaced.Delete(ctx, registry))
	envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.EventGatewaySchemaRegistry) bool {
		if r.GetName() != registry.GetName() {
			return false
		}
		for _, c := range r.GetConditions() {
			if c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType {
				return c.Status == metav1.ConditionFalse &&
					c.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked &&
					strings.Contains(c.Message, "EventGatewayVirtualClusterProducePolicy "+ns.Name+"/"+policy.Name) &&
					controllerutil.ContainsFinalizer(r, konnect.KonnectCleanupFinalizer)
			}
		}
		return false
	}, "EventGatewaySchemaRegistry should report DeletionBlocked naming the policy")

	// The blocked deletion is retried on a fixed one-minute period, longer than
	// the wait below: it resumes through the watch on the policies.
	policyGone.Store(true)
	require.NoError(t, clientNamespaced.Delete(ctx, policy))
	envtest.WatchFor(t, ctx, w, apiwatch.Deleted,
		envtest.ObjectMatchesName(registry),
		"EventGatewaySchemaRegistry should be deleted once no policy references it",
	)
}
