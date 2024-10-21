package envtest

import (
	"context"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumerGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1beta1.KongConsumerGroup](konnectInfiniteSyncTime),
		),
	}
	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up a watch for KongConsumerGroup events")
	cWatch := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("should create, update and delete ConsumerGroup successfully", func(t *testing.T) {
		const (
			cgID          = "consumer-id"
			cgName        = "consumer-group"
			updatedCGName = "consumer-group-updated"
		)
		t.Log("Setting up SDK expectations on KongConsumerGroup creation")
		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroupInput) bool {
					return cg.Name == cgName
				}),
			).Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr(cgID),
			},
		}, nil,
		)

		t.Log("Creating KongConsumerGroup")
		cg := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		watchFor(t, ctx, cWatch, watch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != cg.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		t.Log("Waiting for KongConsumerGroup to be created in the SDK")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ConsumerGroupSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongConsumerGroup update")
		sdk.ConsumerGroupSDK.EXPECT().
			UpsertConsumerGroup(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerGroupRequest) bool {
				return r.ConsumerGroupID == cgID && r.ConsumerGroup.Name == updatedCGName
			})).
			Return(&sdkkonnectops.UpsertConsumerGroupResponse{}, nil)

		t.Log("Patching KongConsumerGroup")
		cgToPatch := cg.DeepCopy()
		cgToPatch.Spec.Name = updatedCGName
		require.NoError(t, clientNamespaced.Patch(ctx, cgToPatch, client.MergeFrom(cg)))

		t.Log("Waiting for KongConsumerGroup to be updated in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ConsumerGroupSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongConsumerGroup deletion")
		sdk.ConsumerGroupSDK.EXPECT().
			DeleteConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), cgID).
			Return(&sdkkonnectops.DeleteConsumerGroupResponse{}, nil)

		t.Log("Deleting KongConsumerGroup")
		require.NoError(t, cl.Delete(ctx, cg))

		require.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(cg), cg),
				))
			}, waitTime, tickTime,
		)

		t.Log("Waiting for KongConsumerGroup to be deleted in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ConsumerGroupSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})

	t.Run("should create ConsumerGroup successfully on conflict when ConsumerGroup with matching uid tag exists", func(t *testing.T) {
		const (
			cgID          = "consumer-id-2"
			cgName        = "consumer-group-2"
			updatedCGName = "consumer-group-updated-2"
		)
		t.Log("Setting up SDK expectations on KongConsumerGroup creation")
		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroupInput) bool {
					return cg.Name == cgName
				}),
			).Return(
			nil,
			&sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "name",
							"messages": [
								"name (type: unique) constraint failed"
							]
						}
					]
				}`,
			},
		)

		sdk.ConsumerGroupSDK.EXPECT().
			ListConsumerGroup(mock.Anything,
				mock.MatchedBy(func(r sdkkonnectops.ListConsumerGroupRequest) bool {
					return r.ControlPlaneID == cp.GetKonnectStatus().GetKonnectID() &&
						r.Tags != nil && strings.HasPrefix(*r.Tags, ops.KubernetesUIDLabelKey)
				}),
			).Return(
			&sdkkonnectops.ListConsumerGroupResponse{
				Object: &sdkkonnectops.ListConsumerGroupResponseBody{
					Data: []sdkkonnectcomp.ConsumerGroup{
						{
							ID: lo.ToPtr(cgID),
						},
					},
				},
			},
			nil,
		)

		t.Log("Creating KongConsumerGroup")
		cg := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		watchFor(t, ctx, cWatch, watch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != cg.GetName() && c.GetKonnectID() != cgID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		t.Log("Waiting for KongConsumerGroup to be created in the SDK")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ConsumerGroupSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
