package konnectapigw

import (
	"slices"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
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

func TestKongPluginBindingOrdering(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()

	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
	seed := seedClient(t, cfg, ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, seed)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, seed, apiAuth)

	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK

	// KongPlugin changes reach Konnect on the next sync, so keep the period short.
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongPluginBinding](time.Second),
			konnect.WithMetricRecorder[configurationv1alpha1.KongPluginBinding](&metricsmocks.MockRecorder{}),
		),
	)

	plugin := &configurationv1.KongPlugin{
		GenerateName: "request-termination-kp-",
		PluginName:   "request-termination",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"status_code": 418}`),
		},
		Ordering: &kong.PluginOrdering{
			Before: kong.PluginOrderingPhase{"access": {"key-auth"}},
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, plugin))
	pluginID := uuid.NewString()

	// Upserts repeat on every sync, so expectations match by ordering and are not limited to one call.
	expectUpsert := func(matches func(*sdkkonnectcomp.Ordering) bool) *mock.Call {
		return sdk.PluginSDK.EXPECT().
			UpsertPlugin(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertPluginRequest) bool {
					return req.PluginID == pluginID && matches(req.Plugin.Ordering)
				}),
			).
			Return(&sdkkonnectops.UpsertPluginResponse{}, nil).Call
	}
	setOrdering := func(t *testing.T, ordering *kong.PluginOrdering) {
		t.Helper()
		require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var kp configurationv1.KongPlugin
			if err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(plugin), &kp); err != nil {
				return err
			}
			kp.Ordering = ordering
			return clientNamespaced.Update(ctx, &kp)
		}))
	}

	sdk.PluginSDK.EXPECT().
		CreatePlugin(
			mock.Anything,
			mock.MatchedBy(func(r sdkkonnectops.CreatePluginRequest) bool {
				return r.ControlPlaneID == cp.GetKonnectStatus().GetKonnectID() &&
					slices.Equal(r.Plugin.Ordering.GetBefore().GetAccess(), []string{"key-auth"})
			}),
		).
		Return(&sdkkonnectops.CreatePluginResponse{Plugin: &sdkkonnectcomp.Plugin{ID: new(pluginID)}}, nil).
		Once()
	// The first sync may happen after the ordering below is already changed.
	expectUpsert(func(o *sdkkonnectcomp.Ordering) bool {
		return slices.Equal(o.GetBefore().GetAccess(), []string{"key-auth"}) && o.GetAfter() == nil
	}).Maybe()

	kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
		konnect.NewKongPluginBindingBuilder().
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			WithPluginRefName(plugin.Name).
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
	)
	assert.EventuallyWithT(t,
		envtest.AssertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
		consts.WaitTime, consts.TickTime,
		"KongPluginBinding wasn't created using Konnect API or its KonnectID wasn't set",
	)

	assertProgrammed := func(t *testing.T, status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string) {
		t.Helper()
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			var got configurationv1alpha1.KongPluginBinding
			if !assert.NoError(c, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), &got)) {
				return
			}
			cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &got)
			if !assert.True(c, ok) {
				return
			}
			assert.Equal(c, status, cond.Status)
			assert.Equal(c, string(reason), cond.Reason)
			assert.Contains(c, cond.Message, message)
		}, consts.WaitTime, consts.TickTime)
	}

	t.Run("updated ordering is sent to Konnect", func(t *testing.T) {
		expectUpsert(func(o *sdkkonnectcomp.Ordering) bool {
			return o.GetBefore() == nil && slices.Equal(o.GetAfter().GetAccess(), []string{"rate-limiting"})
		})
		setOrdering(t, &kong.PluginOrdering{
			After: kong.PluginOrderingPhase{"access": {"rate-limiting"}},
		})
		envtest.EventuallyAssertSDKExpectations(t, sdk.PluginSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("removed ordering is cleared in Konnect", func(t *testing.T) {
		expectUpsert(func(o *sdkkonnectcomp.Ordering) bool { return o == nil })
		setOrdering(t, nil)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PluginSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("non-access phase sets Programmed=False", func(t *testing.T) {
		setOrdering(t, &kong.PluginOrdering{
			Before: kong.PluginOrderingPhase{"header_filter": {"key-auth"}},
		})
		assertProgrammed(t, metav1.ConditionFalse, kcfgkonnect.KonnectEntitiesFailedToUpdateReason, `plugin ordering phase "header_filter" is not supported by Konnect`)
	})

	t.Run("binding recovers once the ordering is valid again", func(t *testing.T) {
		setOrdering(t, nil)
		assertProgrammed(t, metav1.ConditionTrue, konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed, "")
	})

	sdk.PluginSDK.EXPECT().
		DeletePlugin(mock.Anything, sdkkonnectops.DeletePluginRequest{
			ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			PluginID:       pluginID,
		}).
		Return(&sdkkonnectops.DeletePluginResponse{StatusCode: 200}, nil)
	require.NoError(t, clientNamespaced.Delete(ctx, kpb))
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, apierrors.IsNotFound(clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb)))
	}, consts.WaitTime, consts.TickTime, "KongPluginBinding did not get deleted but should have")
	envtest.EventuallyAssertSDKExpectations(t, sdk.PluginSDK, consts.WaitTime, consts.TickTime)
}
