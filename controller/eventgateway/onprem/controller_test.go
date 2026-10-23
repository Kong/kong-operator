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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
)

// pushFixture is a gateway, its GWAdmin, with a pod and a terminating one,
// and a reconciler pushing to fake gw-admin pods.
type pushFixture struct {
	r        *Reconciler
	cl       client.WithWatch
	pods     *fakePods
	recorder *events.FakeRecorder
	gwAdmin  *gwadminsynctest.GWAdmin

	gateway   *eventgatewayv1alpha1.OnPremEventGateway
	gatewayID uuid.UUID
}

const (
	testPodAddress            = "10.0.0.1:8083"
	testTerminatingPodAddress = "10.0.0.2:8083"
)

// newPushFixture returns a pushFixture; change, when set, changes its objects
// before the client is built.
func newPushFixture(t *testing.T, funcs interceptor.Funcs, change func(*pushFixture) []client.Object) *pushFixture {
	t.Helper()
	f := &pushFixture{
		pods:     newFakePods(testPodAddress, testTerminatingPodAddress),
		recorder: events.NewFakeRecorder(10),
		gwAdmin:  newGWAdminFixture(t, endpoint("gw-admin-0", "10.0.0.1", false), endpoint("gw-admin-1", "10.0.0.2", true)),
		gateway:  newTestGateway("gateway"),
	}
	f.gateway.Finalizers = []string{FinalizerGWAdminCleanup}
	f.gatewayID = uuid.MustParse(string(f.gateway.UID))

	objects := append(f.gwAdmin.Objects(), f.gateway)
	if change != nil {
		objects = change(f)
	}
	f.cl = newFakeClientWithInterceptor(t, funcs, objects...)
	f.r = &Reconciler{
		Client:        f.cl,
		TypeConverter: testTypeConverter,
		GWAdminSync:   &gwadminsync.Syncer{Client: f.cl, OperatorNamespace: ns, NewClient: f.pods.newClient},
		eventRecorder: f.recorder,
	}
	return f
}

func (f *pushFixture) pod() *fakeGWAdminClient { return f.pods.get(testPodAddress) }

// inAnotherNamespace moves the GWAdmin to the namespace admins, where no
// KongReferenceGrant allows the gateway to use it.
func inAnotherNamespace(f *pushFixture) []client.Object {
	f.gateway.Spec.GWAdminRef.Namespace = new("admins")
	f.gwAdmin.GWAdmin.Namespace = "admins"
	f.gwAdmin.Bundle.Namespace = "admins"
	f.gwAdmin.EndpointSlice.Namespace = "admins"
	return append(f.gwAdmin.Objects(), f.gateway)
}
func (f *pushFixture) terminatingPod() *fakeGWAdminClient {
	return f.pods.get(testTerminatingPodAddress)
}

func TestPushConfiguration(t *testing.T) {
	t.Run("pushed to every pod", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		require.NoError(t, pushErr)
		assert.True(t, result.done)
		require.NotNil(t, result.target)
		assert.Equal(t, []string{callGetEntities, callRegister, callEntities, callExpected + " " + f.gatewayID.String()}, f.pod().recorded())
		assert.Empty(t, f.terminatingPod().recorded(), "a terminating pod isn't pushed to")
	})

	t.Run("a pod fails", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		f.pod().respond = failing(callEntities, http.StatusBadRequest)
		result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		require.ErrorContains(t, pushErr, "gw-admin pod gw-admin-0: failed to push the entities of the gateway")
		assert.True(t, result.done)
		assert.NotNil(t, result.target)
	})

	t.Run("the GWAdmin is gone", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return []client.Object{f.gateway}
		})
		result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		assert.True(t, result.done)
		assert.Nil(t, result.target)
		notReady, ok := errors.AsType[gwAdminNotReadyError](pushErr)
		require.True(t, ok)
		assert.True(t, notReady.nothingToRemove)
	})

	t.Run("the gateway may not use the GWAdmin: it is removed", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, inAnotherNamespace)
		_, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		notReady, ok := errors.AsType[gwAdminNotReadyError](pushErr)
		require.True(t, ok)
		assert.True(t, notReady.notAllowed)
		assert.True(t, onlyNotReady(pushErr), "not retried")
		assert.Equal(t, []string{callExpected + " ", callDeleteGateway}, f.pod().recorded(), "no gateway of the GWAdmin is allowed")
		assert.Equal(t, []string{callExpected + " ", callDeleteGateway}, f.terminatingPod().recorded())
	})

	t.Run("the gateway may not use the GWAdmin, and a pod fails to remove it", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, inAnotherNamespace)
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		_, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		require.ErrorContains(t, pushErr, "failed to make gw-admin stop serving the gateway: gw-admin pod gw-admin-0")
		assert.False(t, onlyNotReady(pushErr), "retried")

		f.pod().reset()
		result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		assert.Positive(t, result.retryAfter, "after its backoff")
		assert.Empty(t, f.pod().recorded(), "not removed again before its backoff")
		require.ErrorContains(t, pushErr, "failed to make gw-admin stop serving the gateway: gw-admin pod gw-admin-0",
			"the status reports the last failure again")
	})

	t.Run("the gateway may not use the GWAdmin: it is removed once, until it may again", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, inAnotherNamespace)
		removed := []string{callExpected + " ", callDeleteGateway}
		for range 2 {
			_, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
			require.NoError(t, err)
			assert.True(t, onlyNotReady(pushErr))
		}
		assert.Equal(t, removed, f.pod().recorded(), "told once")

		grant := gwadminreftest.Grant("admins", "gateways", ns, "")
		require.NoError(t, f.cl.Create(t.Context(), grant))
		_, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		require.NoError(t, pushErr)

		require.NoError(t, f.cl.Delete(t.Context(), grant))
		_, _, err = f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.NoError(t, err)
		pushed := []string{callGetEntities, callRegister, callEntities, callExpected + " " + f.gatewayID.String()}
		assert.Equal(t, slices.Concat(removed, pushed, removed), f.pod().recorded(), "told again once no longer allowed")
	})

	t.Run("the gateway may not use the GWAdmin, whose pods can't be told yet", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			objects := inAnotherNamespace(f)
			return slices.DeleteFunc(objects, func(o client.Object) bool { return o == f.gwAdmin.ClientSecret })
		})
		for range 2 {
			_, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
			require.NoError(t, err)
			require.ErrorContains(t, pushErr, "gw-admin can't be told to stop serving the gateway yet: The operator client certificate Secret")
			assert.True(t, onlyNotReady(pushErr), "a change to the GWAdmin triggers it again")
		}
		_, removed := f.r.removedFromGWAdmin.Load(f.gateway.UID)
		assert.False(t, removed, "tried again on the next reconcile")
	})

	t.Run("the gateway may not use the GWAdmin, and the GWAdmin can't be read", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*operatorv1alpha1.GWAdmin); ok {
					return errors.New("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}, func(g *pushFixture) []client.Object {
			g.gateway.Spec.GWAdminRef.Namespace = new("admins")
			return []client.Object{g.gateway}
		})
		result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
		require.ErrorContains(t, err, "unavailable", "an error reading the cluster is only retried")
		assert.True(t, result.done)
		assert.True(t, onlyNotReady(pushErr), "the status reports the gateway may not use the GWAdmin")
	})

	transient := []struct {
		name  string
		funcs interceptor.Funcs
		want  string
	}{
		{
			name: "the GWAdmin can't be read",
			funcs: interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*operatorv1alpha1.GWAdmin); ok {
					return errors.New("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			}},
			want: "failed to get the GWAdmin default/gw-admin: unavailable",
		},
		{
			name: "the gateways of the GWAdmin can't be listed",
			funcs: interceptor.Funcs{List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*eventgatewayv1alpha1.OnPremEventGatewayList); ok {
					return errors.New("unavailable")
				}
				return cl.List(ctx, list, opts...)
			}},
			want: "failed to list the OnPremEventGateways of the GWAdmin default/gw-admin: unavailable",
		},
	}
	for _, tc := range transient {
		t.Run("not pushed, only retried: "+tc.name, func(t *testing.T) {
			f := newPushFixture(t, tc.funcs, nil)
			result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
			require.ErrorContains(t, err, tc.want)
			require.NoError(t, pushErr, "not an error of the push: the status doesn't report it")
			assert.False(t, result.done)
			assert.Empty(t, f.pod().recorded())
		})
	}
}

func TestReconcile(t *testing.T) {
	t.Run("pushed: the status reports it, and no event is recorded", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		res, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		gateway := read(t, f.cl, f.gateway)
		assert.Equal(t, testServerURL, gateway.Status.ServerURL)
		assert.True(t, apimeta.IsStatusConditionTrue(gateway.Status.Conditions, "Programmed"))
		assert.Empty(t, recordedEvents(f.recorder))
	})

	t.Run("a failed push is reported once, as a Warning event", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		_, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		f.pod().respond = failing(callGetEntities, http.StatusInternalServerError)
		res, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err, "not retried right away")
		assert.Equal(t, 2*time.Second, res.RequeueAfter, "retried after its backoff")
		gateway := read(t, f.cl, f.gateway)
		cond := apimeta.FindStatusCondition(gateway.Status.Conditions, "ConfigurationPushed")
		require.NotNil(t, cond)
		assert.Equal(t, "ConfigurationPushFailed", cond.Reason)
		recorded := recordedEvents(f.recorder)
		require.Len(t, recorded, 1)
		assert.True(t, strings.HasPrefix(recorded[0], "Warning ConfigurationPushFailed The configuration can't be pushed to gw-admin: gw-admin pod gw-admin-0"), recorded[0])

		res, err = f.r.Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		assert.Positive(t, res.RequeueAfter, "waits for its backoff")
		assert.Empty(t, recordedEvents(f.recorder), "already reported")
	})

	t.Run("a GWAdmin that isn't ready is reported, not retried", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return []client.Object{f.gateway}
		})
		res, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		conditions := read(t, f.cl, f.gateway).Status.Conditions
		cond := apimeta.FindStatusCondition(conditions, "Programmed")
		require.NotNil(t, cond)
		assert.Equal(t, "DependenciesNotReady", cond.Reason)
		assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage, cond.Message)
		resolved := apimeta.FindStatusCondition(conditions, "GWAdminResolved")
		require.NotNil(t, resolved)
		assert.Equal(t, metav1.ConditionFalse, resolved.Status)
		assert.Equal(t, "NotFound", resolved.Reason)
		assert.Equal(t, "The GWAdmin default/gw-admin does not exist", resolved.Message)
		assert.Empty(t, recordedEvents(f.recorder), "a gateway never pushed doesn't stop being pushed")
	})

	t.Run("a pushed gateway whose GWAdmin goes away is reported as a Warning event", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		_, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		require.NoError(t, f.cl.Delete(t.Context(), f.gwAdmin.GWAdmin))
		require.NoError(t, f.cl.Delete(t.Context(), f.gwAdmin.EndpointSlice))
		_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		recorded := recordedEvents(f.recorder)
		require.Len(t, recorded, 1)
		assert.Contains(t, recorded[0], "Warning DependenciesNotReady")
	})

	t.Run("a push without outcome leaves the status as it is", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*eventgatewayv1alpha1.OnPremEventGatewayList); ok {
				return errors.New("unavailable")
			}
			return cl.List(ctx, list, opts...)
		}}, nil)
		_, err := f.r.Reconcile(t.Context(), f.gateway)
		require.ErrorContains(t, err, "unavailable")
		assert.Empty(t, read(t, f.cl, f.gateway).Status.Conditions)
	})

	t.Run("a gateway whose UID is not a version 4 UUID is reported, not retried", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			f.gateway.SetUID("11111111-1111-1111-1111-111111111111")
			f.gateway.Finalizers = nil
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		res, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		gateway := read(t, f.cl, f.gateway)
		assert.NotContains(t, gateway.Finalizers, FinalizerGWAdminCleanup)
		for _, conditionType := range []string{"Programmed", "ConfigurationPushed"} {
			cond := apimeta.FindStatusCondition(gateway.Status.Conditions, conditionType)
			require.NotNil(t, cond, conditionType)
			assert.Equal(t, metav1.ConditionFalse, cond.Status)
			assert.Equal(t, "InvalidUID", cond.Reason)
			assert.Contains(t, cond.Message, "is not a version 4 UUID")
		}
		assert.Empty(t, f.pod().recorded())
		assert.Empty(t, recordedEvents(f.recorder), "the status reports it")
	})

	t.Run("a deleted gateway whose UID is not a version 4 UUID has nothing to clean up", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			// It never got its finalizer: another one holds its deletion.
			f.gateway.SetUID("11111111-1111-1111-1111-111111111111")
			f.gateway.Finalizers = []string{"example.com/hold"}
			f.gateway.SetDeletionTimestamp(new(metav1.Now()))
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		res, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		assert.Empty(t, f.pod().recorded(), "gw-admin isn't called")
		gateway := read(t, f.cl, f.gateway)
		assert.Equal(t, []string{"example.com/hold"}, gateway.Finalizers)
		assert.Empty(t, gateway.Status.Conditions, "its status isn't reported")
	})

	t.Run("the finalizer is added first", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			f.gateway.Finalizers = nil
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		_, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.Contains(t, read(t, f.cl, f.gateway).Finalizers, FinalizerGWAdminCleanup)
	})
}

func TestCleanup(t *testing.T) {
	deleting := func(since time.Duration) func(*pushFixture) []client.Object {
		return func(f *pushFixture) []client.Object {
			f.gateway.SetDeletionTimestamp(new(metav1.NewTime(time.Now().Add(-since))))
			return append(f.gwAdmin.Objects(), f.gateway)
		}
	}

	t.Run("without the finalizer, nothing to do", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, nil)
		gateway := f.gateway.DeepCopy()
		gateway.Finalizers = nil
		res, err := f.r.cleanup(t.Context(), logr.Discard(), gateway, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		assert.Empty(t, f.pod().recorded())
	})

	t.Run("every pod stops serving the gateway", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Second))
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		assert.Equal(t, []string{callExpected + " ", callDeleteGateway}, f.pod().recorded())
		assert.Equal(t, []string{callExpected + " ", callDeleteGateway}, f.terminatingPod().recorded())
		err = f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
		assert.True(t, apierrors.IsNotFound(err), "the finalizer is removed: %v", err)
		assert.Empty(t, recordedEvents(f.recorder))
	})

	t.Run("the start of the cleanup is recorded on the gateway, across restarts", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Hour))
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		_, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		started, err := time.Parse(time.RFC3339, read(t, f.cl, f.gateway).Annotations[annotationCleanupStarted])
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), started, 5*time.Second)

		// Another process, e.g. after a restart, counts from then too.
		restarted := newPushFixture(t, interceptor.Funcs{}, nil)
		restarted.r.Client = f.cl
		restarted.r.GWAdminSync.Client = f.cl
		restarted.r.GWAdminSync.NewClient = f.pods.newClient
		gateway := read(t, f.cl, f.gateway)
		gateway.Annotations[annotationCleanupStarted] = time.Now().Add(-cleanupGracePeriod - time.Second).UTC().Format(time.RFC3339)
		require.NoError(t, f.cl.Update(t.Context(), gateway))
		res, err := restarted.r.cleanup(t.Context(), logr.Discard(), read(t, f.cl, f.gateway), f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero(), "past the grace period recorded")
		err = f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
		assert.True(t, apierrors.IsNotFound(err), "%v", err)
	})

	t.Run("a gateway deleted already is cleaned up", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Second))
		stale := read(t, f.cl, f.gateway)
		_, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		// e.g. a reconcile of the gateway the cache still holds.
		stale.Annotations = map[string]string{annotationCleanupStarted: time.Now().UTC().Format(time.RFC3339)}
		res, err := f.r.cleanup(t.Context(), logr.Discard(), stale, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
	})

	t.Run("within the grace period, a failure is retried later", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Minute))
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.Equal(t, cleanupRetryInterval, res.RequeueAfter)
		assert.Contains(t, read(t, f.cl, f.gateway).Finalizers, FinalizerGWAdminCleanup)
	})

	// triedSince records on the gateway that its cleanup started that long
	// ago. It changes the gateway in the cluster: it is read again.
	triedSince := func(f *pushFixture, since time.Duration) {
		gateway := read(t, f.cl, f.gateway)
		gateway.Annotations = map[string]string{annotationCleanupStarted: time.Now().Add(-since).UTC().Format(time.RFC3339)}
		require.NoError(t, f.cl.Update(t.Context(), gateway))
		f.gateway = read(t, f.cl, f.gateway)
	}

	t.Run("right before the end of the grace period, it is retried at its end", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(cleanupGracePeriod-10*time.Second))
		triedSince(f, cleanupGracePeriod-10*time.Second)
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.LessOrEqual(t, res.RequeueAfter, 10*time.Second)
		assert.GreaterOrEqual(t, res.RequeueAfter, minCleanupRetryInterval)
	})

	t.Run("a gateway deleted long before its first cleanup gets its grace period", func(t *testing.T) {
		// e.g. deleted while the operator didn't run.
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Hour))
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.Equal(t, cleanupRetryInterval, res.RequeueAfter)
		assert.Contains(t, read(t, f.cl, f.gateway).Finalizers, FinalizerGWAdminCleanup)
	})

	t.Run("a finalizer that can't be removed yet is tried again, with a single Warning event", func(t *testing.T) {
		conflicts := 1
		f := newPushFixture(t, interceptor.Funcs{Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
			if conflicts > 0 {
				conflicts--
				return apierrors.NewConflict(schema.GroupResource{}, obj.GetName(), errors.New("changed"))
			}
			return cl.Patch(ctx, obj, p, opts...)
		}}, deleting(cleanupGracePeriod+time.Second))
		triedSince(f, cleanupGracePeriod+time.Second)
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.False(t, res.IsZero(), "retried")
		assert.Empty(t, recordedEvents(f.recorder), "not deleted yet")
		_, err = f.r.cleanup(t.Context(), logr.Discard(), read(t, f.cl, f.gateway), f.gatewayID)
		require.NoError(t, err)
		assert.Len(t, recordedEvents(f.recorder), 1)
	})

	t.Run("past the grace period, the gateway is deleted anyway, with a Warning event", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(cleanupGracePeriod+time.Second))
		triedSince(f, cleanupGracePeriod+time.Second)
		f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
		err = f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
		assert.True(t, apierrors.IsNotFound(err))
		recorded := recordedEvents(f.recorder)
		require.Len(t, recorded, 1)
		assert.Contains(t, recorded[0], "Warning GWAdminCleanupTimedOut Deleted although some gw-admin pod may still serve it")
	})

	t.Run("the GWAdmin is gone: nothing to clean up", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			f.gateway.SetDeletionTimestamp(new(metav1.Now()))
			return []client.Object{f.gateway}
		})
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
	})

	nothingToCleanUp := []struct {
		name   string
		change func(f *pushFixture) []client.Object
	}{
		{
			name: "the GWAdmin has no pod",
			change: func(f *pushFixture) []client.Object {
				f.gwAdmin.EndpointSlice.Endpoints = nil
				return append(f.gwAdmin.Objects(), f.gateway)
			},
		},
		{
			name: "the GWAdmin is gone, but its pods still run",
			change: func(f *pushFixture) []client.Object {
				return []client.Object{f.gwAdmin.EndpointSlice, f.gateway}
			},
		},
		{
			name: "the GWAdmin is being deleted, without its TLS material",
			change: func(f *pushFixture) []client.Object {
				f.gwAdmin.GWAdmin.SetDeletionTimestamp(new(metav1.Now()))
				f.gwAdmin.GWAdmin.SetFinalizers([]string{"example.com/hold"})
				return []client.Object{f.gwAdmin.GWAdmin, f.gwAdmin.Bundle, f.gwAdmin.EndpointSlice, f.gateway}
			},
		},
	}
	for _, tc := range nothingToCleanUp {
		t.Run("nothing to clean up: "+tc.name, func(t *testing.T) {
			f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
				f.gateway.SetDeletionTimestamp(new(metav1.Now()))
				return tc.change(f)
			})
			res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
			require.NoError(t, err)
			assert.True(t, res.IsZero(), "not waited for")
			err = f.cl.Get(t.Context(), client.ObjectKeyFromObject(f.gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
			assert.True(t, apierrors.IsNotFound(err), "the finalizer is removed: %v", err)
			assert.Empty(t, recordedEvents(f.recorder))
			assert.Empty(t, f.pod().recorded())
		})
	}

	t.Run("the pods of a GWAdmin that lost its status are waited for", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			f.gateway.SetDeletionTimestamp(new(metav1.Now()))
			f.gwAdmin.GWAdmin.Status.API = nil
			return append(f.gwAdmin.Objects(), f.gateway)
		})
		res, err := f.r.cleanup(t.Context(), logr.Discard(), f.gateway, f.gatewayID)
		require.NoError(t, err)
		assert.Equal(t, cleanupRetryInterval, res.RequeueAfter)
	})

	t.Run("through Reconcile", func(t *testing.T) {
		f := newPushFixture(t, interceptor.Funcs{}, deleting(time.Second))
		res, err := f.r.Reconcile(t.Context(), f.gateway)
		require.NoError(t, err)
		assert.True(t, res.IsZero())
	})
}

func TestRequeueOnConflict(t *testing.T) {
	conflict := apierrors.NewConflict(schema.GroupResource{}, "x", errors.New("changed"))
	res, err := requeueOnConflict(logr.Discard(), ctrl.Result{}, conflict)
	require.NoError(t, err)
	assert.Equal(t, ctrlconsts.RequeueWithoutBackoff, res.RequeueAfter)

	res, err = requeueOnConflict(logr.Discard(), ctrl.Result{}, errors.Join(errors.New("other"), conflict))
	require.NoError(t, err, "a joined conflict too")
	assert.Equal(t, ctrlconsts.RequeueWithoutBackoff, res.RequeueAfter)

	other := errors.New("other")
	res, err = requeueOnConflict(logr.Discard(), ctrl.Result{RequeueAfter: time.Minute}, other)
	require.ErrorIs(t, err, other)
	assert.Equal(t, time.Minute, res.RequeueAfter)

	res, err = requeueOnConflict(logr.Discard(), ctrl.Result{RequeueAfter: time.Minute}, nil)
	require.NoError(t, err)
	assert.Equal(t, time.Minute, res.RequeueAfter)
}

func TestApplyStatus(t *testing.T) {
	t.Run("the cp endpoint of a GWAdmin not ready, e.g. being rolled out, is published", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newFakeClient(t, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		notReady := gwAdminNotReadyError{msg: "no pod", gwAdmin: newGWAdminFixture(t).GWAdmin}
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, notReady))
		got := read(t, cl, gateway)
		assert.Equal(t, testServerURL, got.Status.ServerURL)
		assert.Equal(t, int64(1), got.Status.ObservedGeneration)
	})
	t.Run("the cp endpoint of a GWAdmin gone is not", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newFakeClient(t, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		target := &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin}
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true, target: target}, nil))
		gateway = read(t, cl, gateway)
		require.Equal(t, testServerURL, gateway.Status.ServerURL)
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, gwAdminNotReadyError{msg: "gone"}))
		assert.Empty(t, read(t, cl, gateway).Status.ServerURL)
	})
	t.Run("a cp endpoint set by someone else is removed when the gateway may not use its GWAdmin", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		gateway.Status.ServerURL = "https://set-by-someone-else.example.com"
		cl := newFakeClient(t, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		notAllowed := gwAdminNotReadyError{msg: "may not", notAllowed: true, gwAdmin: newGWAdminFixture(t).GWAdmin}
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, notAllowed))
		assert.Empty(t, read(t, cl, gateway).Status.ServerURL, "data planes would reach gw-admin there")
	})
	t.Run("an unchanged status isn't applied", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		var applied int
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			SubResourceApply: func(ctx context.Context, cl client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
				applied++
				return cl.SubResource(sub).Apply(ctx, obj, opts...)
			},
		}, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, gwAdminNotReadyError{msg: "no pod"}))
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), read(t, cl, gateway), pushResult{done: true}, gwAdminNotReadyError{msg: "no pod"}))
		assert.Equal(t, 1, applied, "the second one is the status of the cache: no read of the API server, no apply")
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), read(t, cl, gateway), pushResult{done: true}, gwAdminNotReadyError{msg: "other"}))
		assert.Equal(t, 2, applied)
	})
	t.Run("a condition it no longer sets is removed", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newFakeClient(t, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, gwAdminNotReadyError{msg: "no pod"}))
		require.NotNil(t, apimeta.FindStatusCondition(read(t, cl, gateway).Status.Conditions, "GWAdminResolved"))
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), read(t, cl, gateway), pushResult{done: true}, invalidUIDError{}))
		assert.Nil(t, apimeta.FindStatusCondition(read(t, cl, gateway).Status.Conditions, "GWAdminResolved"))
	})
	t.Run("a condition keeps its lastTransitionTime until its status changes", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		past := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
		gateway.Status.Conditions = []metav1.Condition{{
			Type: "ConfigurationPushed", Status: metav1.ConditionFalse, Reason: "ConfigurationPushFailed",
			Message: "pod down", LastTransitionTime: past, ObservedGeneration: 1,
		}}
		cl := newFakeClient(t, gateway)
		r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
		target := &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin, Pods: []gwadminsync.Pod{{Name: "gw-admin-0"}}}

		gateway.Generation = 2
		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true, target: target}, errors.New("pod still down")))
		got := read(t, cl, gateway)
		cond := apimeta.FindStatusCondition(got.Status.Conditions, "ConfigurationPushed")
		require.NotNil(t, cond)
		assert.Equal(t, "pod still down", cond.Message)
		assert.Equal(t, int64(2), cond.ObservedGeneration)
		assert.True(t, past.Equal(&cond.LastTransitionTime), "neither a new message nor a new generation is a transition: %v", cond.LastTransitionTime)

		require.NoError(t, r.applyStatus(t.Context(), logr.Discard(), got, pushResult{done: true, target: target}, nil))
		cond = apimeta.FindStatusCondition(read(t, cl, gateway).Status.Conditions, "ConfigurationPushed")
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, "Pushed to 1 gw-admin pod", cond.Message)
		assert.True(t, cond.LastTransitionTime.After(past.Time), "a new status is a transition")
	})

	t.Run("an error is returned", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		r := &Reconciler{TypeConverter: testTypeConverter, Client: newFakeClientWithInterceptor(t, interceptor.Funcs{
			SubResourceApply: func(context.Context, client.Client, string, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
				return errors.New("unavailable")
			},
		}, gateway)}
		require.ErrorContains(t, r.applyStatus(t.Context(), logr.Discard(), gateway, pushResult{done: true}, errors.New("failed")),
			"failed to update the status: ")
	})
}

func TestPushConditions(t *testing.T) {
	gateway := newTestGateway("gateway")
	byType := func(conditions []metav1.Condition) map[string]metav1.Condition {
		m := map[string]metav1.Condition{}
		for _, c := range conditions {
			m[c.Type] = c
		}
		return m
	}

	pod0, pod1 := gwadminsync.Pod{Name: "gw-admin-0"}, gwadminsync.Pod{Name: "gw-admin-1"}
	target := &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin, Pods: []gwadminsync.Pod{pod0, pod1}}
	pushed := byType(pushConditions(gateway, pushResult{done: true, target: target}, nil))
	assert.Equal(t, metav1.ConditionTrue, pushed["GWAdminResolved"].Status)
	assert.Equal(t, "Resolved", pushed["GWAdminResolved"].Reason)
	assert.Equal(t, "The GWAdmin default/gw-admin serves the gateway", pushed["GWAdminResolved"].Message)
	assert.Equal(t, metav1.ConditionTrue, pushed["Programmed"].Status)
	assert.Equal(t, "Pushed to 2 gw-admin pods", pushed["ConfigurationPushed"].Message)

	someStarting := byType(pushConditions(gateway, pushResult{done: true, target: target, pending: []gwadminsync.Pod{pod1}}, nil))
	assert.Equal(t, metav1.ConditionTrue, someStarting["Programmed"].Status, "a pod serves the gateway")
	assert.Equal(t, "Pushed to 1 gw-admin pod; waiting for the gw-admin pods not serving yet: gw-admin-1", someStarting["ConfigurationPushed"].Message)
	assert.Equal(t, someStarting["ConfigurationPushed"].Message, someStarting["Programmed"].Message, "not every pod serves the gateway yet")

	allStarting := byType(pushConditions(gateway, pushResult{done: true, target: target, pending: []gwadminsync.Pod{pod0, pod1}}, nil))
	assert.Equal(t, metav1.ConditionTrue, allStarting["GWAdminResolved"].Status)
	for _, conditionType := range []string{"ConfigurationPushed", "Programmed"} {
		assert.Equal(t, metav1.ConditionFalse, allStarting[conditionType].Status, "no pod serves the gateway yet")
		assert.Equal(t, "DependenciesNotReady", allStarting[conditionType].Reason)
		assert.Equal(t, "Waiting for the gw-admin pods not serving yet: gw-admin-0, gw-admin-1", allStarting[conditionType].Message)
	}

	// The names of the pods pending, e.g. of a GWAdmin scaled out, are
	// bounded like any message.
	many := make([]gwadminsync.Pod, 0, 200)
	for i := range cap(many) {
		many = append(many, gwadminsync.Pod{Name: fmt.Sprintf("gw-admin-%s-%d", strings.Repeat("x", 40), i)})
	}
	manyTarget := &gwadminsync.Target{GWAdmin: target.GWAdmin, Pods: append([]gwadminsync.Pod{pod0}, many...)}
	for name, pending := range map[string][]gwadminsync.Pod{"some": many, "all": manyTarget.Pods} {
		for conditionType, cond := range byType(pushConditions(gateway, pushResult{done: true, target: manyTarget, pending: pending}, nil)) {
			assert.LessOrEqual(t, len(cond.Message), maxConditionMessageLength, "%s pending: %s", name, conditionType)
		}
	}

	failed := byType(pushConditions(gateway, pushResult{done: true, target: &gwadminsync.Target{GWAdmin: newGWAdminFixture(t).GWAdmin}}, errors.New("pod down")))
	assert.Equal(t, metav1.ConditionTrue, failed["GWAdminResolved"].Status, "a pod failing doesn't make the GWAdmin unresolved")
	assert.Equal(t, "ConfigurationPushFailed", failed["Programmed"].Reason)
	assert.Equal(t, "pod down", failed["Programmed"].Message)

	notAllowed := gwAdminNotReadyError{msg: "may not", reason: eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason, notAllowed: true}
	unresolved := byType(pushConditions(gateway, pushResult{done: true}, errors.Join(notAllowed, errors.New("pod down"))))
	assert.Equal(t, metav1.ConditionFalse, unresolved["GWAdminResolved"].Status)
	assert.Equal(t, "RefNotPermitted", unresolved["GWAdminResolved"].Reason)
	assert.Equal(t, "may not", unresolved["GWAdminResolved"].Message)
	assert.Equal(t, "DependenciesNotReady", unresolved["Programmed"].Reason)
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage, unresolved["Programmed"].Message)
	assert.Equal(t, "DependenciesNotReady", unresolved["ConfigurationPushed"].Reason)
	assert.Equal(t, "may not\npod down", unresolved["ConfigurationPushed"].Message, "the whole error")

	assert.Equal(t, "NotReady", byType(pushConditions(gateway, pushResult{done: true}, gwAdminNotReadyError{msg: "no pod"}))["GWAdminResolved"].Reason)

	invalid := byType(pushConditions(gateway, pushResult{done: true}, invalidUIDError{}))
	assert.NotContains(t, invalid, "GWAdminResolved", "the GWAdmin isn't resolved for a gateway that can't be pushed")
	assert.Equal(t, "InvalidUID", invalid["Programmed"].Reason)
}

func TestFailureReason(t *testing.T) {
	assert.Equal(t, eventgatewayv1alpha1.DependenciesNotReadyReason, failureReason(gwAdminNotReadyError{}))
	assert.Equal(t, eventgatewayv1alpha1.DependenciesNotReadyReason, failureReason(errors.Join(gwAdminNotReadyError{}, errors.New("x"))))
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidUIDReason, failureReason(invalidUIDError{}))
	assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushFailedReason, failureReason(errors.New("x")))
}

func TestForgetDeleted(t *testing.T) {
	r := newPushReconciler()
	gateway := newTestGateway("gateway")
	r.removedFromGWAdmin.Store(gateway.UID, struct{}{})
	r.pendingRetries().Failed(gateway.UID, "inputs", nil, time.Now())
	r.backoff.Failed(gateway.UID, "inputs", errors.New("failed"), time.Now())

	assert.True(t, r.forgetDeleted().Update(event.UpdateEvent{ObjectOld: gateway, ObjectNew: gateway}), "every change, its status too")
	assert.True(t, r.forgetDeleted().Delete(event.DeleteEvent{Object: gateway}))
	_, removed := r.removedFromGWAdmin.Load(gateway.UID)
	assert.False(t, removed)
	pendingWait, _ := r.pendingRetries().Wait(gateway.UID, "inputs", time.Now())
	assert.Zero(t, pendingWait)
	wait, _ := r.backoff.Wait(gateway.UID, "inputs", time.Now())
	assert.Zero(t, wait, "a deleted gateway is forgotten")
}

func TestReconcileSerializesTheGatewaysOfAGWAdmin(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		// With its finalizer already, as the gateway of the fixture: the
		// test reconciles each gateway several times at once, from copies
		// read beforehand, which controller-runtime never does, and adding
		// the finalizer would conflict and requeue without pushing.
		other := newTestGateway("gateway-b")
		other.Finalizers = []string{FinalizerGWAdminCleanup}
		return append(f.gwAdmin.Objects(), f.gateway, other)
	})
	other := read(t, f.cl, newTestGateway("gateway-b"))
	// The fake pod restarts at the start of every push, so that every
	// reconcile pushes it all, and answers slowly. It counts the calls it
	// serves at once: the calls of a reconcile are made one after the other,
	// so two at once are of two reconciles run at once.
	var inFlight, maxInFlight atomic.Int32
	f.pod().respond = func(call string) (*reply, error) {
		if call == callGetEntities {
			f.pod().restart()
		}
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		return nil, nil
	}
	var wg sync.WaitGroup
	var pushed, requeued atomic.Int32
	for range 5 {
		for _, gateway := range []*eventgatewayv1alpha1.OnPremEventGateway{f.gateway, other} {
			wg.Go(func() {
				// Not require: off the goroutine of the test.
				current := &eventgatewayv1alpha1.OnPremEventGateway{}
				if !assert.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), current)) {
					return
				}
				res, err := f.r.Reconcile(t.Context(), current)
				assert.NoError(t, err)
				if res.RequeueAfter == lockRetryInterval {
					requeued.Add(1)
				} else {
					pushed.Add(1)
				}
			})
		}
	}
	wg.Wait()
	assert.Equal(t, int32(10), pushed.Load()+requeued.Load())
	assert.Positive(t, pushed.Load())
	assert.Equal(t, int32(1), maxInFlight.Load(), "the reconciles of the gateways of a GWAdmin run one at a time")

	unlock, ok := f.r.GWAdminSync.TryLock(f.gateway.GWAdminKey(), types.NamespacedName{Namespace: ns, Name: "holder"})
	require.True(t, ok)
	defer unlock()
	res, err := f.r.Reconcile(t.Context(), read(t, f.cl, other))
	require.NoError(t, err)
	assert.Equal(t, lockRetryInterval, res.RequeueAfter, "a reconcile that doesn't get the lock tries again later, not holding its worker")
}

func TestReconcileOnlyWritesWhatChanged(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	_, err := f.r.Reconcile(t.Context(), f.gateway)
	require.NoError(t, err)
	f.pod().reset()

	// The status write of the reconcile triggers it again, as does any
	// change to the gateway: nothing changed for gw-admin.
	res, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.True(t, res.IsZero())
	assert.Equal(t, []string{callGetEntities}, f.pod().recorded(), "only read")

	f.pod().reset()
	f.pod().restart()
	_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Equal(t, []string{callGetEntities, callRegister, callEntities, callExpected + " " + f.gatewayID.String()}, f.pod().recorded(),
		"a restarted pod gets it all")
}

func TestReconcileBacksOff(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	f.pod().respond = failing(callRegister, http.StatusInternalServerError)
	res, err := f.r.Reconcile(t.Context(), f.gateway)
	require.NoError(t, err, "not retried right away")
	assert.Equal(t, 2*time.Second, res.RequeueAfter)
	f.pod().reset()

	// The status write of the failed push triggers it again right away.
	res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter)
	assert.LessOrEqual(t, res.RequeueAfter, 2*time.Second)
	assert.Empty(t, f.pod().recorded(), "not pushed before its backoff")

	// Its status is still reported, e.g. with the cp endpoint of its GWAdmin
	// changed, and set back when someone else changes it.
	gwAdmin := read(t, f.cl, f.gwAdmin.GWAdmin)
	gwAdmin.Status.CP.ServerURL = "https://gw-admin-cp.default.svc:9099"
	require.NoError(t, f.cl.Update(t.Context(), gwAdmin))
	tampered := read(t, f.cl, f.gateway)
	apimeta.SetStatusCondition(&tampered.Status.Conditions, metav1.Condition{
		Type: "Programmed", Status: metav1.ConditionTrue, Reason: "Tampered", ObservedGeneration: 1,
	})
	require.NoError(t, f.cl.Status().Update(t.Context(), tampered))
	res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter)
	assert.Empty(t, f.pod().recorded(), "not pushed before its backoff")
	gateway := read(t, f.cl, f.gateway)
	assert.Equal(t, "https://gw-admin-cp.default.svc:9099", gateway.Status.ServerURL)
	programmed := apimeta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	require.NotNil(t, programmed)
	assert.Equal(t, metav1.ConditionFalse, programmed.Status)
	assert.Equal(t, "ConfigurationPushFailed", programmed.Reason)
	assert.Contains(t, programmed.Message, "failed to register the gateway", "the last failure")
	assert.Empty(t, recordedEvents(f.recorder), "never pushed: no Warning event")

	// A change of its metadata isn't pushed before the backoff either.
	gateway = read(t, f.cl, f.gateway)
	gateway.Annotations = map[string]string{"example.com/note": "changed"}
	require.NoError(t, f.cl.Update(t.Context(), gateway))
	res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter)
	assert.Empty(t, f.pod().recorded(), "not pushed before its backoff")

	// Nor does a change of its spec that leaves its configuration as it is.
	gateway = read(t, f.cl, f.gateway)
	gateway.Generation++
	res, err = f.r.Reconcile(t.Context(), gateway)
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter)
	assert.Empty(t, f.pod().recorded(), "not pushed before its backoff")

	// A new revision of its configuration is pushed right away.
	gateway = read(t, f.cl, f.gateway)
	revision := gateway.Status.ConfigRevision
	gateway.Status.ConfigHash = "sha256:previous"
	require.NoError(t, f.cl.Status().Update(t.Context(), gateway))
	res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, res.RequeueAfter, "pushed, and failed again: a push of another revision starts its backoff over")
	assert.NotEmpty(t, f.pod().recorded())
	assert.Equal(t, revision+1, read(t, f.cl, f.gateway).Status.ConfigRevision)
	f.pod().reset()

	// A change of what is pushed, e.g. another pod to push to, is pushed right
	// away.
	require.NoError(t, f.cl.Delete(t.Context(), f.gwAdmin.EndpointSlice))
	f.gwAdmin.EndpointSlice.ResourceVersion = ""
	f.gwAdmin.EndpointSlice.Endpoints = []discoveryv1.Endpoint{endpoint("gw-admin-0", "10.0.0.1", false), endpoint("gw-admin-1", "10.0.0.2", false)}
	require.NoError(t, f.cl.Create(t.Context(), f.gwAdmin.EndpointSlice))
	f.pod().respond = nil
	_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.NotEmpty(t, f.pod().recorded())
	assert.True(t, apimeta.IsStatusConditionTrue(read(t, f.cl, f.gateway).Status.Conditions, "Programmed"))
}

func TestReconcileStartingPods(t *testing.T) {
	const startingAddress = "10.0.0.3:8083"
	starting := gwadminsynctest.NotServing(endpoint("gw-admin-2", "10.0.0.3", false))
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		f.gwAdmin.EndpointSlice.Endpoints = append(f.gwAdmin.EndpointSlice.Endpoints, starting)
		return append(f.gwAdmin.Objects(), f.gateway)
	})
	// Its container doesn't listen yet, e.g. while its image is pulled.
	refused := &url.Error{Op: "Get", URL: "https://" + startingAddress + "/v1", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}}
	startingPod := &fakeGWAdminClient{respond: func(string) (*reply, error) { return nil, refused }}
	f.pods.add(startingAddress, startingPod)

	res, err := f.r.Reconcile(t.Context(), f.gateway)
	require.NoError(t, err, "a starting pod isn't a failure")
	assert.Equal(t, pendingRetryBase, res.RequeueAfter, "nothing tells once it listens: it is tried again")
	gateway := read(t, f.cl, f.gateway)
	pushed := apimeta.FindStatusCondition(gateway.Status.Conditions, "ConfigurationPushed")
	require.NotNil(t, pushed)
	assert.Equal(t, metav1.ConditionTrue, pushed.Status, "the other pod serves the gateway")
	assert.Equal(t, "Pushed to 1 gw-admin pod; waiting for the gw-admin pods not serving yet: gw-admin-2", pushed.Message)
	assert.Empty(t, recordedEvents(f.recorder))

	// Once it listens, it gets it all.
	startingPod.respond = nil
	res, err = f.r.Reconcile(t.Context(), gateway)
	require.NoError(t, err)
	assert.True(t, res.IsZero())
	assert.Contains(t, startingPod.recorded(), callRegister)
	pushed = apimeta.FindStatusCondition(read(t, f.cl, f.gateway).Status.Conditions, "ConfigurationPushed")
	require.NotNil(t, pushed)
	assert.Equal(t, "Pushed to 2 gw-admin pods", pushed.Message)
}

func TestOnlyDeletions(t *testing.T) {
	gateway := newTestGateway("gateway")
	assert.True(t, onlyDeletions().Delete(event.DeleteEvent{Object: gateway}))
	assert.False(t, onlyDeletions().Create(event.CreateEvent{Object: gateway}))
	assert.False(t, onlyDeletions().Update(event.UpdateEvent{ObjectOld: gateway, ObjectNew: gateway}))
	assert.False(t, onlyDeletions().Generic(event.GenericEvent{Object: gateway}))
}

func TestReconcileWarnsWhenEveryPodIsPending(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	_, err := f.r.Reconcile(t.Context(), f.gateway)
	require.NoError(t, err)
	require.True(t, apimeta.IsStatusConditionTrue(read(t, f.cl, f.gateway).Status.Conditions, "ConfigurationPushed"))

	// Its only pod restarts, and doesn't listen yet.
	f.gwAdmin.EndpointSlice = read(t, f.cl, f.gwAdmin.EndpointSlice)
	f.gwAdmin.EndpointSlice.Endpoints = []discoveryv1.Endpoint{gwadminsynctest.NotServing(endpoint("gw-admin-0", "10.0.0.1", false))}
	require.NoError(t, f.cl.Update(t.Context(), f.gwAdmin.EndpointSlice))
	f.pod().respond = func(string) (*reply, error) { return nil, connectionRefused() }

	res, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Equal(t, pendingRetryBase, res.RequeueAfter)
	recorded := recordedEvents(f.recorder)
	require.Len(t, recorded, 1, "a pushed gateway no gw-admin pod serves anymore")
	assert.Contains(t, recorded[0], "Warning DependenciesNotReady The configuration can't be pushed to gw-admin: Waiting for the gw-admin pods not serving yet: gw-admin-0")

	// e.g. crash looping: tried again less and less often.
	res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	assert.Equal(t, 2*pendingRetryBase, res.RequeueAfter)
	for range 10 {
		res, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	assert.Equal(t, pendingRetryMax, res.RequeueAfter)
	assert.Empty(t, recordedEvents(f.recorder), "reported once")
}

func TestRemovalRetriedOnceItsPodsChange(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, inAnotherNamespace)
	f.pod().respond = failing(callDeleteGateway, http.StatusInternalServerError)
	result, _, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
	require.NoError(t, err)
	require.True(t, result.failed)
	require.Positive(t, result.retryAfter)

	// The pod failing is replaced.
	f.pod().respond = nil
	f.pod().reset()
	slice := read(t, f.cl, f.gwAdmin.EndpointSlice)
	slice.Endpoints = []discoveryv1.Endpoint{endpoint("gw-admin-2", "10.0.0.1", false)}
	require.NoError(t, f.cl.Update(t.Context(), slice))
	result, pushErr, err := f.r.pushConfiguration(t.Context(), f.gateway, f.gatewayID, deriveConfig(f.gateway))
	require.NoError(t, err)
	assert.Zero(t, result.retryAfter, "not waiting for its backoff")
	assert.Equal(t, []string{callExpected + " ", callDeleteGateway}, f.pod().recorded())
	assert.True(t, onlyNotReady(pushErr), "removed")
}

// onlyNotReady reports whether every error joined in err tells the GWAdmin
// isn't ready, e.g. a push that isn't retried.
func onlyNotReady(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if !onlyNotReady(e) {
				return false
			}
		}
		return true
	}
	_, ok := errors.AsType[gwAdminNotReadyError](err)
	return ok
}
