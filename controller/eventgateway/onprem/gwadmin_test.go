/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
)

func TestPush(t *testing.T) {
	gateway := newTestGateway("gateway")
	gatewayID := uuid.MustParse(string(gateway.UID))
	other := uuid.New()
	target := &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin}
	pod := gwadminsync.Pod{Name: "gw-admin-0", UID: "pod-uid", Address: "10.0.0.1:8083"}
	expected := []uuid.UUID{gatewayID, other}
	expectedCall := callExpected + " " + gatewayID.String() + "," + other.String()
	all := []string{callGetEntities, callRegister, callEntities, expectedCall}

	t.Run("a pod that doesn't know the gateway gets it all, in order", func(t *testing.T) {
		r, gwAdmin := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, all, gwAdmin.recorded())
	})

	// pushed returns a reconciler and a pod the gateway was pushed to.
	pushed := func(t *testing.T) (*Reconciler, *fakeGWAdminClient) {
		t.Helper()
		r, gwAdmin := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		gwAdmin.reset()
		return r, gwAdmin
	}

	t.Run("nothing changed: only read", func(t *testing.T) {
		r, gwAdmin := pushed(t)
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, []string{callGetEntities, callListCertificates}, gwAdmin.recorded())
	})

	t.Run("the expected gateways changed: only they are set", func(t *testing.T) {
		r, gwAdmin := pushed(t)
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, []uuid.UUID{gatewayID}, nil))
		assert.Equal(t, []string{callGetEntities, callListCertificates, callExpected + " " + gatewayID.String()}, gwAdmin.recorded())
	})

	t.Run("expected gateways that may have been set by a call that failed are set again", func(t *testing.T) {
		r, gwAdmin := pushed(t)
		// The call times out, but gw-admin may have applied it.
		gwAdmin.respond = func(call string) (*reply, error) {
			if strings.HasPrefix(call, callExpected) {
				return nil, errors.New("timeout")
			}
			return nil, nil
		}
		require.Error(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, []uuid.UUID{gatewayID}, nil))
		gwAdmin.respond = nil
		gwAdmin.reset()
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, []string{callGetEntities, callListCertificates, expectedCall}, gwAdmin.recorded(),
			"those recorded before the failed call aren't trusted, even when they are the ones to set")
	})

	t.Run("a read answered with another 2xx than 200 fails", func(t *testing.T) {
		r, gwAdmin := newPushReconciler(), &fakeGWAdminClient{respond: func(call string) (*reply, error) {
			if call == callGetEntities {
				return &reply{status: http.StatusNoContent}, nil
			}
			return nil, nil
		}}
		require.EqualError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil),
			"failed to read the entities of the gateway: gw-admin returned 204 No Content, not 200")
	})

	t.Run("the pod holds other entities: only they are pushed", func(t *testing.T) {
		r, gwAdmin := pushed(t)
		gwAdmin.gateways[gatewayID] = gwadminapi.GatewayEntitySet{BackendClusters: []gwadminapi.EventGatewayBackendClusterOnPrem{{Name: "stale"}}}
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, []string{callGetEntities, callListCertificates, callEntities}, gwAdmin.recorded())
	})

	t.Run("a restarted pod lost everything: it gets it all", func(t *testing.T) {
		r, gwAdmin := pushed(t)
		gwAdmin.restart()
		require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, all, gwAdmin.recorded())
	})

	t.Run("another pod gets it all", func(t *testing.T) {
		r, _ := pushed(t)
		replaced := &fakeGWAdminClient{}
		require.NoError(t, r.push(t.Context(), target, gwadminsync.Pod{Name: "gw-admin-0", UID: "new-pod-uid", Address: "10.0.0.1:8083"},
			replaced, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
		assert.Equal(t, all, replaced.recorded())
	})

	// A failure is retried with what failed: only what succeeded is
	// remembered, and the pod is read again.
	failures := []struct {
		name      string
		respond   func(string) (*reply, error)
		wantCalls int
		wantErr   string
		// wantRetry are the calls of the push tried again.
		wantRetry []string
	}{
		{name: "reading the entities fails", respond: failing(callGetEntities, http.StatusInternalServerError), wantCalls: 1,
			wantErr:   "failed to read the entities of the gateway: gw-admin returned 500 Internal Server Error: failed get-entities",
			wantRetry: all},
		{name: "registering the gateway fails", respond: failing(callRegister, http.StatusInternalServerError), wantCalls: 2,
			wantErr:   "failed to register the gateway: gw-admin returned 500 Internal Server Error: failed register",
			wantRetry: all},
		// The pod holds no entities, which is what is pushed: the expected
		// gateways, never set, are.
		{name: "pushing the entities fails", respond: failing(callEntities, http.StatusBadRequest), wantCalls: 3,
			wantErr:   "failed to push the entities of the gateway",
			wantRetry: []string{callGetEntities, callListCertificates, expectedCall}},
		{name: "setting the expected gateways fails", respond: failing(callExpected, http.StatusBadRequest), wantCalls: 4,
			wantErr:   "failed to set the gateways gw-admin serves",
			wantRetry: []string{callGetEntities, callListCertificates, expectedCall}},
		{name: "a transport error", respond: func(string) (*reply, error) { return nil, errors.New("connection refused") }, wantCalls: 1,
			wantErr:   "failed to read the entities of the gateway: connection refused",
			wantRetry: all},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			r, gwAdmin := newPushReconciler(), &fakeGWAdminClient{respond: tc.respond}
			require.ErrorContains(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil), tc.wantErr)
			assert.Equal(t, all[:tc.wantCalls], gwAdmin.recorded())

			gwAdmin.respond = nil
			gwAdmin.reset()
			require.NoError(t, r.push(t.Context(), target, pod, gwAdmin, deriveConfig(gateway, emptyConfiguration()), emptyConfiguration(), gatewayID, expected, nil))
			assert.Equal(t, tc.wantRetry, gwAdmin.recorded())
		})
	}
}

func TestPushCertificates(t *testing.T) {
	gwAdmin := newGWAdminFixture(t).GWAdmin
	target := &gwadminsync.Target{GWAdmin: gwAdmin}
	pod := gwadminsync.Pod{Name: "gw-admin-0", UID: "pod-uid", Address: "10.0.0.1:8083"}
	first, second := newTestGateway("first"), newTestGateway("second")
	firstID, secondID := uuid.MustParse(string(first.UID)), uuid.MustParse(string(second.UID))
	certID := uuid.New()
	withCertificate := emptyConfiguration()
	withCertificate.certificates = []dataPlaneCertificate{testCertificate(certID, client.ObjectKey{Namespace: "default", Name: "cert"})}
	registerOn := func(gatewayID uuid.UUID) string {
		return callCertificate + " " + gatewayID.String() + "/" + certID.String()
	}
	push := func(t *testing.T, r *Reconciler, client *fakeGWAdminClient, gateway *eventgatewayv1alpha1.OnPremEventGateway, cfg configuration, expected ...uuid.UUID) error {
		t.Helper()
		return r.push(t.Context(), target, pod, client, deriveConfig(gateway, cfg), cfg, uuid.MustParse(string(gateway.UID)), expected, nil)
	}

	t.Run("a pod that doesn't know the gateway gets its certificates before its entities, and isn't asked for them", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		assert.Equal(t, []string{callGetEntities, callRegister, registerOn(firstID), callEntities, callExpected + " " + firstID.String()}, client.recorded())
	})

	t.Run("a certificate a pod holds as it is isn't registered again", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		client.reset()
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		assert.Equal(t, []string{callGetEntities, callListCertificates}, client.recorded())
	})

	t.Run("a certificate moved to another gateway of the GWAdmin is registered for that one, and removed from the other", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		both := []uuid.UUID{firstID, secondID}
		require.NoError(t, push(t, r, client, first, withCertificate, both...))
		require.NoError(t, push(t, r, client, second, emptyConfiguration(), both...))
		client.reset()
		require.NoError(t, push(t, r, client, second, withCertificate, both...))
		require.NoError(t, push(t, r, client, first, emptyConfiguration(), both...))
		assert.Equal(t, []string{
			callGetEntities, callListCertificates, registerOn(secondID),
			callGetEntities, callListCertificates, callDeleteCertificate + " " + firstID.String() + "/" + certID.String(),
		}, client.recorded())
		assert.Contains(t, client.holds(secondID), certID)
		assert.NotContains(t, client.holds(firstID), certID)
	})

	t.Run("a gateway registered again gets its certificates again: gw-admin deleted them with it", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		// e.g. it may not use its GWAdmin for a while.
		require.NoError(t, r.removeGateway(t.Context(), target, pod, client, firstID, []uuid.UUID{}))
		assert.Empty(t, client.holds(firstID))
		client.reset()
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		assert.Equal(t, []string{
			callGetEntities, callRegister, callCertificate + " " + firstID.String() + "/" + certID.String(),
			callEntities, callExpected + " " + firstID.String(),
		}, client.recorded(), "a gateway just registered holds none: they aren't read")
		assert.Contains(t, client.holds(firstID), certID)
	})

	t.Run("the certificates come before the entities: an entity gw-admin rejects doesn't keep them from being trusted", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{respond: failing(callEntities, http.StatusBadRequest)}
		require.ErrorContains(t, push(t, r, client, first, withCertificate, firstID), "failed to push the entities")
		assert.Contains(t, client.holds(firstID), certID)
	})

	rejectedID := uuid.New()
	withRejected := emptyConfiguration()
	withRejected.certificates = []dataPlaneCertificate{
		testCertificate(rejectedID, client.ObjectKey{Namespace: "default", Name: "rejected"}),
		testCertificate(certID, client.ObjectKey{Namespace: "default", Name: "cert"}),
	}
	rejectedCall := callCertificate + " " + firstID.String() + "/" + rejectedID.String()

	t.Run("a certificate gw-admin rejects isn't trusted as it was, is only reported on it, and holds neither the others, the entities nor the expected gateways", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, push(t, r, client, first, withRejected, firstID))
		client.reset()
		withRejected.certificates[0].body.Certificate = "changed"
		client.respond = failing(rejectedCall, http.StatusBadRequest)
		outcome := newCertificateOutcome()
		err := r.push(t.Context(), target, pod, client, deriveConfig(first, withRejected), withRejected, firstID, []uuid.UUID{firstID}, outcome)
		require.NoError(t, err, "the rest of the configuration doesn't depend on it")
		assert.Equal(t, []string{
			callGetEntities, callListCertificates, rejectedCall, callDeleteCertificate + " " + firstID.String() + "/" + rejectedID.String(),
		}, client.recorded(), "the other certificate, the entities and the expected gateways are as they were")
		assert.NotContains(t, client.holds(firstID), rejectedID)
		_, _, _, rejectedBy := outcome.held(&gwadminsync.Target{Pods: []gwadminsync.Pod{pod}}, nil, withRejected.certificates[0])
		assert.Contains(t, rejectedBy[pod.Name], "failed to register the data plane certificate default/rejected")
		held, _, _, _ := outcome.held(&gwadminsync.Target{Pods: []gwadminsync.Pod{pod}}, nil, withRejected.certificates[1])
		assert.True(t, held)
	})

	t.Run("a pod failing on a certificate isn't told the expected gateways: it mustn't turn ready without it", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{respond: failing(rejectedCall, http.StatusInternalServerError)}
		require.ErrorContains(t, push(t, r, client, first, withRejected, firstID), "failed to register the data plane certificate default/rejected")
		assert.Equal(t, []string{callGetEntities, callRegister, rejectedCall}, client.recorded())
	})

	t.Run("a pod restarted holds nothing, and gets it all again", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		client.restart()
		client.reset()
		require.NoError(t, push(t, r, client, first, withCertificate, firstID))
		assert.Equal(t, []string{callGetEntities, callRegister, registerOn(firstID), callEntities, callExpected + " " + firstID.String()}, client.recorded())
	})

	t.Run("the entities are pushed again with a new version of the configuration", func(t *testing.T) {
		r, client := newPushReconciler(), &fakeGWAdminClient{}
		config := deriveConfig(first, withCertificate)
		require.NoError(t, r.push(t.Context(), target, pod, client, config, withCertificate, firstID, []uuid.UUID{firstID}, nil))
		client.reset()
		config.Version.DataVersion++
		require.NoError(t, r.push(t.Context(), target, pod, client, config, withCertificate, firstID, []uuid.UUID{firstID}, nil))
		assert.Equal(t, []string{callGetEntities, callListCertificates, callEntities}, client.recorded())
	})
}

func TestRemoveGateway(t *testing.T) {
	gatewayID, other := uuid.New(), uuid.New()
	target := &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin}
	pod := gwadminsync.Pod{Name: "gw-admin-0", UID: "pod-uid", Address: "10.0.0.1:8083"}
	tests := []struct {
		name      string
		respond   func(string) (*reply, error)
		wantCalls []string
		wantErr   string
	}{
		{name: "left out of the expected gateways, then deleted", wantCalls: []string{callExpected + " " + other.String(), callDeleteGateway}},
		{name: "a gateway gw-admin doesn't know is deleted already", respond: failing(callDeleteGateway, http.StatusNotFound),
			wantCalls: []string{callExpected + " " + other.String(), callDeleteGateway}},
		{name: "the deletion fails", respond: failing(callDeleteGateway, http.StatusInternalServerError),
			wantCalls: []string{callExpected + " " + other.String(), callDeleteGateway}, wantErr: "failed to delete the gateway"},
		{name: "setting the expected gateways fails", respond: failing(callExpected, http.StatusInternalServerError),
			wantCalls: []string{callExpected + " " + other.String()}, wantErr: "failed to set the gateways gw-admin serves"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, gwAdmin := newPushReconciler(), &fakeGWAdminClient{respond: tc.respond}
			r.GWAdminSync.Record(target, pod, itemExpected, idsKey([]uuid.UUID{gatewayID, other}))
			err := r.removeGateway(t.Context(), target, pod, gwAdmin, gatewayID, []uuid.UUID{other})
			assert.Equal(t, tc.wantCalls, gwAdmin.recorded())
			recorded := r.GWAdminSync.Recorded(target, pod, itemExpected)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if len(tc.wantCalls) > 1 {
				assert.Equal(t, idsKey([]uuid.UUID{other}), recorded, "the expected gateways set are recorded")
			} else {
				assert.Empty(t, recorded, "expected gateways that may have been set by a call that failed aren't trusted")
			}
		})
	}
}

func TestResolveGWAdminNotAllowed(t *testing.T) {
	gateway := newTestGateway("gateway")
	gateway.Namespace = "other"
	gateway.Spec.GWAdminRef.Namespace = new(ns)
	f := newGWAdminFixture(t)
	cl := newFakeClient(t, append(f.Objects(), gateway)...)
	r := &Reconciler{Client: cl, GWAdminSync: &gwadminsync.Syncer{Client: cl, OperatorNamespace: ns}}
	_, err := r.resolveGWAdmin(t.Context(), gateway, true)
	notReady, ok := errors.AsType[gwAdminNotReadyError](err)
	require.True(t, ok, "not ready: %v", err)
	assert.True(t, notReady.notAllowed)
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason, notReady.resolvedReason())
	assert.Contains(t, notReady.msg, "The gateway may not use its GWAdmin: no KongReferenceGrant in namespace default")
}

func TestResolveGWAdminReasons(t *testing.T) {
	gateway := newTestGateway("gateway")
	resolve := func(t *testing.T, objects ...client.Object) gwAdminNotReadyError {
		t.Helper()
		cl := newFakeClient(t, append(objects, gateway)...)
		r := &Reconciler{Client: cl, GWAdminSync: &gwadminsync.Syncer{Client: cl, OperatorNamespace: ns}}
		_, err := r.resolveGWAdmin(t.Context(), gateway, true)
		notReady, ok := errors.AsType[gwAdminNotReadyError](err)
		require.True(t, ok, "not ready: %v", err)
		return notReady
	}

	notFound := resolve(t)
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason, notFound.resolvedReason())
	assert.Equal(t, "The GWAdmin default/gw-admin does not exist", notFound.msg)
	assert.True(t, notFound.nothingToRemove)

	f := newGWAdminFixture(t)
	noPod := resolve(t, f.Objects()...)
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason, noPod.resolvedReason(), "any other cause")
	assert.Equal(t, "The GWAdmin default/gw-admin has no gw-admin pod", noPod.msg)
	assert.True(t, noPod.nothingToRemove, "a GWAdmin without a pod has nothing to remove")
}

func TestRequestsForGWAdmin(t *testing.T) {
	gateway, other := newTestGateway("gateway"), newTestGateway("other")
	other.Spec.GWAdminRef.Name = "other"
	key := client.ObjectKey{Namespace: ns, Name: testGWAdminName}
	cl := newFakeClient(t, gateway, other)
	assert.Equal(t, requestsFor(gateway), requestsForGWAdmin(t.Context(), cl, key), "the gateways of the GWAdmin, whether it exists or not")

	cl = newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, requestsForGWAdmin(t.Context(), cl, key), "an error maps to nothing")
}

func TestMapGatewayToGatewaysOfItsGWAdmin(t *testing.T) {
	deleted, sibling, other := newTestGateway("deleted"), newTestGateway("sibling"), newTestGateway("other")
	other.Spec.GWAdminRef.Name = "other"
	cl := newFakeClient(t, sibling, other)
	assert.Equal(t, requestsFor(sibling), mapGatewayToGatewaysOfItsGWAdmin(cl)(t.Context(), deleted),
		"the gateways of the GWAdmin of the deleted one")
	assert.Empty(t, mapGatewayToGatewaysOfItsGWAdmin(cl)(t.Context(), newGWAdminFixture(t).GWAdmin), "not a gateway")
}

func TestMapKongReferenceGrantToOnPremEventGateways(t *testing.T) {
	gateway := newTestGateway("gateway")
	gateway.Namespace = "other"
	gateway.Spec.GWAdminRef.Namespace = new(ns)
	// Neither depends on the grant: one uses a GWAdmin of its own namespace,
	// the other one is in the namespace of the grant.
	local := newTestGateway("local")
	local.Namespace = "other"
	sameNamespace := newTestGateway("same-namespace")
	gw := &operatorv1alpha1.GWAdmin{Name: testGWAdminName, Namespace: ns}
	grant := gwadminreftest.Grant(ns, "grant", "other", "")
	cl := newFakeClient(t, gateway, local, sameNamespace, gw, grant)
	assert.Equal(t, requestsFor(gateway), mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), grant),
		"the gateways of the granted namespaces referencing a GWAdmin of the namespace of the grant")

	notGWAdmins := grant.DeepCopy()
	notGWAdmins.Spec.To[0].Kind = "Service"
	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), notGWAdmins), "a grant that isn't about GWAdmins")

	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), gw), "not a KongReferenceGrant")

	cl = newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), grant))
}

func TestExpectedGateways(t *testing.T) {
	gateway := newTestGateway("gateway")
	deleting := newTestGateway("deleting")
	deleting.Finalizers = []string{FinalizerGWAdminCleanup}
	deleting.SetDeletionTimestamp(new(metav1.Now()))
	notV4 := newTestGateway("not-v4")
	notV4.SetUID("11111111-1111-1111-1111-111111111111")
	notAllowed := newTestGateway("not-allowed")
	notAllowed.Namespace = "other"
	notAllowed.Spec.GWAdminRef.Namespace = new(ns)
	allowed := newTestGateway("allowed")
	allowed.Namespace = "granted"
	allowed.Spec.GWAdminRef.Namespace = new(ns)
	otherGWAdmin := newTestGateway("other-gw-admin")
	otherGWAdmin.Spec.GWAdminRef.Name = "other"
	cl := newFakeClient(t, gateway, deleting, notV4, notAllowed, allowed, otherGWAdmin,
		gwadminreftest.Grant(ns, "grant", "granted", ""))

	ids, err := expectedGateways(t.Context(), cl, gateway)
	require.NoError(t, err)
	want := []uuid.UUID{uuid.MustParse(string(gateway.UID)), uuid.MustParse(string(allowed.UID))}
	slices.SortFunc(want, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	assert.Equal(t, want, ids, "the gateways allowed to use the GWAdmin, not being deleted, with a version 4 UID, sorted")

	_, err = expectedGateways(t.Context(), newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList}), gateway)
	require.Error(t, err)
}
