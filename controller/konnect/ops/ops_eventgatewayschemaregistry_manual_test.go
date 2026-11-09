package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	sdkwrappermocks "github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestDeleteEventGatewaySchemaRegistryGuarded(t *testing.T) {
	t.Parallel()

	const (
		gatewayID    = "gateway-1"
		registryID   = "registry-id"
		registryName = "orders-in-konnect"
	)

	newRegistry := func() *configurationv1alpha1.EventGatewaySchemaRegistry {
		obj := &configurationv1alpha1.EventGatewaySchemaRegistry{Name: "orders", Namespace: "default"}
		obj.Spec.APISpec.EventGatewaySchemaRegistryConfig = &configurationv1alpha1.EventGatewaySchemaRegistryConfig{
			Type: configurationv1alpha1.EventGatewaySchemaRegistryConfigTypeSchemaRegistryConfluent,
			SchemaRegistryConfluent: &configurationv1alpha1.SchemaRegistryConfluent{
				Name: registryName,
			},
		}
		obj.SetGatewayID(gatewayID)
		obj.SetKonnectID(registryID)
		return obj
	}
	newProducePolicy := func(namespace, name, konnectID string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		p := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: name, Namespace: namespace}
		p.SetKonnectID(konnectID)
		return p
	}
	newConsumePolicy := func(namespace, name, konnectID string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
		p := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: name, Namespace: namespace}
		p.SetKonnectID(konnectID)
		return p
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build()
	}
	// Each subtest gets its own error instance: the ops layer mutates it.
	badRequest := func() error {
		return &sdkkonnecterrs.BadRequestError{
			Status: http.StatusBadRequest,
			Title:  "Bad Request",
			Detail: "server wording is not part of the contract",
		}
	}
	type sdks struct {
		registries      *sdkmocks.MockEventGatewaySchemaRegistriesSDK
		virtualClusters *sdkmocks.MockEventGatewayVirtualClustersSDK
		producePolicies *sdkmocks.MockEventGatewayVirtualClusterProducePoliciesSDK
		consumePolicies *sdkmocks.MockEventGatewayVirtualClusterConsumePoliciesSDK
	}
	newSDKs := func(t *testing.T) sdks {
		return sdks{
			registries:      sdkmocks.NewMockEventGatewaySchemaRegistriesSDK(t),
			virtualClusters: sdkmocks.NewMockEventGatewayVirtualClustersSDK(t),
			producePolicies: sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t),
			consumePolicies: sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t),
		}
	}
	guardedDelete := func(t *testing.T, s sdks, cl client.Client) error {
		return deleteEventGatewaySchemaRegistryGuarded(t.Context(), s.registries, s.virtualClusters, s.producePolicies, s.consumePolicies, cl, newRegistry())
	}
	expectDelete := func(sdk *sdkmocks.MockEventGatewaySchemaRegistriesSDK, err error) {
		var resp *sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse
		if err == nil {
			resp = &sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse{}
		}
		sdk.EXPECT().DeleteEventGatewaySchemaRegistry(mock.Anything, gatewayID, registryID).Return(resp, err).Once()
	}
	expectVirtualClusters := func(sdk *sdkmocks.MockEventGatewayVirtualClustersSDK, vcs ...sdkkonnectcomp.VirtualCluster) {
		sdk.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayVirtualClustersRequest) bool {
				return req.GatewayID == gatewayID && req.PageAfter == nil
			})).
			Return(&sdkkonnectops.ListEventGatewayVirtualClustersResponse{
				ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{Data: vcs},
			}, nil).
			Once()
	}
	// expectProduce and expectConsume mirror the SDK: the decoded policies drop
	// their config, which is only available in the raw response body.
	expectProduce := func(sdk *sdkmocks.MockEventGatewayVirtualClusterProducePoliciesSDK, vcID, body string) {
		sdk.EXPECT().
			ListEventGatewayVirtualClusterProducePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
				GatewayID: gatewayID, VirtualClusterID: vcID,
			}).
			Return(&sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse{RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))}}, nil).
			Once()
	}
	expectConsume := func(sdk *sdkmocks.MockEventGatewayVirtualClusterConsumePoliciesSDK, vcID, body string) {
		sdk.EXPECT().
			ListEventGatewayVirtualClusterConsumePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesRequest{
				GatewayID: gatewayID, VirtualClusterID: vcID,
			}).
			Return(&sdkkonnectops.ListEventGatewayVirtualClusterConsumePoliciesResponse{RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))}}, nil).
			Once()
	}
	vc := func(id, name string) sdkkonnectcomp.VirtualCluster {
		return sdkkonnectcomp.VirtualCluster{ID: id, Name: name}
	}
	const (
		produceValidateByID = `[{"id":"produce-a-id","name":"validate","type":"schema_validation","config":{"type":"confluent_schema_registry",` +
			`"schema_registry":{"id":"registry-id"},"value_validation_action":"reject"}}]`
		consumeTranscodeByName = `[{"id":"consume-b-id","name":"transcode","type":"transcode","config":{"avro":{"schema_source":` +
			`{"type":"reference","reference":{"schema_registry":{"name":"orders-in-konnect"}}}}}}]`
		produceNotUsing = `[{"id":"produce-c-id","name":"validate-other","type":"schema_validation","config":{"type":"confluent_schema_registry",` +
			`"schema_registry":{"id":"other-registry-id"}}},{"id":"produce-d-id","name":"modify-headers","type":"modify_headers","config":{"actions":[]}}]`
		none = `[]`
	)

	t.Run("deletes the schema registry", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, nil)
		require.NoError(t, guardedDelete(t, s, newClient(t)))
	})

	t.Run("reports the policies using the schema registry when Konnect refuses the deletion", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"), vc("vc-2", "payments"))
		expectProduce(s.producePolicies, "vc-1", produceValidateByID)
		expectConsume(s.consumePolicies, "vc-1", none)
		expectProduce(s.producePolicies, "vc-2", produceNotUsing)
		expectConsume(s.consumePolicies, "vc-2", consumeTranscodeByName)

		err := guardedDelete(t, s, newClient(t,
			newProducePolicy("default", "validate", "produce-a-id"),
			newProducePolicy("default", "validate-other", "produce-c-id"),
		))
		inUse, ok := errors.AsType[EventGatewaySchemaRegistryInUseError](err)
		require.True(t, ok, "expected EventGatewaySchemaRegistryInUseError, got %v", err)
		assert.Equal(t, []string{"default/validate"}, inUse.ProduceUsers)
		assert.Empty(t, inUse.ConsumeUsers)
		assert.Equal(t, []string{"payments/transcode"}, inUse.UnmanagedConsumePolicies)
		assert.Equal(t, "deletion blocked: the schema registry is in use by EventGatewayVirtualClusterProducePolicy default/validate, "+
			"and by Konnect consume policies payments/transcode, which are not managed from this cluster; "+
			"delete them or stop using the schema registry and the deletion will proceed automatically",
			inUse.DeletionBlockedMessage())
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, blocked, "the reconciler reports it as DeletionBlocked")
		var badReq *sdkkonnecterrs.BadRequestError
		assert.ErrorAs(t, err, &badReq)
	})

	t.Run("counts but does not name policies in other namespaces", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectProduce(s.producePolicies, "vc-1", none)
		expectConsume(s.consumePolicies, "vc-1", strings.ReplaceAll(consumeTranscodeByName, "orders-in-konnect", registryName))

		err := guardedDelete(t, s, newClient(t, newConsumePolicy("other", "transcode", "consume-b-id")))
		inUse, ok := errors.AsType[EventGatewaySchemaRegistryInUseError](err)
		require.True(t, ok, "expected EventGatewaySchemaRegistryInUseError, got %v", err)
		assert.Empty(t, inUse.ConsumeUsers)
		assert.Equal(t, 1, inUse.OtherNamespacesConsumeUsers)
		assert.NotContains(t, inUse.DeletionBlockedMessage(), "other/transcode")
	})

	t.Run("returns the Konnect error when no policy uses the schema registry", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectProduce(s.producePolicies, "vc-1", produceNotUsing)
		expectConsume(s.consumePolicies, "vc-1", none)

		// A policy object referencing it in its spec doesn't count: Konnect
		// refused the deletion for another reason.
		err := guardedDelete(t, s, newClient(t, newProducePolicy("default", "validate", "produce-a-id")))
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
		var badReq *sdkkonnecterrs.BadRequestError
		assert.ErrorAs(t, err, &badReq)
	})

	t.Run("returns the Konnect error when probing the policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectProduce(s.producePolicies, "vc-1", none)
		s.consumePolicies.EXPECT().
			ListEventGatewayVirtualClusterConsumePolicies(mock.Anything, mock.Anything).
			Return(nil, errors.New("konnect unavailable")).
			Once()

		err := guardedDelete(t, s, newClient(t))
		require.Error(t, err)
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
	})

	t.Run("skips virtual clusters deleted while probing", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		notFound := &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "Not Found"}
		s.producePolicies.EXPECT().
			ListEventGatewayVirtualClusterProducePolicies(mock.Anything, mock.Anything).
			Return(nil, notFound).
			Once()
		s.consumePolicies.EXPECT().
			ListEventGatewayVirtualClusterConsumePolicies(mock.Anything, mock.Anything).
			Return(nil, notFound).
			Once()

		err := guardedDelete(t, s, newClient(t))
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
		var badReq *sdkkonnecterrs.BadRequestError
		assert.ErrorAs(t, err, &badReq)
	})

	t.Run("does not report policies as unmanaged when listing the cluster's policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.registries, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectProduce(s.producePolicies, "vc-1", produceValidateByID)
		expectConsume(s.consumePolicies, "vc-1", none)
		cl := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return errors.New("cache not synced")
				},
			}).
			Build()

		err := guardedDelete(t, s, cl)
		require.ErrorContains(t, err, "cache not synced")
		_, isInUse := errors.AsType[EventGatewaySchemaRegistryInUseError](err)
		assert.False(t, isInUse)
	})

	t.Run("does not probe on errors other than a bad request", func(t *testing.T) {
		t.Parallel()

		// No list expectation: probing must not happen.
		s := newSDKs(t)
		expectDelete(s.registries, errors.New("connection reset"))

		require.ErrorContains(t, guardedDelete(t, s, newClient(t)), "connection reset")
	})

	t.Run("ops.Delete reports the blocked deletion", func(t *testing.T) {
		t.Parallel()

		sdk := sdkwrappermocks.NewMockSDKWrapperWithT(t)
		expectDelete(sdk.EventGatewaySchemaRegistriesSDK, badRequest())
		expectVirtualClusters(sdk.EventGatewayVirtualClustersSDK, vc("vc-1", "orders"))
		expectProduce(sdk.EventGatewayVirtualClusterProducePoliciesSDK, "vc-1", produceValidateByID)
		expectConsume(sdk.EventGatewayVirtualClusterConsumePoliciesSDK, "vc-1", none)

		err := Delete(t.Context(), sdk, newClient(t), &metricsmocks.MockRecorder{}, newRegistry())
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, blocked, "expected a DeletionBlockedError, got %v", err)
	})
}

// TestConsumePolicyConfigsWithRealSDK checks, against the real SDK client, that
// a consume policy's config is read from the raw response body, as for produce
// policies (see TestProducePolicyConfigsWithRealSDK).
func TestConsumePolicyConfigsWithRealSDK(t *testing.T) {
	t.Parallel()

	const body = `[{"id":"policy-a-id","name":"validate","type":"schema_validation","config":{"type":"confluent_schema_registry",` +
		`"schema_registry":{"id":"registry-id"},"value_validation_action":"skip"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/event-gateways/gateway-1/virtual-clusters/vc-1/consume-policies" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	sdk := sdkkonnectgo.New(
		sdkkonnectgo.WithServerURL(srv.URL),
		sdkkonnectgo.WithSecurity(sdkkonnectcomp.Security{PersonalAccessToken: new("token")}),
	)

	policies, err := consumePolicyConfigs(t.Context(), sdk.EventGatewayVirtualClusterConsumePolicies, "gateway-1", "vc-1")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.True(t, referencesSchemaRegistry(policies[0].Config, "registry-id", ""), "the config must be read from the raw body")
}

func TestReferencesSchemaRegistry(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		config string
		want   bool
	}{
		{name: "by id", config: `{"schema_registry":{"id":"registry-id"}}`, want: true},
		{name: "by name", config: `{"schema_registry":{"name":"orders"}}`, want: true},
		{name: "nested in a transcode schema source", config: `{"json":{"schema_source":{"reference":{"schema_registry":{"name":"orders"}}}}}`, want: true},
		{name: "another registry", config: `{"schema_registry":{"id":"other-id","name":"other"}}`},
		{name: "no registry", config: `{"actions":[]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var config any
			require.NoError(t, json.Unmarshal([]byte(tt.config), &config))
			assert.Equal(t, tt.want, referencesSchemaRegistry(config, "registry-id", "orders"))
		})
	}
}
