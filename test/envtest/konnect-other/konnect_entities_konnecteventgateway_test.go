package konnectother

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// TestKonnectEventGatewayUpdate5xxDoesNotHotLoop verifies that a persistent 5xx
// from Konnect on update, carrying a unique trace ID in the error instance for
// each request, does not make the reconciler loop without backoff.
func TestKonnectEventGatewayUpdate5xxDoesNotHotLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectEventGateway](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectEventGateway](&metricsmocks.MockRecorder{}),
		),
	)

	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	const eventGatewayID = "event-gateway-5xx-12345"

	t.Log("Setting up SDK expectations on creation")
	sdk.EventGatewaysSDK.EXPECT().
		CreateEventGateway(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.CreateEventGatewayResponse{
			EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{ID: eventGatewayID},
		}, nil)

	var (
		updateCalls atomic.Int64
		traceID     atomic.Int64
	)
	t.Log("Setting up SDK expectations on update: always 500 with a unique trace ID in the instance")
	sdk.EventGatewaysSDK.EXPECT().
		UpdateEventGateway(mock.Anything, eventGatewayID, mock.Anything).
		RunAndReturn(func(context.Context, string, sdkkonnectcomp.UpdateGatewayRequest, ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayResponse, error) {
			updateCalls.Add(1)
			return nil, &sdkkonnecterrs.InternalError{
				Status:   500,
				Title:    "Internal Server Error",
				Instance: fmt.Sprintf("kong:trace:%d", traceID.Add(1)),
			}
		}).
		Maybe()

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectEventGateway")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	gateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)

	t.Log("Waiting for KonnectEventGateway to be Programmed")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var got konnectv1alpha1.KonnectEventGateway
		if !assert.NoError(c, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(gateway), &got)) {
			return
		}
		assert.Equal(c, eventGatewayID, got.GetKonnectID())
		assert.True(c, k8sutils.IsProgrammed(&got))
	}, consts.WaitTime, consts.TickTime)

	t.Log("Updating the spec to trigger an update against Konnect")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var got konnectv1alpha1.KonnectEventGateway
		if !assert.NoError(c, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(gateway), &got)) {
			return
		}
		got.Spec.APISpec.Description = "updated description"
		assert.NoError(c, clientNamespaced.Update(ctx, &got))
	}, consts.WaitTime, consts.TickTime)

	t.Log("Waiting for Programmed=False condition caused by the 500 error")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var got konnectv1alpha1.KonnectEventGateway
		if !assert.NoError(c, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(gateway), &got)) {
			return
		}
		assert.True(c, envtest.ConditionsContainProgrammedFalse(got.GetConditions()))
	}, consts.WaitTime, consts.TickTime)

	t.Log("Observing that the reconciler does not hot-loop on the failing update")
	// Default controller-runtime backoff (5ms base) allows ~10 retries in 5s;
	// a hot loop produces hundreds.
	const maxCallsInWindow = 15
	callsBefore := updateCalls.Load()
	require.Never(t, func() bool {
		return updateCalls.Load()-callsBefore > maxCallsInWindow
	}, 5*time.Second, 100*time.Millisecond,
		"UpdateEventGateway called too many times: reconciler is hot-looping on 5xx errors")
	t.Logf("UpdateEventGateway calls before window: %d, total: %d", callsBefore, updateCalls.Load())
}
