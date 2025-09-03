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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongKeySet(t *testing.T) {
	const (
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongKeySet](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongKeySet](&metricsmocks.MockRecorder{}),
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

	t.Run("create, patch, delete", func(t *testing.T) {
		t.Log("Setting up SDK expectations on KongKeySet creation")
		sdk.KeySetsSDK.EXPECT().CreateKeySet(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input *sdkkonnectcomp.KeySet) bool {
				return input.Name != nil && *input.Name == keySetName
			}),
		).Return(&sdkkonnectops.CreateKeySetResponse{
			KeySet: &sdkkonnectcomp.KeySet{
				ID: lo.ToPtr(keySetID),
			},
		}, nil)

		w := setupWatch[configurationv1alpha1.KongKeySetList](t, ctx, cl, client.InNamespace(ns.Name))
		createdKeySet := deploy.KongKeySet(t, ctx, clientNamespaced, keySetName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KongKeySet to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdKeySet),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongKeySet](),
			),
			"KongKeySet's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongKeySet to be created in the SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.KeySetsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKeySet update")
		sdk.KeySetsSDK.EXPECT().UpsertKeySet(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertKeySetRequest) bool {
			return r.KeySetID == keySetID &&
				lo.Contains(r.KeySet.Tags, "addedTag")
		})).Return(&sdkkonnectops.UpsertKeySetResponse{}, nil)

		t.Log("Patching KongKeySet")
		certToPatch := createdKeySet.DeepCopy()
		certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
		require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdKeySet)))

		t.Log("Waiting for KongKeySet to be updated")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdKeySet),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongKeySet](),
			),
			"KongKeySet should be updated",
		)

		t.Log("Waiting for KongKeySet to be updated in the SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.KeySetsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongKeySet deletion")
		sdk.KeySetsSDK.EXPECT().DeleteKeySet(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), keySetID).
			Return(&sdkkonnectops.DeleteKeySetResponse{}, nil)

		t.Log("Deleting KongKeySet")
		require.NoError(t, cl.Delete(ctx, createdKeySet))

		t.Log("Waiting for KongKeySet to be deleted")
		require.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(createdKeySet), createdKeySet),
				))
			}, waitTime, tickTime,
		)

		eventuallyAssertSDKExpectations(t, factory.SDK.KeySetsSDK, waitTime, tickTime)
	})

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
			mock.MatchedBy(func(input *sdkkonnectcomp.KeySet) bool {
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

		w := setupWatch[configurationv1alpha1.KongKeySetList](t, ctx, cl, client.InNamespace(ns.Name))
		deploy.KongKeySet(t, ctx, clientNamespaced, keySetName,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		t.Log("Watching for KeySet to verify the created KeySet programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKeySet) bool {
			return c.GetKonnectID() == keySetID && k8sutils.IsProgrammed(c)
		}, "KeySet should be programmed and have ID in status after handling conflict")

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		t.Log("Setting up SDK expectations on KongKeySet creation")
		sdk.KeySetsSDK.EXPECT().CreateKeySet(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input *sdkkonnectcomp.KeySet) bool {
				return input.Name != nil && *input.Name == keySetName
			}),
		).Return(&sdkkonnectops.CreateKeySetResponse{
			KeySet: &sdkkonnectcomp.KeySet{
				ID: lo.ToPtr(keySetID),
			},
		}, nil)

		w := setupWatch[configurationv1alpha1.KongKeySetList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Creating KongKeySet with ControlPlaneRef type=konnectID")
		createdKeySet := deploy.KongKeySet(t, ctx, clientNamespaced, keySetName,
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongKeySet to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongKeySet) bool {
			if c.GetName() != createdKeySet.GetName() {
				return false
			}
			if c.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongKeySet's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.KeySetsSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongKeySetList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongKeySet creation")
		sdk.KeySetsSDK.EXPECT().
			CreateKeySet(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req *sdkkonnectcomp.KeySet) bool {
					return slices.Contains(req.Tags, "test-1")
				}),
			).
			Return(
				&sdkkonnectops.CreateKeySetResponse{
					KeySet: &sdkkonnectcomp.KeySet{
						ID:   lo.ToPtr(id),
						Tags: []string{"test-1"},
					},
				},
				nil,
			)

		created := deploy.KongKeySet(t, ctx, clientNamespaced, "keyset-1",
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1alpha1.KongKeySet)
				cg.Spec.Tags = append(cg.Spec.Tags, "test-1")
			},
		)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("Key didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		eventuallyAssertSDKExpectations(t, factory.SDK.KeysSDK, waitTime, tickTime)

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for KongKeySet to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongKeySet didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})
}
