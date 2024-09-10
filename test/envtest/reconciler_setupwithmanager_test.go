package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestNewKonnectEntityReconciler(t *testing.T) {
	// Setup up the envtest environment and share it across the test cases.
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
	eventuallyPredicate func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) bool
}

var konnectGatewayControlPlaneTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should resolve auth",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: ns.Name,
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:  konnectv1alpha1.KonnectAPIAuthTypeToken,
					Token: "kpat_test",
				},
			}
			require.NoError(t, cl.Create(ctx, auth))
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-1",
					Namespace: ns.Name,
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "auth",
						},
					},
				},
			}
			require.NoError(t, cl.Create(ctx, cp))
		},
		eventuallyPredicate: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) bool {
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-1",
					},
					cp,
				),
			)
			// TODO: setup mock Konnect SDK and verify that Konnect CP is "Created".
			return lo.ContainsBy(cp.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == conditions.KonnectEntityAPIAuthConfigurationResolvedRefConditionType && condition.Status == metav1.ConditionTrue
			})
		},
	},
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

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme.Get(),
			Metrics: metricsserver.Options{
				// We do not need metrics server so we set BindAddress to 0 to disable it.
				BindAddress: "0",
			},
		})
		require.NoError(t, err)

		cl := mgr.GetClient()
		reconciler := konnect.NewKonnectEntityReconciler[T, TEnt](&ops.MockSDKFactory{}, false, cl)
		require.NoError(t, reconciler.SetupWithManager(ctx, mgr))

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
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
				tc.objectOps(ctx, t, cl, ns)
				require.Eventually(t, func() bool {
					return tc.eventuallyPredicate(ctx, t, cl, ns)
				}, wait, tick)
			})
		}
	})
}
