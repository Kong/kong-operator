package konnect

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/envtest"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestNewKonnectEntityReconciler(t *testing.T) {
	testNewKonnectEntityReconciler(t, konnectv1alpha1.KonnectGatewayControlPlane{}, konnectGatewayControlPlaneTestCases)
	testNewKonnectEntityReconciler(t, configurationv1alpha1.KongService{}, nil)
	testNewKonnectEntityReconciler(t, configurationv1.KongConsumer{}, nil)
	testNewKonnectEntityReconciler(t, configurationv1alpha1.KongRoute{}, nil)
	testNewKonnectEntityReconciler(t, configurationv1beta1.KongConsumerGroup{}, nil)
	testNewKonnectEntityReconciler(t, configurationv1alpha1.KongPluginBinding{}, nil)
}

const (
	testNamespaceName   = "test"
	envTestWaitDuration = time.Second
	envTestWaitTick     = 20 * time.Millisecond
)

type konnectEntityReconcilerTestCase struct {
	name                string
	objectOps           func(ctx context.Context, t *testing.T, cl client.Client)
	eventuallyPredicate func(ctx context.Context, t *testing.T, cl client.Client) bool
}

var konnectGatewayControlPlaneTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should resolve auth",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client) {
			auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: testNamespaceName,
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
					Namespace: testNamespaceName,
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
		eventuallyPredicate: func(ctx context.Context, t *testing.T, cl client.Client) bool {
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{}
			err := cl.Get(ctx, k8stypes.NamespacedName{Namespace: testNamespaceName, Name: "cp-1"}, cp)
			require.NoError(t, err)
			// TODO: setup mock Konnect SDK and verify that Konnect CP is "Created".
			return lo.ContainsBy(cp.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == conditions.KonnectEntityAPIAuthConfigurationResolvedRefConditionType && condition.Status == metav1.ConditionTrue
			})
		},
	},
}

func testNewKonnectEntityReconciler[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	ent T,
	testCases []konnectEntityReconcilerTestCase,
) {
	t.Helper()

	sdkFactory := &ops.MockSDKFactory{}

	t.Run(ent.GetTypeName(), func(t *testing.T) {
		s := scheme.Get()
		require.NoError(t, configurationv1alpha1.AddToScheme(s))
		require.NoError(t, configurationv1beta1.AddToScheme(s))
		require.NoError(t, konnectv1alpha1.AddToScheme(s))
		cfg := envtest.Setup(t, s)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: s,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		require.NoError(t, err)

		cl := mgr.GetClient()
		reconciler := NewKonnectEntityReconciler[T, TEnt](sdkFactory, false, cl)
		require.NoError(t, reconciler.SetupWithManager(mgr))

		err = cl.Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		})
		require.NoError(t, err)

		t.Logf("Starting manager for test case %s", t.Name())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			err := mgr.Start(ctx)
			require.NoError(t, err)
		}()

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				tc.objectOps(ctx, t, cl)
				require.Eventually(t, func() bool {
					return tc.eventuallyPredicate(ctx, t, cl)
				}, envTestWaitDuration, envTestWaitTick)
			})
		}
	})
}
