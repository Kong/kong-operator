package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TestKonnectEntityReconcilers tests Konnect entity reconcilers. The test cases are run against a real Kubernetes API
// server provided by the envtest package and a mock Konnect SDK.
func TestKonnectEntityReconcilers(t *testing.T) {
	cfg := Setup(t, scheme.Get())

	testNewKonnectEntityReconciler(t, cfg, konnectv1alpha1.KonnectGatewayControlPlane{}, konnectGatewayControlPlaneTestCases)
	testNewKonnectEntityReconciler(t, cfg, configurationv1alpha1.KongService{}, nil)
	testNewKonnectEntityReconciler(t, cfg, configurationv1.KongConsumer{}, nil)
	testNewKonnectEntityReconciler(t, cfg, configurationv1alpha1.KongRoute{}, nil)
	testNewKonnectEntityReconciler(t, cfg, configurationv1beta1.KongConsumerGroup{}, nil)
	testNewKonnectEntityReconciler(t, cfg, configurationv1alpha1.KongPluginBinding{}, nil)
}

type konnectEntityReconcilerTestCase struct {
	name                string
	objectOps           func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace)
	mockExpectations    func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace)
	eventuallyPredicate func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace)
}

// testNewKonnectEntityReconciler is a helper function to test Konnect entity reconcilers.
// It creates a new namespace for each test case and starts a new controller manager.
// The provided rest.Config designates the Kubernetes API server to use for the tests.
func testNewKonnectEntityReconciler[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	cfg *rest.Config,
	ent T,
	testCases []konnectEntityReconcilerTestCase,
) {
	t.Helper()

	t.Run(ent.GetTypeName(), func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		nsName := uuid.NewString()
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme.Get(),
			Metrics: metricsserver.Options{
				// We do not need metrics server so we set BindAddress to 0 to disable it.
				BindAddress: "0",
			},
		})
		require.NoError(t, err)

		cl := mgr.GetClient()
		factory := ops.NewMockSDKFactory(t)
		sdk := factory.SDK
		reconciler := konnect.NewKonnectEntityReconciler[T, TEnt](factory, false, cl)
		require.NoError(t, reconciler.SetupWithManager(ctx, mgr))

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		require.NoError(t, cl.Create(ctx, ns))

		t.Logf("Starting manager for test case %s", t.Name())
		go func() {
			err := mgr.Start(ctx)
			require.NoError(t, err)
		}()

		const (
			wait = time.Second
			tick = 20 * time.Millisecond
		)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				tc.mockExpectations(t, sdk, ns)
				tc.objectOps(ctx, t, cl, ns)
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					tc.eventuallyPredicate(ctx, collect, cl, ns)
				}, wait, tick)
			})
		}
	})
}
