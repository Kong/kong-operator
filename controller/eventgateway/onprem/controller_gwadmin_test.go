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
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

func TestProvisionGWAdminWithoutGateways(t *testing.T) {
	// starting makes the pod of the fixture not serving yet, e.g. one of a
	// rollout.
	starting := func(f *pushFixture) {
		f.gwAdmin.EndpointSlice.Endpoints[0] = gwadminsynctest.NotServing(f.gwAdmin.EndpointSlice.Endpoints[0])
	}
	// withoutGateways leaves the GWAdmin of the fixture without gateways, its
	// pod not serving yet.
	withoutGateways := func(f *pushFixture) []client.Object {
		starting(f)
		return f.gwAdmin.Objects()
	}
	provision := func(t *testing.T, f *pushFixture) reconcile.Result {
		t.Helper()
		res, err := f.r.provisionGWAdminWithoutGateways(t.Context(), reconcile.Request{NamespacedName: f.gateway.GWAdminKey()})
		require.NoError(t, err)
		return res
	}

	t.Run("a pod not serving of a GWAdmin without gateways is told it serves none", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, withoutGateways)
		assert.Zero(t, provision(t, f))
		assert.Equal(t, []string{callExpected + " "}, f.pod().recorded())
		assert.Empty(t, f.terminatingPod().recorded(), "a terminating pod isn't provisioned")
	})

	t.Run("a pod serving passed the readiness gate already", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object { return f.gwAdmin.Objects() })
		assert.Zero(t, provision(t, f))
		assert.Empty(t, f.pod().recorded())
	})

	t.Run("a call that failed is tried again after its backoff, and reported once", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, withoutGateways)
		const base = 200 * time.Millisecond
		f.r.backoff.Base, f.r.backoff.Max = base, 2*base
		f.pod().respond = failing(callExpected, http.StatusInternalServerError)
		assert.Equal(t, base, provision(t, f).RequeueAfter, "not returned: the rate limiter would retry it right away")
		recorded := recordedEvents(f.recorder)
		require.Len(t, recorded, 1, "no gateway reports it: the GWAdmin does")
		assert.Contains(t, recorded[0], "Warning "+eventReasonProvisionFailed+" The gw-admin pods can't be told they serve no OnPremEventGateway")

		// Triggered again, e.g. by a change to its Secrets, it waits.
		f.pod().reset()
		assert.Positive(t, provision(t, f).RequeueAfter)
		assert.Empty(t, f.pod().recorded())

		// Once it has waited, it is tried again, and not reported again.
		time.Sleep(base)
		assert.Equal(t, 2*base, provision(t, f).RequeueAfter)
		assert.Equal(t, []string{callExpected + " "}, f.pod().recorded())
		assert.Empty(t, recordedEvents(f.recorder))

		f.pod().respond = nil
		f.pod().reset()
		time.Sleep(2 * base)
		assert.Zero(t, provision(t, f))
		assert.Equal(t, []string{callExpected + " "}, f.pod().recorded())
		assert.Empty(t, recordedEvents(f.recorder))
	})

	t.Run("a failure to list the gateways keeps the backoff of a call that failed", func(t *testing.T) {
		listFails := false
		f := newPushFixture(t, interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*eventgatewayv1alpha1.OnPremEventGatewayList); ok && listFails {
					return errors.New("cache not synced")
				}
				return cl.List(ctx, list, opts...)
			},
		}, withoutGateways)
		f.pod().respond = failing(callExpected, http.StatusInternalServerError)
		assert.Positive(t, provision(t, f).RequeueAfter)
		require.Len(t, recordedEvents(f.recorder), 1)

		listFails = true
		_, err := f.r.provisionGWAdminWithoutGateways(t.Context(), reconcile.Request{NamespacedName: f.gateway.GWAdminKey()})
		require.Error(t, err)

		listFails = false
		f.pod().reset()
		assert.Positive(t, provision(t, f).RequeueAfter, "still waiting")
		assert.Empty(t, f.pod().recorded())
		assert.Empty(t, recordedEvents(f.recorder), "not reported again")
	})

	t.Run("a call cut short by the operator stopping isn't reported", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, withoutGateways)
		ctx, cancel := context.WithCancel(t.Context())
		// It fails as the operator stops.
		fail := failing(callExpected, http.StatusInternalServerError)
		f.pod().respond = func(call string) (*reply, error) {
			cancel()
			return fail(call)
		}
		_, err := f.r.provisionGWAdminWithoutGateways(ctx, reconcile.Request{NamespacedName: f.gateway.GWAdminKey()})
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, recordedEvents(f.recorder))

		f.pod().respond = nil
		f.pod().reset()
		assert.Zero(t, provision(t, f), "nor counted as a failure: tried again right away")
		assert.Equal(t, []string{callExpected + " "}, f.pod().recorded())
	})

	t.Run("a GWAdmin with a gateway is left to its reconcile", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			starting(f)
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		assert.Zero(t, provision(t, f))
		assert.Empty(t, f.pod().recorded())
	})

	t.Run("a GWAdmin with a gateway is left to its reconcile without taking its lock", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			starting(f)
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		unlock, ok := f.r.GWAdminSync.TryLock(f.gateway.GWAdminKey(), client.ObjectKeyFromObject(f.gateway))
		require.True(t, ok)
		defer unlock()
		assert.Zero(t, provision(t, f), "not tried again: the gateway provisions the pods")
	})

	t.Run("a pod still pending is tried again less and less often", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, withoutGateways)
		// e.g. crash looping.
		f.pod().respond = func(string) (*reply, error) { return nil, connectionRefused() }
		assert.Equal(t, pendingRetryBase, provision(t, f).RequeueAfter)
		assert.Equal(t, 2*pendingRetryBase, provision(t, f).RequeueAfter)
		for range 10 {
			provision(t, f)
		}
		assert.Equal(t, pendingRetryMax, provision(t, f).RequeueAfter)
		assert.Empty(t, recordedEvents(f.recorder), "a pod pending isn't a failure")

		// Once it listens, it is provisioned, and its backoff forgotten.
		f.pod().respond = nil
		assert.Zero(t, provision(t, f))
		f.pod().respond = func(string) (*reply, error) { return nil, connectionRefused() }
		assert.Equal(t, pendingRetryBase, provision(t, f).RequeueAfter)
	})

	t.Run("a gateway that may not use the GWAdmin doesn't count", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			objects := inAnotherNamespace(f)
			starting(f)
			return objects
		})
		assert.Zero(t, provision(t, f))
		assert.Equal(t, []string{callExpected + " "}, f.pod().recorded())
	})

	t.Run("a GWAdmin locked by a gateway is tried again later", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, withoutGateways)
		unlock, ok := f.r.GWAdminSync.TryLock(f.gateway.GWAdminKey(), types.NamespacedName{Namespace: ns, Name: "holder"})
		require.True(t, ok)
		defer unlock()
		assert.Equal(t, lockRetryInterval, provision(t, f).RequeueAfter)
		assert.Empty(t, f.pod().recorded())
	})

	t.Run("a GWAdmin that doesn't exist is left alone", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(*pushFixture) []client.Object { return nil })
		assert.Zero(t, provision(t, f))
		assert.Empty(t, f.pod().recorded())
	})
}

func TestMapKongReferenceGrantToGWAdmins(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	grant := &configurationv1alpha1.KongReferenceGrant{Name: "grant", Namespace: ns}
	assert.Equal(t, []reconcile.Request{{NamespacedName: f.gateway.GWAdminKey()}}, mapKongReferenceGrantToGWAdmins(f.cl)(t.Context(), grant))
	assert.Empty(t, mapKongReferenceGrantToGWAdmins(f.cl)(t.Context(), &configurationv1alpha1.KongReferenceGrant{Name: "grant", Namespace: "elsewhere"}))
}
