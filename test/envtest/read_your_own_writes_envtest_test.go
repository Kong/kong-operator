package envtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// configMapWatcher is a no-op controller watching ConfigMaps. Its only purpose is
// to make the manager start the cache and its ConfigMap informer, the way the
// operator's controllers do in production.
type configMapWatcher struct{}

func (configMapWatcher) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Complete(reconcile.Func(func(context.Context, ctrl.Request) (ctrl.Result, error) {
			return ctrl.Result{}, nil
		}))
}

// TestReadYourOwnWritesConsistency guards the manager's
// EnableReadYourWritesConsistency option (see modules/manager/run.go): a read
// through the manager client that follows a write through the same client must
// observe that write.
//
// This is the shape of a quick subsequent reconcile. A controller persists e.g. a
// Konnect ID to .status, the resulting watch event enqueues the next reconcile,
// and that reconcile reads the object back. Without the option the read is served
// by the informer cache before it has seen the write, so the ID still looks empty
// and the operator creates the same Konnect entity a second time, which Konnect
// rejects with 409 Conflict.
func TestReadYourOwnWritesConsistency(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	StartReconcilers(ctx, t, mgr, logs, configMapWatcher{})

	// Both the write and the read go through the manager's cached client. Without
	// EnableReadYourWritesConsistency the read races the informer and returns
	// NotFound; with it the read waits for the cache to observe the write.
	//
	// The loop is what makes the race reliable rather than occasional: on a local
	// API server the informer usually wins a single create/read pair, but a burst
	// of them makes it fall behind.
	cl := mgr.GetClient()
	for i := range 100 {
		cm := &corev1.ConfigMap{
			Name:      fmt.Sprintf("ryow-%d", i),
			Namespace: ns.Name,
		}
		require.NoError(t, cl.Create(ctx, cm))

		require.NoErrorf(t, cl.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}),
			"read %d after write through the same client must observe the write", i,
		)
	}
}
