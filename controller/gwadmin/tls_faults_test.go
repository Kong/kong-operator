package gwadmin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

var errInjected = errors.New("injected API failure")

// faults fails the failAt-th API call, counted from the last reset.
type faults struct{ calls, failAt int }

func (f *faults) next() error {
	f.calls++
	if f.calls == f.failAt {
		return errInjected
	}
	return nil
}

func (f *faults) funcs() interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return c.List(ctx, list, opts...)
		},
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return c.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return apiServerPatch(ctx, c, obj, patch, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if err := f.next(); err != nil {
				return err
			}
			return c.Delete(ctx, obj, opts...)
		},
	}
}

// faultScenario drives a GWAdmin through ensureTLS passes, each run with
// pass.
type faultScenario func(t *testing.T, r *Reconciler, now *time.Time, pass func(*operatorv1alpha1.GWAdmin, *appsv1.Deployment) *tlsMaterial)

// abortScenario stops a scenario once its faulted pass ran.
type abortScenario struct{}

// runFaulted runs scenario, failing the n-th API call of its k-th pass (none
// when n is 0), and checks that pass returns the failure. It reports how many
// API calls the k-th pass made, and whether the scenario has a k-th pass.
func runFaulted(t *testing.T, scenario faultScenario, k, n int) (calls int, reached bool) {
	t.Helper()
	defer func() {
		if v := recover(); v != nil {
			if _, ok := v.(abortScenario); !ok {
				panic(v)
			}
		}
	}()
	now := time.Now()
	f := &faults{}
	// A plain object tracker skips client-go's per-call static REST mapper rebuild, which
	// dominates the cost of the exhaustive fault sweep.
	sm := managerscheme.Get()
	ot := k8stesting.NewObjectTracker(sm, serializer.NewCodecFactory(sm).UniversalDecoder())
	r := reconcilerWith(interceptor.NewClient(fake.NewClientBuilder().WithScheme(sm).WithObjectTracker(ot).Build(), f.funcs()), &now)
	passes := 0
	scenario(t, r, &now, func(gw *operatorv1alpha1.GWAdmin, d *appsv1.Deployment) *tlsMaterial {
		if passes++; passes < k {
			return ensure(t, r, gw, d)
		}
		f.calls, f.failAt = 0, n
		_, err := r.ensureTLS(t.Context(), gw, d)
		f.failAt = 0
		calls, reached = f.calls, true
		if n == 0 {
			require.NoError(t, err, "pass %d", k)
		} else {
			require.ErrorIs(t, err, errInjected, "pass %d, call %d of %d", k, n, calls)
		}
		panic(abortScenario{})
	})
	return calls, reached
}

// sweepFaults fails every API call of every pass of scenario in turn: each
// failure must surface from ensureTLS, never be dropped.
func sweepFaults(t *testing.T, scenario faultScenario) {
	for k := 1; ; k++ {
		calls, reached := runFaulted(t, scenario, k, 0)
		if !reached {
			return
		}
		for n := 1; n <= calls; n++ {
			runFaulted(t, scenario, k, n)
		}
	}
}

func TestEnsureTLS_APIFailuresSurface(t *testing.T) {
	t.Parallel()

	t.Run("automatic CAs rotate with a data plane", func(t *testing.T) {
		t.Parallel()
		sweepFaults(t, func(t *testing.T, r *Reconciler, _ *time.Time, pass func(*operatorv1alpha1.GWAdmin, *appsv1.Deployment) *tlsMaterial) {
			gw := newGWAdmin(nil)
			require.NoError(t, r.Create(t.Context(), gw))
			grantDataPlanes(t, r, dDataPlaneNamespace)
			m := pass(gw, nil)
			m = pass(gw, rolledDeployment(m))
			rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
			dRotate(gw)
			for range 4 {
				m = pass(gw, rollingDeployment(m))
				m = pass(gw, rolledDeployment(m))
				rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
			}
		})
	})

	t.Run("manual certificates change issuer", func(t *testing.T) {
		t.Parallel()
		sweepFaults(t, func(t *testing.T, r *Reconciler, now *time.Time, pass func(*operatorv1alpha1.GWAdmin, *appsv1.Deployment) *tlsMaterial) {
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName), CP: manualCP()})
			require.NoError(t, r.Create(t.Context(), gw))
			require.NoError(t, r.Create(t.Context(), userCASecret(t, "ca-a")))
			require.NoError(t, r.Create(t.Context(), userCPSecretFrom(t, userCASecret(t, "public-a"), time.Time{}, 90*24*time.Hour, false)))
			grantDataPlanes(t, r, dDataPlaneNamespace)
			m := pass(gw, nil)
			m = pass(gw, rolledDeployment(m))
			rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
			rotateManualCA(t, r, "ca-b")
			replaceUserCP(t, r, userCPSecretFrom(t, userCASecret(t, "public-b"), *now, 90*24*time.Hour, false))
			for range 4 {
				m = pass(gw, rollingDeployment(m))
				m = pass(gw, rolledDeployment(m))
				rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
			}
		})
	})
}
