package envtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
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
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongKey](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongKey](&metricsmocks.MockRecorder{}),
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
		mock.MatchedBy(func(input sdkkonnectcomp.Key) bool {
			return input.Kid == keyKid &&
				input.Name != nil && *input.Name == keyName
		}),
	).Return(&sdkkonnectops.CreateKeyResponse{
		Key: &sdkkonnectcomp.Key{
			ID: lo.ToPtr(keyID),
		},
	}, nil)

	w := setupWatch[configurationv1alpha1.KongKeyList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("without KongKeySet", func(t *testing.T) {
		t.Log("Creating KongKey")
		createdKey := deploy.KongKey(t, ctx, clientNamespaced, keyKid, keyName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KongKey to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKey's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKey update")
		sdk.KeysSDK.EXPECT().UpsertKey(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeyRequest) bool {
			return r.KeyID == keyID &&
				lo.Contains(r.Key.Tags, "addedTag")
		})).Return(&sdkkonnectops.UpsertKeyResponse{}, nil)

		t.Log("Patching KongKey")
		certToPatch := createdKey.DeepCopy()
		certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
		require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdKey)))

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKey deletion")
		sdk.KeysSDK.EXPECT().DeleteKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), keyID).
			Return(&sdkkonnectops.DeleteKeyResponse{}, nil)

		t.Log("Deleting KongKey")
		require.NoError(t, cl.Delete(ctx, createdKey))

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)
	})

	t.Run("without KongKeySet but with conflict response", func(t *testing.T) {
		const (
			keyID   = "key-conflict-id"
			keyKid  = "key-conflict-kid"
			keyName = "key-conflict-name"
		)
		t.Log("Setting up SDK expectations on KongKey creation with conflict")
		sdk.KeysSDK.EXPECT().CreateKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input sdkkonnectcomp.Key) bool {
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
		createdKey := deploy.KongKey(t, ctx, clientNamespaced, keyKid, keyName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KongKey to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(k *configurationv1alpha1.KongKey) bool {
			if k.GetName() != createdKey.GetName() {
				return false
			}
			return lo.ContainsBy(k.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKey's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)
	})

	t.Run("with KongKeySet", func(t *testing.T) {
		t.Log("Creating KongKey")
		createdKey := deploy.KongKey(t, ctx, clientNamespaced, keyKid, keyName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				key := obj.(*configurationv1alpha1.KongKey)
				key.Spec.KeySetRef = &configurationv1alpha1.KeySetRef{
					Type: configurationv1alpha1.KeySetRefNamespacedRef,
					NamespacedRef: lo.ToPtr(commonv1alpha1.NameRef{
						Name: keySetName,
					}),
				}
			},
		)

		t.Log("Waiting for KeySetRefValid condition to be false")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKey) bool {
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
			mock.MatchedBy(func(input sdkkonnectcomp.Key) bool {
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
		keySet := deploy.KongKeySet(t, ctx, clientNamespaced, keySetName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongKeySetStatusWithProgrammed(t, ctx, clientNamespaced, keySet, keySetID, cp.GetKonnectStatus().GetKonnectID())

		t.Log("Waiting for KongKey to be programmed and associated with KongKeySet")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKey) bool {
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
			exactlyZeroOwnerReference := len(c.GetOwnerReferences()) == 0

			return programmed && associated && keySetIDPopulated && exactlyZeroOwnerReference
		}, "KongKey's Programmed and KeySetRefValid conditions should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)

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
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}

			if c.Spec.KeySetRef != nil {
				return false
			}

			return len(c.GetOwnerReferences()) == 0
		}, "KongKey should be deattached from KongKeySet eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		t.Log("Setting up SDK expectations on KongKey creation")
		sdk.KeysSDK.EXPECT().CreateKey(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input sdkkonnectcomp.Key) bool {
				return input.Kid == keyKid &&
					input.Name != nil && *input.Name == keyName
			}),
		).Return(&sdkkonnectops.CreateKeyResponse{
			Key: &sdkkonnectcomp.Key{
				ID: lo.ToPtr(keyID),
			},
		}, nil)

		t.Log("Creating KongKey with ControlPlaneRef type=konnectID")
		createdKey := deploy.KongKey(t, ctx, clientNamespaced, keyKid, keyName,
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongKey to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKey) bool {
			if c.GetName() != createdKey.GetName() {
				return false
			}
			if c.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKey's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongKeyList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongKey creation")
		sdk.KeysSDK.EXPECT().
			CreateKey(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Key) bool {
					return slices.Contains(req.Tags, "test-1")
				}),
			).
			Return(
				&sdkkonnectops.CreateKeyResponse{
					Key: &sdkkonnectcomp.Key{
						ID:   lo.ToPtr(id),
						Tags: []string{"test-1"},
					},
				},
				nil,
			)

		created := deploy.KongKey(t, ctx, clientNamespaced, "key-kid", "key-name",
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1alpha1.KongKey)
				cg.Spec.Tags = append(cg.Spec.Tags, "test-1")
			},
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("Key didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for KongKey to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongKey didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})
}
