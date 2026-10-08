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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	sdkwrappermocks "github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestDeleteEventGatewayStaticKeyGuarded(t *testing.T) {
	t.Parallel()

	const (
		gatewayID     = "gateway-1"
		staticKeyID   = "static-key-id"
		staticKeyName = "encryption-key"
	)

	newStaticKey := func() *configurationv1alpha1.EventGatewayStaticKey {
		obj := &configurationv1alpha1.EventGatewayStaticKey{
			Name:      "static-key",
			Namespace: "default",
		}
		obj.Spec.APISpec.Name = staticKeyName
		obj.SetGatewayID(gatewayID)
		obj.SetKonnectID(staticKeyID)
		return obj
	}
	newClusterPolicy := func(namespace, name, konnectID string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		p := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: name, Namespace: namespace}
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
		staticKeys      *sdkmocks.MockEventGatewayStaticKeysSDK
		virtualClusters *sdkmocks.MockEventGatewayVirtualClustersSDK
		producePolicies *sdkmocks.MockEventGatewayVirtualClusterProducePoliciesSDK
	}
	newSDKs := func(t *testing.T) sdks {
		return sdks{
			staticKeys:      sdkmocks.NewMockEventGatewayStaticKeysSDK(t),
			virtualClusters: sdkmocks.NewMockEventGatewayVirtualClustersSDK(t),
			producePolicies: sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t),
		}
	}
	guardedDelete := func(t *testing.T, s sdks, cl client.Client) error {
		return deleteEventGatewayStaticKeyGuarded(t.Context(), s.staticKeys, s.virtualClusters, s.producePolicies, cl, newStaticKey())
	}
	expectDelete := func(sdk *sdkmocks.MockEventGatewayStaticKeysSDK, err error) {
		var resp *sdkkonnectops.DeleteEventGatewayStaticKeyResponse
		if err == nil {
			resp = &sdkkonnectops.DeleteEventGatewayStaticKeyResponse{}
		}
		sdk.EXPECT().
			DeleteEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(resp, err).
			Once()
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
	// expectPolicies mirrors the SDK: the decoded policies drop their config,
	// which is only available in the raw response body.
	expectPolicies := func(sdk *sdkmocks.MockEventGatewayVirtualClusterProducePoliciesSDK, vcID, body string) {
		sdk.EXPECT().
			ListEventGatewayVirtualClusterProducePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
				GatewayID:        gatewayID,
				VirtualClusterID: vcID,
			}).
			Return(&sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse{
				RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))},
			}, nil).
			Once()
	}
	vc := func(id, name string) sdkkonnectcomp.VirtualCluster {
		return sdkkonnectcomp.VirtualCluster{ID: id, Name: name}
	}
	const (
		encryptByID = `[{"id":"policy-a-id","name":"encrypt-a","type":"encrypt","config":{"failure_mode":"error","part_of_record":["value"],` +
			`"encryption_key":{"type":"static","key":{"id":"static-key-id"}}}}]`
		encryptFieldsByName = `[{"id":"policy-b-id","name":"encrypt-fields-b","type":"encrypt_fields","config":{"encrypt_fields":[` +
			`{"field":"a","encryption_key":{"type":"aws","arn":"arn:aws:kms:x"}},` +
			`{"field":"b","encryption_key":{"type":"static","key":{"name":"encryption-key"}}}]}}]`
		notUsing = `[{"id":"policy-c-id","name":"encrypt-c","type":"encrypt","config":{"encryption_key":{"type":"static","key":{"id":"other-key-id"}}}},` +
			`{"id":"policy-d-id","name":"modify-headers","type":"modify_headers","config":{"actions":[]}}]`
	)

	t.Run("deletes the static key", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, nil)

		require.NoError(t, guardedDelete(t, s, newClient(t)))
	})

	t.Run("reports the produce policies using the static key when Konnect refuses the deletion", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"), vc("vc-2", "payments"))
		expectPolicies(s.producePolicies, "vc-1", encryptByID)
		expectPolicies(s.producePolicies, "vc-2", encryptFieldsByName)
		cl := newClient(t,
			newClusterPolicy("default", "encrypt-a", "policy-a-id"),
		)

		err := guardedDelete(t, s, cl)
		inUse, ok := errors.AsType[EventGatewayStaticKeyInUseError](err)
		require.True(t, ok, "expected EventGatewayStaticKeyInUseError, got %v", err)
		assert.Equal(t, []string{"default/encrypt-a"}, inUse.Users)
		assert.Equal(t, []string{"payments/encrypt-fields-b"}, inUse.UnmanagedKonnectPolicies)
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, isBlocked, "the error must be a DeletionBlockedError")
		assert.Equal(t,
			"deletion blocked: the static key is in use by EventGatewayVirtualClusterProducePolicy default/encrypt-a, "+
				"and by Konnect produce policies payments/encrypt-fields-b, which are not managed from this cluster; "+
				"delete them or stop using the static key and the deletion will proceed automatically",
			inUse.DeletionBlockedMessage(),
		)
	})

	t.Run("counts but does not name produce policies in other namespaces", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectPolicies(s.producePolicies, "vc-1", encryptByID)
		cl := newClient(t, newClusterPolicy("team-b", "encrypt-a", "policy-a-id"))

		err := guardedDelete(t, s, cl)
		inUse, ok := errors.AsType[EventGatewayStaticKeyInUseError](err)
		require.True(t, ok, "expected EventGatewayStaticKeyInUseError, got %v", err)
		assert.Empty(t, inUse.Users)
		assert.Equal(t, 1, inUse.OtherNamespacesUsers)
		assert.NotContains(t, inUse.DeletionBlockedMessage(), "team-b")
	})

	t.Run("returns the Konnect error when no produce policy uses the static key", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectPolicies(s.producePolicies, "vc-1", notUsing)

		err := guardedDelete(t, s, newClient(t))
		require.Error(t, err)
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, isBlocked)
		var badReq *sdkkonnecterrs.BadRequestError
		assert.ErrorAs(t, err, &badReq)
	})

	t.Run("returns the Konnect error when probing the produce policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		s.virtualClusters.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, mock.Anything).
			Return(nil, errors.New("konnect unavailable")).
			Once()

		err := guardedDelete(t, s, newClient(t))
		require.Error(t, err)
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, isBlocked)
	})

	vcPage := func(next *string, vcs ...sdkkonnectcomp.VirtualCluster) *sdkkonnectops.ListEventGatewayVirtualClustersResponse {
		return &sdkkonnectops.ListEventGatewayVirtualClustersResponse{
			ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{
				Data: vcs,
				Meta: &sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: next}},
			},
		}
	}
	pageAfter := func(cursor string) any {
		return mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayVirtualClustersRequest) bool {
			if cursor == "" {
				return req.PageAfter == nil
			}
			return req.PageAfter != nil && *req.PageAfter == cursor
		})
	}
	const nextCursor1 = "https://us.api.konghq.com/v1/event-gateways/gateway-1/virtual-clusters?page%5Bafter%5D=cursor-1"

	t.Run("probes the virtual clusters of every page", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		s.virtualClusters.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, pageAfter("")).
			Return(vcPage(new(nextCursor1), vc("vc-1", "orders")), nil).
			Once()
		s.virtualClusters.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, pageAfter("cursor-1")).
			Return(vcPage(nil, vc("vc-2", "payments")), nil).
			Once()
		expectPolicies(s.producePolicies, "vc-1", notUsing)
		expectPolicies(s.producePolicies, "vc-2", encryptByID)

		err := guardedDelete(t, s, newClient(t))
		inUse, ok := errors.AsType[EventGatewayStaticKeyInUseError](err)
		require.True(t, ok, "expected EventGatewayStaticKeyInUseError, got %v", err)
		assert.Equal(t, []string{"payments/encrypt-a"}, inUse.UnmanagedKonnectPolicies)
	})

	t.Run("returns the Konnect error when the virtual clusters pagination repeats a cursor", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		s.virtualClusters.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, mock.Anything).
			Return(vcPage(new(nextCursor1)), nil).
			Times(2)

		err := guardedDelete(t, s, newClient(t))
		var badReq *sdkkonnecterrs.BadRequestError
		require.ErrorAs(t, err, &badReq)
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, isBlocked)
	})

	t.Run("skips virtual clusters deleted while probing", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"), vc("vc-2", "payments"))
		s.producePolicies.EXPECT().
			ListEventGatewayVirtualClusterProducePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
				GatewayID:        gatewayID,
				VirtualClusterID: "vc-1",
			}).
			Return(nil, &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "Not Found"}).
			Once()
		expectPolicies(s.producePolicies, "vc-2", encryptByID)

		err := guardedDelete(t, s, newClient(t))
		inUse, ok := errors.AsType[EventGatewayStaticKeyInUseError](err)
		require.True(t, ok, "expected EventGatewayStaticKeyInUseError, got %v", err)
		assert.Equal(t, []string{"payments/encrypt-a"}, inUse.UnmanagedKonnectPolicies)
	})

	t.Run("does not report policies as unmanaged when listing the cluster's policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectDelete(s.staticKeys, badRequest())
		expectVirtualClusters(s.virtualClusters, vc("vc-1", "orders"))
		expectPolicies(s.producePolicies, "vc-1", encryptByID)
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
		var badReq *sdkkonnecterrs.BadRequestError
		require.ErrorAs(t, err, &badReq)
		_, isInUse := errors.AsType[EventGatewayStaticKeyInUseError](err)
		assert.False(t, isInUse)
	})

	t.Run("does not probe on errors other than a bad request", func(t *testing.T) {
		t.Parallel()

		// No list expectation: probing must not happen.
		s := newSDKs(t)
		expectDelete(s.staticKeys, errors.New("connection reset"))

		require.ErrorContains(t, guardedDelete(t, s, newClient(t)), "connection reset")
	})

	t.Run("ops.Delete reports the blocked deletion", func(t *testing.T) {
		t.Parallel()

		sdk := sdkwrappermocks.NewMockSDKWrapperWithT(t)
		expectDelete(sdk.EventGatewayStaticKeysSDK, badRequest())
		expectVirtualClusters(sdk.EventGatewayVirtualClustersSDK, vc("vc-1", "orders"))
		expectPolicies(sdk.EventGatewayVirtualClusterProducePoliciesSDK, "vc-1", encryptByID)

		err := Delete(t.Context(), sdk, newClient(t), &metricsmocks.MockRecorder{}, newStaticKey())
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, isBlocked, "expected a DeletionBlockedError, got %v", err)
	})
}

// TestProducePolicyConfigsWithRealSDK checks, against the real SDK client, the
// contract the static key guard relies on: the SDK drops a produce policy's
// config when decoding (it models it as an empty struct) but restores the raw
// response body, from which the config is read.
func TestProducePolicyConfigsWithRealSDK(t *testing.T) {
	t.Parallel()

	const body = `[{"id":"policy-a-id","name":"encrypt-a","type":"encrypt","config":{"encryption_key":{"type":"static","key":{"id":"static-key-id"}}}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/event-gateways/gateway-1/virtual-clusters/vc-1/produce-policies" {
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

	policies, err := producePolicyConfigs(t.Context(), sdk.EventGatewayVirtualClusterProducePolicies, "gateway-1", "vc-1")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, "policy-a-id", policies[0].ID)
	assert.True(t, referencesStaticKey(policies[0].Config, "static-key-id", ""), "the config must be read from the raw body")
}

func TestReferencesStaticKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "encrypt policy by ID", config: `{"encryption_key":{"type":"static","key":{"id":"key-id"}}}`, want: true},
		{name: "encrypt policy by name", config: `{"encryption_key":{"type":"static","key":{"name":"key-name"}}}`, want: true},
		{name: "encrypt fields policy", config: `{"encrypt_fields":[{"encryption_key":{"type":"aws","arn":"x"}},{"encryption_key":{"type":"static","key":{"id":"key-id"}}}]}`, want: true},
		{name: "other static key", config: `{"encryption_key":{"type":"static","key":{"id":"other-id","name":"other-name"}}}`, want: false},
		{name: "AWS key", config: `{"encryption_key":{"type":"aws","arn":"key-id"}}`, want: false},
		{name: "a matching ID outside a static key", config: `{"key":{"id":"key-id"}}`, want: false},
		{name: "no config", config: `null`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config any
			require.NoError(t, json.Unmarshal([]byte(tt.config), &config))
			assert.Equal(t, tt.want, referencesStaticKey(config, "key-id", "key-name"))
		})
	}
}

// TestProducePolicyStaticKeyReferences verifies that a produce policy's static
// key references resolve in the SDK request: namespacedRef members become the
// EventGatewayStaticKey's Konnect ID, in the encrypt policy's single key and
// in each field of an encrypt fields policy, while Konnect IDs and names are
// sent as they are, in order.
func TestProducePolicyStaticKeyReferences(t *testing.T) {
	t.Parallel()

	staticKey := &configurationv1alpha1.EventGatewayStaticKey{Name: "static-key", Namespace: "default"}
	staticKey.SetGatewayID("gateway-1")
	staticKey.SetKonnectID("static-key-id")
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(staticKey).Build()
	byNamespacedRef := &configurationv1alpha1.EncryptionKeyStaticReference{
		NamespacedRef: &configurationv1alpha1.EventGatewayStaticKeyRef{Name: "static-key"},
	}
	newPolicy := func(config configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		p := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: "policy", Namespace: "default"}
		p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &config
		p.SetGatewayID("gateway-1")
		return p
	}
	requestBody := func(t *testing.T, policy *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) map[string]any {
		t.Helper()
		req, err := policy.ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		data, err := json.Marshal(req.EventGatewayProducePolicyCreate)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(data, &body))
		return body
	}

	t.Run("encrypt policy", func(t *testing.T) {
		t.Parallel()

		body := requestBody(t, newPolicy(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
			Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy,
			EncryptPolicy: &configurationv1alpha1.EventGatewayEncryptPolicy{
				Name: "encrypt",
				Config: configurationv1alpha1.EventGatewayEncryptConfig{
					FailureMode:  "error",
					PartOfRecord: []configurationv1alpha1.EncryptionRecordPart{"value"},
					EncryptionKey: &configurationv1alpha1.EventGatewayEncryptConfigEncryptionKey{
						Type:   configurationv1alpha1.EventGatewayEncryptConfigEncryptionKeyTypeStatic,
						Static: &configurationv1alpha1.EncryptionKeyStatic{Key: byNamespacedRef},
					},
				},
			},
		}))
		config := body["config"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "static", "key": map[string]any{"id": "static-key-id"}}, config["encryption_key"])
	})

	t.Run("encrypt fields policy", func(t *testing.T) {
		t.Parallel()

		static := func(key *configurationv1alpha1.EncryptionKeyStaticReference) configurationv1alpha1.EventGatewayParsedRecordEncryptionSelector {
			return configurationv1alpha1.EventGatewayParsedRecordEncryptionSelector{
				EncryptionKey: &configurationv1alpha1.EventGatewayParsedRecordEncryptionSelectorEncryptionKey{
					Type:   configurationv1alpha1.EventGatewayParsedRecordEncryptionSelectorEncryptionKeyTypeStatic,
					Static: &configurationv1alpha1.EncryptionKeyStatic{Key: key},
				},
			}
		}
		// Encrypt fields policies require a parent_policy_id, which the CRD
		// doesn't expose, so a full SDK request can't be built for them:
		// check the references are collected and resolved.
		policy := newPolicy(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
			Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate,
			ParsedRecordEncryptFieldsPolicyCreate: &configurationv1alpha1.EventGatewayParsedRecordEncryptFieldsPolicyCreate{
				Name: "encrypt-fields",
				Config: configurationv1alpha1.EventGatewayParsedRecordEncryptFieldsConfig{
					FailureMode: "error",
					EncryptFields: []configurationv1alpha1.EventGatewayParsedRecordEncryptionSelector{
						static(&configurationv1alpha1.EncryptionKeyStaticReference{ID: new("konnect-key-id")}),
						static(byNamespacedRef),
						static(&configurationv1alpha1.EncryptionKeyStaticReference{Name: new("konnect-key-name")}),
					},
				},
			},
		})
		assert.Equal(t,
			[]configurationv1alpha1.EventGatewayStaticKeyRef{{Name: "static-key"}},
			configurationv1alpha1.RefsAtEventGatewayVirtualClusterProducePolicyEncryptFieldsConfigEncryptFieldsEncryptionKeyStaticKey(policy),
		)
		assert.Equal(t,
			[]client.ObjectKey{{Namespace: "default", Name: "static-key"}},
			configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayStaticKey(policy),
		)
		require.NoError(t, policy.ResolveKonnectReferences(t.Context(), cl))
	})

	// CEL rejects a static key set alongside another key type, but objects
	// that bypass it (e.g. stored before the rule) must not have the stale
	// reference collected, nor attributed to another element.
	t.Run("ignores static key references of elements using another key type", func(t *testing.T) {
		t.Parallel()

		aws := configurationv1alpha1.EventGatewayParsedRecordEncryptionSelector{
			EncryptionKey: &configurationv1alpha1.EventGatewayParsedRecordEncryptionSelectorEncryptionKey{
				Type:   configurationv1alpha1.EventGatewayParsedRecordEncryptionSelectorEncryptionKeyTypeAWS,
				AWS:    &configurationv1alpha1.EncryptionKeyAWS{Arn: "arn:aws:kms:us-east-1:000000000000:key/k"},
				Static: &configurationv1alpha1.EncryptionKeyStatic{Key: byNamespacedRef},
			},
		}
		policy := newPolicy(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
			Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate,
			ParsedRecordEncryptFieldsPolicyCreate: &configurationv1alpha1.EventGatewayParsedRecordEncryptFieldsPolicyCreate{
				Name: "encrypt-fields",
				Config: configurationv1alpha1.EventGatewayParsedRecordEncryptFieldsConfig{
					FailureMode:   "error",
					EncryptFields: []configurationv1alpha1.EventGatewayParsedRecordEncryptionSelector{aws},
				},
			},
		})
		assert.Empty(t, configurationv1alpha1.RefsAtEventGatewayVirtualClusterProducePolicyEncryptFieldsConfigEncryptFieldsEncryptionKeyStaticKey(policy))
		assert.Empty(t, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayStaticKey(policy))
	})

	t.Run("sends only the key type in use for an encrypt policy", func(t *testing.T) {
		t.Parallel()

		policy := newPolicy(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
			Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy,
			EncryptPolicy: &configurationv1alpha1.EventGatewayEncryptPolicy{
				Name: "encrypt",
				Config: configurationv1alpha1.EventGatewayEncryptConfig{
					FailureMode:  "error",
					PartOfRecord: []configurationv1alpha1.EncryptionRecordPart{"value"},
					EncryptionKey: &configurationv1alpha1.EventGatewayEncryptConfigEncryptionKey{
						Type:   configurationv1alpha1.EventGatewayEncryptConfigEncryptionKeyTypeAWS,
						AWS:    &configurationv1alpha1.EncryptionKeyAWS{Arn: "arn:aws:kms:us-east-1:000000000000:key/k"},
						Static: &configurationv1alpha1.EncryptionKeyStatic{Key: byNamespacedRef},
					},
				},
			},
		})
		assert.Empty(t, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayStaticKey(policy))
		config := requestBody(t, policy)["config"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "aws", "arn": "arn:aws:kms:us-east-1:000000000000:key/k"}, config["encryption_key"])
	})
}

func TestUpdateEventGatewayStaticKey(t *testing.T) {
	t.Parallel()

	const (
		gatewayID   = "gateway-1"
		staticKeyID = "static-key-id"
		keyValue    = "Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy1vayE="
	)
	newStaticKey := func() *configurationv1alpha1.EventGatewayStaticKey {
		obj := &configurationv1alpha1.EventGatewayStaticKey{Name: "static-key", Namespace: "default"}
		obj.Spec.APISpec.Name = "encryption-key"
		obj.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
			Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
			SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "key-secret", Key: "key"},
		}
		obj.SetGatewayID(gatewayID)
		obj.SetKonnectID(staticKeyID)
		return obj
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build()
	}
	secret := &corev1.Secret{
		Name: "key-secret", Namespace: "default",
		Data: map[string][]byte{"key": []byte(keyValue)},
	}
	notFound := func() error {
		return &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "Not Found"}
	}

	t.Run("does nothing while the static key exists in Konnect", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().
			GetEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(&sdkkonnectops.GetEventGatewayStaticKeyResponse{}, nil).
			Once()

		obj := newStaticKey()
		require.NoError(t, updateEventGatewayStaticKey(t.Context(), newClient(t), sdk, obj))
		assert.Equal(t, staticKeyID, obj.GetKonnectID())
	})

	t.Run("recreates the static key deleted from Konnect out of band", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().
			GetEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(nil, notFound()).
			Once()
		sdk.EXPECT().
			CreateEventGatewayStaticKey(mock.Anything, gatewayID, mock.MatchedBy(func(req *sdkkonnectcomp.EventGatewayStaticKeyCreate) bool {
				return req.Name == "encryption-key" && req.Value == keyValue
			})).
			Return(&sdkkonnectops.CreateEventGatewayStaticKeyResponse{
				EventGatewayStaticKey: &sdkkonnectcomp.EventGatewayStaticKey{ID: "recreated-id"},
			}, nil).
			Once()

		obj := newStaticKey()
		require.NoError(t, updateEventGatewayStaticKey(t.Context(), newClient(t, secret), sdk, obj))
		assert.Equal(t, "recreated-id", obj.GetKonnectID())
	})

	// The SDK error Konnect returns when a static key with the same name exists.
	nameTaken := func(t *testing.T) error {
		var err sdkkonnecterrs.BadRequestError
		require.NoError(t, json.Unmarshal([]byte(`{"status":400,"title":"Bad Request","detail":"Bad Request: name: must be unique",`+
			`"invalid_parameters":[{"field":"name","reason":"must be unique","source":"body"}]}`), &err))
		return &err
	}
	expectRecreateNameTaken := func(t *testing.T, sdk *sdkmocks.MockEventGatewayStaticKeysSDK, keys ...sdkkonnectcomp.EventGatewayStaticKey) {
		sdk.EXPECT().
			GetEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(nil, notFound()).
			Once()
		sdk.EXPECT().
			CreateEventGatewayStaticKey(mock.Anything, gatewayID, mock.Anything).
			Return(nil, nameTaken(t)).
			Once()
		sdk.EXPECT().
			ListEventGatewayStaticKeys(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.ListEventGatewayStaticKeysResponse{
				ListEventGatewayStaticKeysResponse: &sdkkonnectcomp.ListEventGatewayStaticKeysResponse{Data: keys},
			}, nil).
			Once()
	}

	// A recreation whose new ID was lost (e.g. the status update conflicted)
	// is retried while the recreated key holds the name.
	t.Run("adopts its own key recreated earlier when the name is taken", func(t *testing.T) {
		t.Parallel()

		obj := newStaticKey()
		obj.SetUID("static-key-uid")
		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		expectRecreateNameTaken(t, sdk,
			sdkkonnectcomp.EventGatewayStaticKey{ID: "other-id", Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"}},
			sdkkonnectcomp.EventGatewayStaticKey{ID: "recreated-id", Labels: map[string]string{KubernetesUIDLabelKey: "static-key-uid"}},
		)

		require.NoError(t, updateEventGatewayStaticKey(t.Context(), newClient(t, secret), sdk, obj))
		assert.Equal(t, "recreated-id", obj.GetKonnectID())
	})

	t.Run("fails when the name is taken by another static key", func(t *testing.T) {
		t.Parallel()

		obj := newStaticKey()
		obj.SetUID("static-key-uid")
		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		expectRecreateNameTaken(t, sdk,
			sdkkonnectcomp.EventGatewayStaticKey{ID: "other-id", Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"}},
		)

		require.ErrorContains(t, updateEventGatewayStaticKey(t.Context(), newClient(t, secret), sdk, obj), "must be unique")
		assert.Equal(t, staticKeyID, obj.GetKonnectID())
	})

	t.Run("fails to recreate the static key when its Secret is gone", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().
			GetEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(nil, notFound()).
			Once()

		obj := newStaticKey()
		require.Error(t, updateEventGatewayStaticKey(t.Context(), newClient(t), sdk, obj))
		assert.Equal(t, staticKeyID, obj.GetKonnectID())
	})

	t.Run("returns other Konnect errors", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockEventGatewayStaticKeysSDK(t)
		sdk.EXPECT().
			GetEventGatewayStaticKey(mock.Anything, gatewayID, staticKeyID).
			Return(nil, &sdkkonnecterrs.SDKError{StatusCode: http.StatusInternalServerError, Message: "boom"}).
			Once()

		require.Error(t, updateEventGatewayStaticKey(t.Context(), newClient(t), sdk, newStaticKey()))
	})
}
