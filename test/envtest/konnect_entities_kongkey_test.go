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
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongKey(t *testing.T) {
	const (
		keyKid  = "key-kid"
		keyName = "key-name"
		keyID   = "key-id"

		keySetName = "key-set-name"
		keySetID   = "key-set-id"
	)
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongKey](konnectInfiniteSyncTime),
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

	t.Log("Setting up SDK expectations on KongKey creation")
	sdk.KeysSDK.EXPECT().CreateKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input sdkkonnectcomp.KeyInput) bool {
			return input.Kid == keyKid &&
				input.Name != nil && *input.Name == keyName
		}),
	).Return(&sdkkonnectops.CreateKeyResponse{
		Key: &sdkkonnectcomp.Key{
			ID: lo.ToPtr(keyID),
		},
	}, nil)

	t.Log("Setting up a watch for KongKey events")
	w := setupWatch[configurationv1alpha1.KongKeyList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("without KongKeySet", func(t *testing.T) {
		t.Log("Creating KongKey")
		createdKey := deploy.KongKeyAttachedToCP(t, ctx, clientNamespaced, keyKid, keyName, cp)

		t.Log("Waiting for KongKey to be programmed")
		watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKey's Programmed condition should be true eventually")

		t.Log("Checking SDK KongKey operations")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKey update")
		sdk.KeysSDK.EXPECT().UpsertKey(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeyRequest) bool {
			return r.KeyID == keyID &&
				lo.Contains(r.Key.Tags, "addedTag")
		})).Return(&sdkkonnectops.UpsertKeyResponse{}, nil)

		t.Log("Patching KongKey")
		certToPatch := createdKey.DeepCopy()
		certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
		require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdKey)))

		t.Log("Waiting for KongKey to be updated in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKey deletion")
		sdk.KeysSDK.EXPECT().DeleteKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), keyID).
			Return(&sdkkonnectops.DeleteKeyResponse{}, nil)

		t.Log("Deleting KongKey")
		require.NoError(t, cl.Delete(ctx, createdKey))

		t.Log("Waiting for KongKey to be deleted in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})

	t.Run("without KongKeySet but with conflict response", func(t *testing.T) {
		const (
			keyID   = "key-conflict-id"
			keyKid  = "key-conflict-kid"
			keyName = "key-conflict-name"
		)
		t.Log("Setting up SDK expectations on KongKey creation with conflict")
		sdk.KeysSDK.EXPECT().CreateKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input sdkkonnectcomp.KeyInput) bool {
				return input.Kid == keyKid &&
					input.Name != nil && *input.Name == keyName
			}),
		).Return(&sdkkonnectops.CreateKeyResponse{
			Key: &sdkkonnectcomp.Key{
				ID: lo.ToPtr(keyID),
			},
		}, &sdkkonnecterrs.ConflictError{})

		sdk.KeysSDK.EXPECT().ListKey(
			mock.Anything,
			mock.MatchedBy(func(r sdkkonnectops.ListKeyRequest) bool {
				return r.ControlPlaneID == cp.GetKonnectID() &&
					r.Tags != nil && strings.HasPrefix(*r.Tags, "k8s-uid")
			}),
		).Return(&sdkkonnectops.ListKeyResponse{
			Object: &sdkkonnectops.ListKeyResponseBody{
				Data: []sdkkonnectcomp.Key{
					{
						ID: lo.ToPtr(keyID),
					},
				},
			},
		}, nil)

		t.Log("Creating KongKey")
		createdKey := deploy.KongKeyAttachedToCP(t, ctx, clientNamespaced, keyKid, keyName, cp)

		t.Log("Waiting for KongKey to be programmed")
		watchFor(t, ctx, w, watch.Modified, func(k *configurationv1alpha1.KongKey) bool {
			if k.GetName() != createdKey.GetName() {
				return false
			}
			return lo.ContainsBy(k.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKey's Programmed condition should be true eventually")

		t.Log("Checking SDK KongKey operations")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})

	t.Run("with KongKeySet", func(t *testing.T) {
		t.Log("Creating KongKey")
		withKeySetRef := func(key *configurationv1alpha1.KongKey) {
			key.Spec.KeySetRef = &configurationv1alpha1.KeySetRef{
				Type: configurationv1alpha1.KeySetRefNamespacedRef,
				NamespacedRef: lo.ToPtr(configurationv1alpha1.KeySetNamespacedRef{
					Name: keySetName,
				}),
			}
		}
		createdKey := deploy.KongKeyAttachedToCP(t, ctx, clientNamespaced, keyKid, keyName, cp, withKeySetRef)

		t.Log("Waiting for KeySetRefValid condition to be false")
		watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KeySetRefValidConditionType &&
					condition.Status == metav1.ConditionFalse
			})
		}, "KongKey's KeySetRefValid condition should be false eventually as the KongKeySet is not created yet")

		t.Log("Setting up SDK expectations on KongKey creation with KeySetRef")
		sdk.KeysSDK.EXPECT().CreateKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input sdkkonnectcomp.KeyInput) bool {
				return input.Kid == keyKid &&
					input.Name != nil && *input.Name == keyName &&
					input.Set != nil && input.Set.GetID() != nil && *input.Set.GetID() == keySetID
			}),
		).Return(&sdkkonnectops.CreateKeyResponse{
			Key: &sdkkonnectcomp.Key{
				ID: lo.ToPtr(keyID),
			},
		}, nil)

		t.Log("Creating KongKeySet")
		keySet := deploy.KongKeySetAttachedToCP(t, ctx, clientNamespaced, keySetName, cp)
		updateKongKeySetStatusWithProgrammed(t, ctx, clientNamespaced, keySet, keySetID, cp.GetKonnectStatus().GetKonnectID())

		t.Log("Waiting for KongKey to be programmed and associated with KongKeySet")
		watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			programmed := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
			associated := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KeySetRefValidConditionType &&
					condition.Status == metav1.ConditionTrue
			})
			keySetIDPopulated := c.Status.Konnect != nil && c.Status.Konnect.KeySetID != ""
			exactlyOneOwnerReference := len(c.GetOwnerReferences()) == 1
			hasOwnerRefToKeySet := c.GetOwnerReferences()[0].Name == keySet.GetName()

			return programmed && associated && keySetIDPopulated && exactlyOneOwnerReference && hasOwnerRefToKeySet
		}, "KongKey's Programmed and KeySetRefValid conditions should be true eventually")

		t.Log("Waiting for KongKey to be created in the SDK")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKeySet deattachment")
		sdk.KeysSDK.EXPECT().UpsertKey(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeyRequest) bool {
			return r.KeyID == keyID &&
				r.Key.Set == nil
		})).Return(&sdkkonnectops.UpsertKeyResponse{}, nil)

		t.Log("Patching KongKey to deattach from KongKeySet")
		keyToPatch := createdKey.DeepCopy()
		keyToPatch.Spec.KeySetRef = nil
		require.NoError(t, clientNamespaced.Patch(ctx, keyToPatch, client.MergeFrom(createdKey)))

		t.Log("Waiting for KongKey to be deattached from KongKeySet")
		watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			exactlyOneOwnerReference := len(c.GetOwnerReferences()) == 1
			hasOwnerReferenceToCP := c.GetOwnerReferences()[0].Name == cp.GetName()

			return exactlyOneOwnerReference && hasOwnerReferenceToCP
		}, "KongKey should be deattached from KongKeySet eventually")

		t.Log("Waiting for KongKey to be deattached from KongKeySet in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
