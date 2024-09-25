package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongKey(t *testing.T) {
	const (
		keyKid  = "key-kid"
		keyName = "key-name"
		keyID   = "key-id"
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
	apiAuth := deployKonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deployKonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

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

	t.Log("Creating KongKey")
	createdKey := deployKongKeyAttachedToCP(t, ctx, clientNamespaced, keyKid, keyName, cp)

	t.Log("Waiting for KongKey to be programmed")
	watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongKey) bool {
		if c.GetName() != createdKey.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
				condition.Status == metav1.ConditionTrue
		})
	}, "KongKey's Programmed condition should be true eventually")

	t.Log("Waiting for KongKey to be created in the SDK")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongKey update")
	sdk.KeysSDK.EXPECT().UpsertKey(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeyRequest) bool {
		return r.KeyID == "12345" &&
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
	sdk.KeysSDK.EXPECT().DeleteKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), "12345").
		Return(&sdkkonnectops.DeleteKeyResponse{}, nil)

	t.Log("Deleting KongKey")
	require.NoError(t, cl.Delete(ctx, createdKey))

	t.Log("Waiting for KongKey to be deleted in the SDK")
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KeysSDK.AssertExpectations(t))
	}, waitTime, tickTime)
}
