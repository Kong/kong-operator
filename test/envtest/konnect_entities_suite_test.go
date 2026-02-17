package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// TestKonnectEntityReconcilers tests Konnect entity reconcilers. The test cases are run against a real Kubernetes API
// server provided by the envtest package and a mock Konnect SDK.
func TestKonnectEntityReconcilers(t *testing.T) {
	cfg, _ := Setup(t, t.Context(), scheme.Get())

	testNewKonnectEntityReconciler(t, cfg, konnectv1alpha2.KonnectGatewayControlPlane{}, konnectGatewayControlPlaneTestCases)
}

type konnectEntityReconcilerTestCase struct {
	enabled             bool
	name                string
	objectOps           func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace)
	mockExpectations    func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace)
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

		ctx := t.Context()

		mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: NameFromT(t),
			},
		}
		require.NoError(t, mgr.GetClient().Create(ctx, ns))

		cl := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
		factory := sdkmocks.NewMockSDKFactory(t)
		sdk := factory.SDK

		StartReconcilers(ctx, t, mgr, logs,
			konnect.NewKonnectEntityReconciler(
				factory, logging.DevelopmentMode, cl,
				konnect.WithMetricRecorder[T, TEnt](&metricsmocks.MockRecorder{})))

		const (
			wait = 10 * time.Second
			tick = 200 * time.Millisecond
		)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				if !tc.enabled {
					t.Skip("test case disabled")
				}
				tc.mockExpectations(t, sdk, cl, ns)
				tc.objectOps(ctx, t, cl, ns)
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					tc.eventuallyPredicate(ctx, collect, cl, ns)
				}, wait, tick)
			})
		}
	})
}
