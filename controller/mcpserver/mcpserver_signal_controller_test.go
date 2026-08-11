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

package mcpserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

// cpOwnerRef builds an OwnerReference matching what ownerControlPlaneName looks for.
func cpOwnerRef(cpName string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: konnectv1alpha2.GroupVersion.String(),
		Kind:       "KonnectGatewayControlPlane",
		Name:       cpName,
	}
}

// registerResetCh registers a reset channel for namespace/cpName on sm, the same
// way registerControlPlane does, so tests can observe NotifyMCPServerDeleted.
func registerResetCh(sm *SignalManager, namespace, cpName string) chan struct{} {
	resetCh := make(chan struct{}, 1)
	sm.mu.Lock()
	sm.resetChs[namespace+"/"+cpName] = resetCh
	sm.mu.Unlock()
	return resetCh
}

// Test_MCPServerSignalReconciler_Reconcile is the regression test for the bug
// where the signal-reset finalizer was moved onto MCPServerDataPlane instead of
// staying on the mirrored MCPServer: the fetcher's own stale-mirror delete
// (mcpserverfetcher.go) has no finalizer to intercept it, so the signal offset
// reset never fired, and deleting an unrelated MCPServerDataPlane fired it
// spuriously. MCPServerSignalReconciler restores add/remove of mcpServerFinalizer
// and the NotifyMCPServerDeleted call on the MCPServer's own lifecycle.
func Test_MCPServerSignalReconciler_Reconcile(t *testing.T) {
	const (
		ns     = "default"
		cpName = "my-cp"
	)

	t.Run("mirror owned by a control plane gets the finalizer added", func(t *testing.T) {
		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       ns,
				Name:            "mcp-1",
				OwnerReferences: []metav1.OwnerReference{cpOwnerRef(cpName)},
			},
		}
		cl := newIndexedFakeClient(mcpServer)
		sm := newTestSignalManager(t)
		r := &MCPServerSignalReconciler{Client: cl, SignalManager: sm}

		_, err := r.Reconcile(t.Context(), mcpServer)
		require.NoError(t, err)

		var got konnectv1alpha1.MCPServer
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(mcpServer), &got))
		assert.Contains(t, got.Finalizers, mcpServerFinalizer)
	})

	t.Run("MCPServer with no control plane owner is left alone", func(t *testing.T) {
		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "mcp-orphan"},
		}
		cl := newIndexedFakeClient(mcpServer)
		sm := newTestSignalManager(t)
		r := &MCPServerSignalReconciler{Client: cl, SignalManager: sm}

		_, err := r.Reconcile(t.Context(), mcpServer)
		require.NoError(t, err)

		var got konnectv1alpha1.MCPServer
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(mcpServer), &got))
		assert.Empty(t, got.Finalizers)
	})

	t.Run("regression: foreign deletion of the mirror resets the signal offset", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         ns,
				Name:              "mcp-1",
				OwnerReferences:   []metav1.OwnerReference{cpOwnerRef(cpName)},
				Finalizers:        []string{mcpServerFinalizer},
				DeletionTimestamp: &now,
			},
		}
		cl := newIndexedFakeClient(mcpServer)
		sm := newTestSignalManager(t)
		resetCh := registerResetCh(sm, ns, cpName)
		r := &MCPServerSignalReconciler{Client: cl, SignalManager: sm}

		_, err := r.Reconcile(t.Context(), mcpServer)
		require.NoError(t, err)

		select {
		case <-resetCh:
			// signal offset reset as expected
		default:
			t.Fatal("expected NotifyMCPServerDeleted to fire, but resetCh was empty")
		}

		var got konnectv1alpha1.MCPServer
		err = cl.Get(t.Context(), client.ObjectKeyFromObject(mcpServer), &got)
		require.True(t, apierrors.IsNotFound(err), "expected mirror to be fully deleted once the finalizer was removed")
	})

	t.Run("deleting a mirror whose control plane is not registered still removes the finalizer", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         ns,
				Name:              "mcp-1",
				OwnerReferences:   []metav1.OwnerReference{cpOwnerRef("unregistered-cp")},
				Finalizers:        []string{mcpServerFinalizer},
				DeletionTimestamp: &now,
			},
		}
		cl := newIndexedFakeClient(mcpServer)
		sm := newTestSignalManager(t) // no resetCh registered for "unregistered-cp"
		r := &MCPServerSignalReconciler{Client: cl, SignalManager: sm}

		require.NotPanics(t, func() {
			_, err := r.Reconcile(t.Context(), mcpServer)
			require.NoError(t, err)
		})

		var got konnectv1alpha1.MCPServer
		err := cl.Get(t.Context(), client.ObjectKeyFromObject(mcpServer), &got)
		require.True(t, apierrors.IsNotFound(err), "mirror must not get stuck just because its control plane isn't registered")
	})

	t.Run("deleting a mirror that never carried the signal finalizer does not reset the offset", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		mcpServer := &konnectv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns,
				Name:      "mcp-1",
				OwnerReferences: []metav1.OwnerReference{
					cpOwnerRef(cpName),
				},
				// Kept alive by an unrelated finalizer so the fake client accepts a
				// DeletionTimestamp without mcpServerFinalizer, matching the "someone
				// else already handled cleanup" case.
				Finalizers:        []string{"test.example.com/hold"},
				DeletionTimestamp: &now,
			},
		}
		cl := newIndexedFakeClient(mcpServer)
		sm := newTestSignalManager(t)
		resetCh := registerResetCh(sm, ns, cpName)
		r := &MCPServerSignalReconciler{Client: cl, SignalManager: sm}

		_, err := r.Reconcile(t.Context(), mcpServer)
		require.NoError(t, err)

		select {
		case <-resetCh:
			t.Fatal("did not expect a reset for a mirror that never carried mcpServerFinalizer")
		default:
		}
	})
}
