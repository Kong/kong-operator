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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongKeySet(t *testing.T) {
	const (
		keySetName = "key-set-name"
		keySetID   = "key-set-id"
	)

	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := ops.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongKeySet](konnectInfiniteSyncTime),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up SDK expectations on KongKeySet creation")
	sdk.KeySetsSDK.EXPECT().CreateKeySet(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input sdkkonnectcomp.KeySetInput) bool {
			return input.Name != nil && *input.Name == keySetName
		}),
	).Return(&sdkkonnectops.CreateKeySetResponse{
		KeySet: &sdkkonnectcomp.KeySet{
			ID: lo.ToPtr(keySetID),
		},
	}, nil)

	t.Log("Setting up a watch for KongKeySet events")
	w := setupWatch[configurationv1alpha1.KongKeySetList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KongKeySet")
	createdKeySet := deploy.KongKeySetAttachedToCP(t, ctx, clientNamespaced, keySetName, cp)

	t.Log("Waiting for KongKeySet to be programmed")
	watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKeySet) bool {
		if c.GetName() != createdKeySet.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == metav1.ConditionTrue
		})
	}, "KongKeySet's Programmed condition should be true eventually")

	t.Log("Waiting for KongKeySet to be created in the SDK")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KeySetsSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongKeySet update")
	sdk.KeySetsSDK.EXPECT().UpsertKeySet(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeySetRequest) bool {
		return r.KeySetID == keySetID &&
			lo.Contains(r.KeySet.Tags, "addedTag")
	})).Return(&sdkkonnectops.UpsertKeySetResponse{}, nil)

	t.Log("Patching KongKeySet")
	certToPatch := createdKeySet.DeepCopy()
	certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
	require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdKeySet)))

	t.Log("Waiting for KongKeySet to be updated in the SDK")
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KeySetsSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongKeySet deletion")
	sdk.KeySetsSDK.EXPECT().DeleteKeySet(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), keySetID).
		Return(&sdkkonnectops.DeleteKeySetResponse{}, nil)

	t.Log("Deleting KongKeySet")
	require.NoError(t, cl.Delete(ctx, createdKeySet))

	t.Log("Waiting for KongKeySet to be deleted in the SDK")
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KeySetsSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Run("should handle conflict in creation correctly", func(t *testing.T) {
		const (
			keySetID   = "keyset-id-conflict"
			keySetName = "keyset-name-conflict"
		)
		t.Log("Setup mock SDK for creating KeySet and listing KeySets by UID")
		cpID := cp.GetKonnectStatus().GetKonnectID()
		sdk.KeySetsSDK.EXPECT().CreateKeySet(
			mock.Anything,
			cpID,
			mock.MatchedBy(func(input sdkkonnectcomp.KeySetInput) bool {
				return *input.Name == keySetName
			}),
		).Return(&sdkkonnectops.CreateKeySetResponse{}, &sdkkonnecterrs.ConflictError{})

		sdk.KeySetsSDK.EXPECT().ListKeySet(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.ListKeySetRequest) bool {
				return req.ControlPlaneID == cpID &&
					req.Tags != nil && strings.HasPrefix(*req.Tags, "k8s-uid")
			}),
		).Return(&sdkkonnectops.ListKeySetResponse{
			Object: &sdkkonnectops.ListKeySetResponseBody{
				Data: []sdkkonnectcomp.KeySet{
					{
						ID: lo.ToPtr(keySetID),
					},
				},
			},
		}, nil)

		t.Log("Creating a KeySet")
		createdKeySet := deploy.KongKeySetAttachedToCP(t, ctx, clientNamespaced, keySetName, cp)

		t.Log("Watching for KeySet to verify the created KeySet programmed")
		watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKeySet) bool {
			if c.GetName() != createdKeySet.GetName() {
				return false
			}
			return c.GetKonnectID() == keySetID && lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KeySet should be programmed and have ID in status after handling conflict")

		t.Log("Ensuring that the SDK's create and list methods are called")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ConsumersSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
