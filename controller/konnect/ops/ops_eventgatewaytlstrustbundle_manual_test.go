package ops

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

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

func TestDeleteEventGatewayTLSTrustBundleGuarded(t *testing.T) {
	t.Parallel()

	const (
		gatewayID       = "gateway-1"
		trustBundleID   = "trust-bundle-id"
		trustBundleName = "client-ca"
	)

	newTrustBundle := func() *configurationv1alpha1.EventGatewayTLSTrustBundle {
		obj := &configurationv1alpha1.EventGatewayTLSTrustBundle{
			Name:      "trust-bundle",
			Namespace: "default",
		}
		obj.Spec.APISpec.Name = trustBundleName
		obj.SetGatewayID(gatewayID)
		obj.SetKonnectID(trustBundleID)
		return obj
	}
	newClusterPolicy := func(namespace, name, konnectID string) *configurationv1alpha1.EventGatewayListenerPolicy {
		p := testEventGatewayListenerPolicy()
		p.Namespace = namespace
		p.Name = name
		p.SetKonnectID(konnectID)
		return p
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build()
	}
	notFound := func() error {
		return &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "Not Found"}
	}
	listenersPage := func(next *string, listeners ...sdkkonnectcomp.EventGatewayListener) *sdkkonnectops.ListEventGatewayListenersResponse {
		return &sdkkonnectops.ListEventGatewayListenersResponse{
			ListEventGatewayListenersResponse: &sdkkonnectcomp.ListEventGatewayListenersResponse{
				Data: listeners,
				Meta: &sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: next}},
			},
		}
	}
	// policiesResponse mirrors the SDK: the decoded policies drop their config,
	// which is only available in the raw response body.
	policiesResponse := func(body string) *sdkkonnectops.ListEventGatewayListenerPoliciesResponse {
		return &sdkkonnectops.ListEventGatewayListenerPoliciesResponse{
			RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))},
		}
	}
	// expectGet returns the trust bundle as stored in Konnect, with konnectName.
	expectGet := func(sdk *sdkmocks.MockEventGatewayTLSTrustBundlesSDK, konnectName string) {
		sdk.EXPECT().
			GetEventGatewayTLSTrustBundle(mock.Anything, gatewayID, trustBundleID).
			Return(&sdkkonnectops.GetEventGatewayTLSTrustBundleResponse{
				TLSTrustBundle: &sdkkonnectcomp.TLSTrustBundle{ID: trustBundleID, Name: konnectName},
			}, nil).
			Once()
	}
	expectListeners := func(sdk *sdkmocks.MockEventGatewayListenersSDK, listeners ...sdkkonnectcomp.EventGatewayListener) {
		sdk.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayListenersRequest) bool {
				return req.GatewayID == gatewayID && req.PageAfter == nil
			})).
			Return(listenersPage(nil, listeners...), nil).
			Once()
	}
	expectPolicies := func(sdk *sdkmocks.MockEventGatewayListenerPoliciesSDK, listenerID, body string) {
		sdk.EXPECT().
			ListEventGatewayListenerPolicies(mock.Anything, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
				GatewayID:  gatewayID,
				ListenerID: listenerID,
			}).
			Return(policiesResponse(body), nil).
			Once()
	}
	expectDelete := func(sdk *sdkmocks.MockEventGatewayTLSTrustBundlesSDK) {
		sdk.EXPECT().
			DeleteEventGatewayTLSTrustBundle(mock.Anything, gatewayID, trustBundleID).
			Return(&sdkkonnectops.DeleteEventGatewayTLSTrustBundleResponse{}, nil).
			Once()
	}
	listener := func(id, name string) sdkkonnectcomp.EventGatewayListener {
		return sdkkonnectcomp.EventGatewayListener{ID: id, Name: name}
	}
	type sdks struct {
		trustBundles *sdkmocks.MockEventGatewayTLSTrustBundlesSDK
		listeners    *sdkmocks.MockEventGatewayListenersSDK
		policies     *sdkmocks.MockEventGatewayListenerPoliciesSDK
	}
	newSDKs := func(t *testing.T) sdks {
		return sdks{
			trustBundles: sdkmocks.NewMockEventGatewayTLSTrustBundlesSDK(t),
			listeners:    sdkmocks.NewMockEventGatewayListenersSDK(t),
			policies:     sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t),
		}
	}
	guardedDelete := func(t *testing.T, s sdks, cl client.Client, obj *configurationv1alpha1.EventGatewayTLSTrustBundle) error {
		return deleteEventGatewayTLSTrustBundleGuarded(t.Context(), s.trustBundles, s.listeners, s.policies, cl, obj)
	}
	const (
		policyUsingByID   = `[{"id":"policy-a-id","name":"tls-a","type":"tls_server","config":{"client_authentication":{"mode":"required","tls_trust_bundles":[{"id":"other-bundle-id"},{"id":"trust-bundle-id"}]}}}]`
		policyUsingByName = `[{"id":"policy-b-id","name":"tls-b","type":"tls_server","config":{"client_authentication":{"mode":"required","tls_trust_bundles":[{"name":"client-ca"}]}}}]`
		policiesNotUsing  = `[{"id":"policy-c-id","name":"tls-c","type":"tls_server","config":{"client_authentication":{"mode":"required","tls_trust_bundles":[{"id":"other-bundle-id"},{"name":"other-ca"}]}}},` +
			`{"id":"policy-d-id","name":"tls-d","type":"tls_server","config":{"client_authentication":null}},` +
			`{"id":"policy-e-id","name":"forward","type":"forward_to_virtual_cluster","config":{"type":"port_mapping"}}]`
	)

	t.Run("deletes the trust bundle when no Konnect listener policy references it", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"))
		expectPolicies(s.policies, "listener-1", policiesNotUsing)
		expectDelete(s.trustBundles)

		require.NoError(t, guardedDelete(t, s, newClient(t), newTrustBundle()))
	})

	t.Run("blocks the deletion while Konnect listener policies reference it by ID or name", func(t *testing.T) {
		t.Parallel()

		// No delete expectation: Konnect must not be called.
		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"), listener("listener-2", "l2"))
		expectPolicies(s.policies, "listener-1", policyUsingByID)
		expectPolicies(s.policies, "listener-2", policyUsingByName)
		cl := newClient(t, newClusterPolicy("default", "policy-a", "policy-a-id"))

		err := guardedDelete(t, s, cl, newTrustBundle())
		inUse, ok := errors.AsType[EventGatewayTLSTrustBundleInUseError](err)
		require.True(t, ok, "expected EventGatewayTLSTrustBundleInUseError, got %v", err)
		assert.Equal(t, []string{"default/policy-a"}, inUse.Users)
		assert.Equal(t, []string{"l2/tls-b"}, inUse.UnmanagedKonnectPolicies)
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, isBlocked, "the error must be a DeletionBlockedError")
		assert.Equal(t,
			"deletion blocked: the TLS trust bundle is in use by EventGatewayListenerPolicy default/policy-a, "+
				"and by Konnect listener policies l2/tls-b, which are not managed from this cluster; "+
				"delete them or remove their references to it and the deletion will proceed automatically",
			inUse.DeletionBlockedMessage(),
		)
	})

	t.Run("matches the name stored in Konnect while a rename is pending", func(t *testing.T) {
		t.Parallel()

		// The spec was renamed to client-ca but Konnect still has old-ca,
		// which the policy references.
		s := newSDKs(t)
		expectGet(s.trustBundles, "old-ca")
		expectListeners(s.listeners, listener("listener-1", "l1"))
		expectPolicies(s.policies, "listener-1",
			`[{"id":"policy-f-id","name":"tls-f","type":"tls_server","config":{"client_authentication":{"mode":"required","tls_trust_bundles":[{"name":"old-ca"}]}}}]`)

		err := guardedDelete(t, s, newClient(t), newTrustBundle())
		inUse, ok := errors.AsType[EventGatewayTLSTrustBundleInUseError](err)
		require.True(t, ok, "expected EventGatewayTLSTrustBundleInUseError, got %v", err)
		assert.Equal(t, []string{"l1/tls-f"}, inUse.UnmanagedKonnectPolicies)
	})

	t.Run("counts but does not name policies in other namespaces", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"))
		expectPolicies(s.policies, "listener-1", policyUsingByID)
		cl := newClient(t, newClusterPolicy("team-b", "policy-a", "policy-a-id"))

		err := guardedDelete(t, s, cl, newTrustBundle())
		inUse, ok := errors.AsType[EventGatewayTLSTrustBundleInUseError](err)
		require.True(t, ok, "expected EventGatewayTLSTrustBundleInUseError, got %v", err)
		assert.Empty(t, inUse.Users)
		assert.Equal(t, 1, inUse.OtherNamespacesUsers)
		assert.NotContains(t, inUse.DeletionBlockedMessage(), "team-b")
		assert.Contains(t, inUse.DeletionBlockedMessage(), "1 EventGatewayListenerPolicy in other namespaces")
	})

	t.Run("a policy referencing the trust bundle only in its spec does not block the deletion", func(t *testing.T) {
		t.Parallel()

		// A programmed policy in another namespace names the trust bundle
		// through namespacedRef: the reference is rejected and never reaches
		// Konnect, so it must not block the deletion.
		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"))
		expectPolicies(s.policies, "listener-1", policiesNotUsing)
		expectDelete(s.trustBundles)
		other := newClusterPolicy("team-b", "other", "policy-c-id")
		other.Spec.APISpec.EventGatewayTLSListen.Config.ClientAuthentication.TLSTrustBundles = []configurationv1alpha1.TLSTrustBundleReference{{
			NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle", Namespace: "default"},
		}}

		require.NoError(t, guardedDelete(t, s, newClient(t, other), newTrustBundle()))
	})

	t.Run("deletes when the trust bundle is already gone from Konnect", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		s.trustBundles.EXPECT().
			GetEventGatewayTLSTrustBundle(mock.Anything, gatewayID, trustBundleID).
			Return(nil, notFound()).
			Once()
		// The generated delete treats the not found response as deleted.
		s.trustBundles.EXPECT().
			DeleteEventGatewayTLSTrustBundle(mock.Anything, gatewayID, trustBundleID).
			Return(nil, notFound()).
			Once()

		require.NoError(t, guardedDelete(t, s, newClient(t), newTrustBundle()))
	})

	t.Run("deletes when the gateway is gone from Konnect while listing its listeners", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		s.listeners.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.Anything).
			Return(nil, notFound()).
			Once()
		expectDelete(s.trustBundles)

		require.NoError(t, guardedDelete(t, s, newClient(t), newTrustBundle()))
	})

	t.Run("skips a listener deleted while listing its policies", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"), listener("listener-2", "l2"))
		s.policies.EXPECT().
			ListEventGatewayListenerPolicies(mock.Anything, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
				GatewayID:  gatewayID,
				ListenerID: "listener-1",
			}).
			Return(nil, notFound()).
			Once()
		expectPolicies(s.policies, "listener-2", policyUsingByName)

		err := guardedDelete(t, s, newClient(t), newTrustBundle())
		inUse, ok := errors.AsType[EventGatewayTLSTrustBundleInUseError](err)
		require.True(t, ok, "expected EventGatewayTLSTrustBundleInUseError, got %v", err)
		assert.Equal(t, []string{"l2/tls-b"}, inUse.UnmanagedKonnectPolicies)
	})

	t.Run("reports the missing gateway ID without calling Konnect", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		obj := newTrustBundle()
		obj.Status.GatewayID = nil

		err := guardedDelete(t, s, newClient(t), obj)
		var parentErr CantPerformOperationWithoutParentIDError
		require.ErrorAs(t, err, &parentErr)
	})

	t.Run("follows the listeners pagination", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		s.listeners.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayListenersRequest) bool {
				return req.PageAfter == nil
			})).
			Return(listenersPage(new("https://us.api.konghq.com/v1/event-gateways/gateway-1/listeners?page%5Bafter%5D=cursor-1"), listener("listener-1", "l1")), nil).
			Once()
		s.listeners.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayListenersRequest) bool {
				return req.PageAfter != nil && *req.PageAfter == "cursor-1"
			})).
			Return(listenersPage(nil, listener("listener-2", "l2")), nil).
			Once()
		expectPolicies(s.policies, "listener-1", policiesNotUsing)
		expectPolicies(s.policies, "listener-2", policyUsingByName)

		err := guardedDelete(t, s, newClient(t), newTrustBundle())
		inUse, ok := errors.AsType[EventGatewayTLSTrustBundleInUseError](err)
		require.True(t, ok, "expected EventGatewayTLSTrustBundleInUseError, got %v", err)
		assert.Equal(t, []string{"l2/tls-b"}, inUse.UnmanagedKonnectPolicies)
	})

	t.Run("does not delete when the listeners pagination repeats a cursor", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		next := new("https://us.api.konghq.com/v1/event-gateways/gateway-1/listeners?page%5Bafter%5D=cursor-1")
		s.listeners.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.Anything).
			Return(listenersPage(next), nil).
			Twice()

		require.ErrorContains(t, guardedDelete(t, s, newClient(t), newTrustBundle()), `next page cursor "cursor-1" repeated`)
	})

	t.Run("does not delete when listing the Konnect listener policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"))
		s.policies.EXPECT().
			ListEventGatewayListenerPolicies(mock.Anything, mock.Anything).
			Return(nil, errors.New("konnect unavailable")).
			Once()

		err := guardedDelete(t, s, newClient(t), newTrustBundle())
		require.ErrorContains(t, err, "konnect unavailable")
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, isBlocked)
	})

	t.Run("does not delete when the policies response body is missing or malformed", func(t *testing.T) {
		t.Parallel()

		for name, resp := range map[string]*sdkkonnectops.ListEventGatewayListenerPoliciesResponse{
			"missing":   {},
			"malformed": policiesResponse(`{"not": "an array"`),
		} {
			t.Run(name, func(t *testing.T) {
				s := newSDKs(t)
				expectGet(s.trustBundles, trustBundleName)
				expectListeners(s.listeners, listener("listener-1", "l1"))
				s.policies.EXPECT().
					ListEventGatewayListenerPolicies(mock.Anything, mock.Anything).
					Return(resp, nil).
					Once()

				require.Error(t, guardedDelete(t, s, newClient(t), newTrustBundle()))
			})
		}
	})

	t.Run("does not delete when listing the cluster policies fails", func(t *testing.T) {
		t.Parallel()

		s := newSDKs(t)
		expectGet(s.trustBundles, trustBundleName)
		expectListeners(s.listeners, listener("listener-1", "l1"))
		expectPolicies(s.policies, "listener-1", policyUsingByID)
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return errors.New("list failed")
				},
			}).
			Build()

		err := guardedDelete(t, s, cl, newTrustBundle())
		require.ErrorContains(t, err, "list failed")
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, isBlocked)
	})

	t.Run("ops.Delete reports the blocked deletion", func(t *testing.T) {
		t.Parallel()

		sdk := sdkwrappermocks.NewMockSDKWrapperWithT(t)
		expectGet(sdk.EventGatewayTLSTrustBundlesSDK, trustBundleName)
		expectListeners(sdk.EventGatewayListenersSDK, listener("listener-1", "l1"))
		expectPolicies(sdk.EventGatewayListenerPoliciesSDK, "listener-1", policyUsingByID)

		err := Delete(t.Context(), sdk, newClient(t), &metricsmocks.MockRecorder{}, newTrustBundle())
		_, isBlocked := errors.AsType[DeletionBlockedError](err)
		assert.True(t, isBlocked, "expected a DeletionBlockedError, got %v", err)
	})
}
