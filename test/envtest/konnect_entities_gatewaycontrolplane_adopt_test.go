package envtest

import (
	"context"
	"testing"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongGatewayControlPlaneAdopt(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectGatewayControlPlane](konnectInfiniteSyncTime),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	testCPID := "test-12345"

	t.Log("Setting up SDK expectations on KonnectGatewayControlPlane getting")
	sdk.ControlPlaneSDK.EXPECT().GetControlPlane(
		mock.Anything,
		testCPID,
		mock.Anything,
	).Return(
		&sdkkonnectops.GetControlPlaneResponse{},
		nil,
	)

	t.Log("Setting up a watch for KonnectGatewayControlPlane events")
	w := setupWatch[konnectv1alpha1.KonnectGatewayControlPlaneList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KonnectGatewayControlPlane")
	cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, apiAuth, deploy.WithAnnotation(
		konnect.AnnotationKeyAdoptEntity, testCPID,
	))

	t.Log("Waiting for Get of KonnectGatewayControlPlane in SDK")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.ControlPlaneSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Watching for KonnectGatewayControlPlane to verify that the KonnectID, finalizer, and programmend conditions are set")
	watchFor(t, ctx, w, watch.Modified, func(c *konnectv1alpha1.KonnectGatewayControlPlane) bool {
		if c.GetName() != cp.GetName() {
			return false
		}
		return c.GetKonnectID() == testCPID && k8sutils.IsProgrammed(c) && lo.Contains(c.Finalizers, konnect.KonnectCleanupFinalizer)
	}, "KonnectGatewayControlPlane should be programmed, has finalizer, and have the ID same as in the adopt annotation")

}
