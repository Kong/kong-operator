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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newIndexedFakeClient builds a fake client with the MCPServerDataPlane->MCPServer
// index registered, matching how the manager registers it for real.
func newIndexedFakeClient(objs ...client.Object) client.Client {
	builder := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(objs...)
	for _, opt := range index.OptionsForMCPServerDataPlane() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	return builder.Build()
}

// errListClient wraps a client.Client and fails every List call, to exercise
// the map func's error branch.
type errListClient struct{ client.Client }

func (c *errListClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return assert.AnError
}

func Test_enqueueMCPServerForMCPServerDataPlane(t *testing.T) {
	const ns = "default"

	// mcpServer has a fetcher-style hashed name, distinct from the DataPlane's
	// user-chosen name -- this is the shape that broke with
	// handler.EnqueueRequestForObject.
	mcpServer := &konnectv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "test-cp-1a2b3c4d"},
	}

	dpMatching := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "mcpserver1"},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: mcpServer.Name},
			},
		},
	}
	dpMatchingSecond := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "mcpserver1-second"},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: mcpServer.Name},
			},
		},
	}
	dpOther := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "unrelated"},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: "other-mcp-server"},
			},
		},
	}
	dpOtherNamespace := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: "other-ns", Name: "mcpserver1"},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: mcpServer.Name},
			},
		},
	}

	cl := newIndexedFakeClient(dpMatching, dpMatchingSecond, dpOther, dpOtherNamespace)

	tests := []struct {
		name string
		cl   client.Client
		obj  client.Object
		want []ctrl.Request
	}{
		{
			name: "returns requests for MCPServerDataPlanes referencing the MCPServer, not the MCPServer itself",
			cl:   cl,
			obj:  mcpServer,
			want: []ctrl.Request{
				{NamespacedName: types.NamespacedName{Namespace: ns, Name: dpMatching.Name}},
				{NamespacedName: types.NamespacedName{Namespace: ns, Name: dpMatchingSecond.Name}},
			},
		},
		{
			name: "ignores MCPServerDataPlanes in another namespace",
			cl:   newIndexedFakeClient(dpOtherNamespace),
			obj:  mcpServer,
			want: nil,
		},
		{
			name: "returns nil when obj is not a MCPServer",
			cl:   cl,
			obj:  &mcpv1alpha1.MCPServerDataPlane{},
			want: nil,
		},
		{
			name: "returns nil when List fails",
			cl:   &errListClient{Client: cl},
			obj:  mcpServer,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapFunc := enqueueMCPServerForMCPServerDataPlane(tc.cl)
			got := mapFunc(t.Context(), tc.obj)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// Test_MCPServerDataPlaneReconciler_reconcileEventHandler is the regression test
// for the bug where the fast-path reconcile nudge pushed on ReconcileEventCh
// never reached the MCPServerDataPlane reconciler: the channel carries
// *konnectv1alpha1.MCPServer objects with a fetcher-generated hashed name, but
// the consumer reconciles MCPServerDataPlane objects, which users name freely.
// handler.EnqueueRequestForObject blindly keyed the reconcile.Request off the
// MCPServer's own (hashed) name, so it almost never matched a real
// MCPServerDataPlane and the signal was silently dropped as NotFound.
func Test_MCPServerDataPlaneReconciler_reconcileEventHandler(t *testing.T) {
	const ns = "default"

	mcpServer := &konnectv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "test-cp-1a2b3c4d"},
	}
	mcpDataPlane := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "mcpserver1"},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: mcpServer.Name},
			},
		},
	}

	r := &MCPServerDataPlaneReconciler{
		Client: newIndexedFakeClient(mcpDataPlane),
	}

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	defer q.ShutDown()

	// This is exactly what mcpserverfetcher.go pushes onto ReconcileEventCh.
	r.reconcileEventHandler().Generic(t.Context(), event.GenericEvent{Object: mcpServer}, q)

	require.Equal(t, 1, q.Len())
	got, shutdown := q.Get()
	require.False(t, shutdown)
	q.Done(got)

	assert.Equal(t, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: mcpDataPlane.Name}}, got)
	assert.NotEqual(t, mcpServer.Name, got.Name, "request must be keyed by the MCPServerDataPlane, not the MCPServer")
}

// Test_MCPServerDataPlaneReconciler_DeletionDoesNotResetSignalOffset is the
// regression test for the other half of the misplaced-finalizer bug: deleting
// an MCPServerDataPlane must not reset the referenced MCPServer's
// signal-polling offset. That reset belongs solely to MCPServerSignalReconciler,
// triggered by the mirrored MCPServer's own deletion (see
// mcpserver_signal_controller_test.go). Before the fix, this reconciler ran the
// finalizer/notify dance on the MCPServerDataPlane itself, so tearing down a
// workload for an otherwise-untouched MCPServer wrongly reset the whole
// control plane's offset.
func Test_MCPServerDataPlaneReconciler_DeletionDoesNotResetSignalOffset(t *testing.T) {
	const (
		ns     = "default"
		cpName = "my-cp"
	)

	mcpServer := &konnectv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "mcp-1",
			OwnerReferences: []metav1.OwnerReference{
				cpOwnerRef(cpName),
			},
		},
	}
	now := metav1.NewTime(time.Now())
	mcpDataPlane := &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "mcpserver1",
			// Kept alive by an unrelated finalizer so the fake client accepts a
			// DeletionTimestamp on an object this reconciler no longer finalizes.
			Finalizers:        []string{"test.example.com/hold"},
			DeletionTimestamp: &now,
		},
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type:                 mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{Name: mcpServer.Name},
			},
		},
	}

	cl := newIndexedFakeClient(mcpServer, mcpDataPlane)
	sm := newTestSignalManager(t)
	resetCh := registerResetCh(sm, ns, cpName)

	r := &MCPServerDataPlaneReconciler{Client: cl, SignalManager: sm}

	// SdkFactory is intentionally left nil: if the reconciler tried to reach
	// Konnect from the deletion path, this would panic.
	_, err := r.Reconcile(t.Context(), mcpDataPlane)
	require.NoError(t, err)

	select {
	case <-resetCh:
		t.Fatal("MCPServerDataPlane deletion must not reset the MCPServer's signal offset")
	default:
	}
}
