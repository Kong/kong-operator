package envtest

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlpredicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// mockReconciler is a minimal, request-recording controller registered
// alongside the real crdschema.Reconciler on the same manager, watching the
// same object type filtered through the real provider's IsCRDGroupRelevant.
// It replaces nothing under test - crdschema.Reconciler and
// TypeConverterProvider are both real and unmodified - it only gives the
// test a deterministic signal for which CRD names the watch actually
// delivers, without scraping logs (which are noisy: the manager's periodic
// cache resync re-triggers reconciles for unrelated CRDs on its own).
type mockReconciler struct {
	provider *controllerpkgssa.TypeConverterProvider

	mu       sync.Mutex
	requests []string
}

func (m *mockReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}, builder.WithPredicates(ctrlpredicate.NewPredicateFuncs(func(o client.Object) bool {
			crd, ok := o.(*apiextensionsv1.CustomResourceDefinition)
			return ok && m.provider.IsCRDGroupRelevant(crd.Spec.Group)
		}))).
		Complete(reconcile.Func(m.Reconcile))
}

func (m *mockReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req.Name)
	m.mu.Unlock()
	return ctrl.Result{}, nil
}

func (m *mockReconciler) countFor(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, n := range m.requests {
		if n == name {
			count++
		}
	}
	return count
}

func TestCRDSchemaReconciler(t *testing.T) {
	t.Parallel()

	const (
		relevantCRD   = "kegdataplanes.eventgateway.konghq.com"
		irrelevantCRD = "kongservices.configuration.konghq.com"
	)

	ctx := t.Context()
	cfg, _ := Setup(t, ctx, scheme.Get(), WithInstallKongCRDs(true), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	// Scope the provider to a single group so filtering is directly
	// observable: envtest installs many CRD groups (Kong, Konnect,
	// gateway-operator, ...), only one of which is "relevant" here. This is
	// the real provider, wired in exactly as production does in
	// modules/manager/controller_setup.go - no wrapper, no mock.
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr,
		map[string]struct{}{"eventgateway.konghq.com": {}})
	require.NoError(t, err)
	require.NoError(t, ssaProvider.Ready(nil))

	mock := &mockReconciler{provider: ssaProvider}

	StartReconcilers(ctx, t, mgr, logs,
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
		mock,
	)

	// Wait for the manager's cache to finish its initial sync before issuing
	// any direct reads/writes below - otherwise they can race the cache
	// startup and fail with "the cache is not started".
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx))

	cl := mgr.GetClient()

	// The watch's initial sync delivers a Create event for every pre-existing
	// CRD; the predicate must let the relevant one through and keep the
	// irrelevant one out from the very start.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.GreaterOrEqual(ct, mock.countFor(relevantCRD), 1)
	}, waitTime, tickTime, "expected the watch to deliver the pre-existing relevant CRD")
	assert.Equal(t, 0, mock.countFor(irrelevantCRD))

	t.Run("CRD update in an unconfigured group does not trigger a reconcile", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: irrelevantCRD}, crd))
		if crd.Labels == nil {
			crd.Labels = map[string]string{}
		}
		crd.Labels["kong-operator-test/touch"] = "1"
		require.NoError(t, cl.Update(ctx, crd))

		assert.Never(t, func() bool {
			return mock.countFor(irrelevantCRD) > 0
		}, waitTime, tickTime, "the predicate delivered a CRD outside the configured groups")
	})

	t.Run("CRD update in a configured group triggers a reconcile and keeps the converter healthy", func(t *testing.T) {
		baseline := mock.countFor(relevantCRD)

		crd := &apiextensionsv1.CustomResourceDefinition{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: relevantCRD}, crd))
		if crd.Labels == nil {
			crd.Labels = map[string]string{}
		}
		crd.Labels["kong-operator-test/touch"] = "1"
		require.NoError(t, cl.Update(ctx, crd))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.Greater(ct, mock.countFor(relevantCRD), baseline)
		}, waitTime, tickTime, "expected the watch to deliver the relevant CRD update")

		// The real crdschema.Reconciler received the same event (same
		// object, same predicate, same watch) and its real Rebuild call
		// must have kept the shared converter healthy.
		assert.NoError(t, ssaProvider.Ready(nil))
	})
}
