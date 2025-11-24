package envtest

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongConsumer(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1.KongConsumer](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1.KongConsumer](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1beta1.KongConsumerGroup](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1beta1.KongConsumerGroup](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKongCredentialSecretReconciler(logging.DevelopmentMode, mgr.GetClient(), mgr.GetScheme()),
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
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

	wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("should create, update and delete Consumer without ConsumerGroups successfully", func(t *testing.T) {
		const (
			consumerID      = "consumer-id"
			username        = "user-1"
			updatedUsername = "user-1-updated"
		)
		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		t.Log("Creating KongConsumer")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KongConsumer to be programmed")
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongConsumer update")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				match := r.ConsumerID == consumerID &&
					r.Consumer.Username != nil && *r.Consumer.Username == updatedUsername
				return match
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil)

		t.Log("Patching KongConsumer")
		consumerToPatch := createdConsumer.DeepCopy()
		consumerToPatch.Username = updatedUsername
		require.NoError(t, clientNamespaced.Patch(ctx, consumerToPatch, client.MergeFrom(createdConsumer)))

		t.Log("Waiting for KongConsumer to be patched")
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectMatchesKonnectID[*configurationv1.KongConsumer](consumerID),
				func(c *configurationv1.KongConsumer) bool {
					return c.Username == updatedUsername
				},
			),
			"KongConsumer should get patched with new username",
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongConsumer deletion")
		sdk.ConsumersSDK.EXPECT().
			DeleteConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), consumerID).
			Return(&sdkkonnectops.DeleteConsumerResponse{}, nil)

		t.Log("Deleting KongConsumer")
		require.NoError(t, cl.Delete(ctx, createdConsumer))

		require.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(createdConsumer), createdConsumer),
				))
			}, waitTime, tickTime,
		)
	})

	cgWatch := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("should create, update and delete Consumer with ConsumerGroups successfully", func(t *testing.T) {
		const (
			consumerID = "consumer-id"
			username   = "user-2"

			cgID              = "consumer-group-id"
			consumerGroupName = "consumer-group-1"
		)
		t.Log("Setting up SDK expectations on KongConsumer creation with ConsumerGroup")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer (due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.ConsumerGroup) bool {
					return input.Name == consumerGroupName
				}),
			).Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr(cgID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		emptyListCall := sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{
			// Returning no ConsumerGroups associated with the Consumer in Konnect to trigger addition.
		}, nil)

		t.Log("Setting up SDK expectation on adding Consumer to ConsumerGroup")
		sdk.ConsumerGroupSDK.EXPECT().
			AddConsumerToGroup(mock.Anything, sdkkonnectops.AddConsumerToGroupRequest{
				ConsumerGroupID: cgID,
				ControlPlaneID:  cp.GetKonnectStatus().GetKonnectID(),
				RequestBody: &sdkkonnectops.AddConsumerToGroupRequestBody{
					ConsumerID: lo.ToPtr(consumerID),
				},
			}).Return(&sdkkonnectops.AddConsumerToGroupResponse{}, nil)

		t.Log("Creating KongConsumerGroup")
		createdConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = consumerGroupName
			},
		)

		t.Log("Creating KongConsumer and patching it with ConsumerGroup")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		consumer := createdConsumer.DeepCopy()
		consumer.ConsumerGroups = []string{createdConsumerGroup.GetName()}
		require.NoError(t, clientNamespaced.Patch(ctx, consumer, client.MergeFrom(createdConsumer)))

		t.Log("Waiting for KongConsumer to be programmed")
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		watchFor(t, ctx, cgWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != createdConsumerGroup.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on KongConsumer update with ConsumerGroup")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID &&
					r.Consumer.Username != nil && *r.Consumer.Username == "user-2-updated"
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil)

		emptyListCall.Unset() // Unset the previous expectation to allow the new one to be set.
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{
			Object: &sdkkonnectops.ListConsumerGroupsForConsumerResponseBody{
				Data: []sdkkonnectcomp.ConsumerGroup{
					{
						// Returning an ID that we haven't defined to be associated with the Consumer.
						// Should trigger removal.
						ID: lo.ToPtr("not-defined-in-crd"),
					},
					{
						// Returning the ID of the ConsumerGroup we have defined to be associated with the Consumer.
						// Should not trigger any action as it's already associated.
						ID: lo.ToPtr(cgID),
					},
				},
			},
		}, nil)

		sdk.ConsumerGroupSDK.EXPECT().
			RemoveConsumerFromGroup(mock.Anything, sdkkonnectops.RemoveConsumerFromGroupRequest{
				ConsumerGroupID: "not-defined-in-crd",
				ControlPlaneID:  cp.GetKonnectStatus().GetKonnectID(),
				ConsumerID:      consumerID,
			}).Return(&sdkkonnectops.RemoveConsumerFromGroupResponse{}, nil)

		t.Log("Patching KongConsumer to trigger reconciliation")
		consumerToPatch := createdConsumer.DeepCopy()
		consumerToPatch.Username = "user-2-updated"
		require.NoError(t, clientNamespaced.Patch(ctx, consumerToPatch, client.MergeFrom(createdConsumer)))
	})

	t.Run("should handle conflict in creation correctly", func(t *testing.T) {
		const (
			consumerID = "consumer-id-conflict"
			username   = "user-3"
		)
		t.Log("Setup mock SDK for creating consumer and listing consumers by UID")
		cpID := cp.GetKonnectStatus().GetKonnectID()
		sdk.ConsumersSDK.EXPECT().CreateConsumer(
			mock.Anything,
			cpID,
			mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
				return input.Username != nil && *input.Username == username
			}),
		).Return(&sdkkonnectops.CreateConsumerResponse{}, &sdkkonnecterrs.ConflictError{})

		sdk.ConsumersSDK.EXPECT().ListConsumer(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.ListConsumerRequest) bool {
				return req.ControlPlaneID == cpID &&
					req.Tags != nil && strings.HasPrefix(*req.Tags, "k8s-uid")
			}),
		).Return(&sdkkonnectops.ListConsumerResponse{
			Object: &sdkkonnectops.ListConsumerResponseBody{
				Data: []sdkkonnectcomp.Consumer{
					{
						ID: lo.ToPtr(consumerID),
					},
				},
			},
		}, nil)

		t.Log("Creating a KongConsumer")
		deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Watching for KongConsumers to verify the created KongConsumer programmed")
		watchFor(t, ctx, wConsumer, apiwatch.Modified, func(c *configurationv1.KongConsumer) bool {
			return c.GetKonnectID() == consumerID && k8sutils.IsProgrammed(c)
		}, "KongConsumer should be programmed and have ID in status after handling conflict")
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		const (
			consumerID = "consumer-with-cp-konnect-id"
			username   = "user-with-cp-konnect-id"
		)
		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		t.Log("Creating KongConsumer with ControlPlaneRef type=konnectID")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongConsumer to be programmed")
		watchFor(t, ctx, wConsumer, apiwatch.Modified, func(c *configurationv1.KongConsumer) bool {
			if c.GetName() != createdConsumer.GetName() {
				return false
			}
			if c.GetControlPlaneRef().Type != commonv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumer's Programmed condition should be true eventually")
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id   = "abc-12345"
			name = "name-1"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Consumer) bool {
					return req.Username != nil && *req.Username == name
				}),
			).
			Return(
				&sdkkonnectops.CreateConsumerResponse{
					Consumer: &sdkkonnectcomp.Consumer{
						ID:       lo.ToPtr(id),
						Username: lo.ToPtr(name),
					},
				},
				nil,
			)

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     id,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		created := deploy.KongConsumer(t, ctx, clientNamespaced, name,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				c := obj.(*configurationv1.KongConsumer)
				c.Username = name
			},
		)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("Consumer didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for KongConsumer to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongConsumer didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})

	t.Run("detaching and reattaching the referenced CP correctly removes and readds the konnect cleanup finalizer", func(t *testing.T) {
		const (
			id   = "abc-1234567"
			name = "name-2"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Consumer) bool {
					return req.Username != nil && *req.Username == name
				}),
			).
			Return(
				&sdkkonnectops.CreateConsumerResponse{
					Consumer: &sdkkonnectcomp.Consumer{
						ID:       lo.ToPtr(id),
						Username: lo.ToPtr(name),
					},
				},
				nil,
			)

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     id,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		created := deploy.KongConsumer(t, ctx, clientNamespaced, name,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				c := obj.(*configurationv1.KongConsumer)
				c.Username = name
			},
		)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("Consumer didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for object to be get Programmed and ControlPlaneRefValid conditions with status=False and konnect cleanup finalizer removed")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				assertNot(objectHasFinalizer[*configurationv1.KongConsumer](konnect.KonnectCleanupFinalizer)),
				conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			),
			"Object didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)

		id2 := uuid.New().String()
		t.Log("Setting up SDK expectations on KongConsumer update (after KonnectGatewayControlPlane deletion)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == id
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{
				Consumer: &sdkkonnectcomp.Consumer{
					ID: lo.ToPtr(id2),
				},
			}, nil)

		cp = deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth,
			func(obj client.Object) {
				cpNew := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
				cpNew.Name = cp.Name
			},
		)
		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     id,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		t.Log("Waiting for object to be get Programmed with status=True and konnect cleanup finalizer re added")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
				objectHasFinalizer[*configurationv1.KongConsumer](konnect.KonnectCleanupFinalizer),
			),
			"Object didn't get Programmed set to True",
		)
	})
}

func TestKongConsumerSecretCredentials(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1.KongConsumer](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1.KongConsumer](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialBasicAuth](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialBasicAuth](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialAPIKey](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialAPIKey](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialACL](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialACL](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialJWT](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialJWT](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialHMAC](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialHMAC](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKongCredentialSecretReconciler(logging.DevelopmentMode, mgr.GetClient(), mgr.GetScheme()),
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

	t.Run("BasicAuth", func(t *testing.T) {
		consumerID := fmt.Sprintf("consumer-%d", rand.Int31n(1000))
		username := fmt.Sprintf("user-secret-credentials-%d", rand.Int31n(1000))
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		s := deploy.Secret(t, ctx, clientNamespaced,
			map[string][]byte{
				"username": []byte(username),
				"password": []byte("password"),
			},
			deploy.WithLabel("konghq.com/credential", konnect.KongCredentialTypeBasicAuth),
		)

		t.Log("Setting up SDK expectation on (managed) BasicAuth credentials creation")
		sdk.KongCredentialsBasicAuthSDK.EXPECT().
			CreateBasicAuthWithConsumer(
				mock.Anything,
				mock.MatchedBy(
					func(r sdkkonnectops.CreateBasicAuthWithConsumerRequest) bool {
						return r.ControlPlaneID == cp.GetKonnectID() &&
							r.BasicAuthWithoutParents.Username == username &&
							r.BasicAuthWithoutParents.Password == "password"
					},
				),
			).
			Return(
				&sdkkonnectops.CreateBasicAuthWithConsumerResponse{
					BasicAuth: &sdkkonnectcomp.BasicAuth{
						ID: lo.ToPtr("basic-auth-id"),
					},
				},
				nil,
			)

		t.Log("Creating KongConsumerf")
		wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))
		wBasicAuth := setupWatch[configurationv1alpha1.KongCredentialBasicAuthList](t, ctx, cl, client.InNamespace(ns.Name))
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				consumer := obj.(*configurationv1.KongConsumer)
				consumer.Credentials = []string{
					s.Name,
				}
			},
		)

		t.Log("Waiting for KongConsumer to be programmed")
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongCredentialBasicAuth to be programmed")
		watchFor(t, ctx, wBasicAuth, apiwatch.Modified,
			objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongCredentialBasicAuth](),
			"BasicAuth credential should get the Programmed condition",
		)
	})

	t.Run("APIKey", func(t *testing.T) {
		consumerID := fmt.Sprintf("consumer-%d", rand.Int31n(1000))
		username := fmt.Sprintf("user-secret-credentials-%d", rand.Int31n(1000))
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		s := deploy.Secret(t, ctx, clientNamespaced,
			map[string][]byte{
				"key": []byte("api-key"),
			},
			deploy.WithLabel("konghq.com/credential", konnect.KongCredentialTypeAPIKey),
		)

		t.Log("Setting up SDK expectation on (managed) APIKey credentials creation")
		sdk.KongCredentialsAPIKeySDK.EXPECT().
			CreateKeyAuthWithConsumer(
				mock.Anything,
				mock.MatchedBy(
					func(r sdkkonnectops.CreateKeyAuthWithConsumerRequest) bool {
						return r.ControlPlaneID == cp.GetKonnectID() &&
							r.KeyAuthWithoutParents.Key != nil && *r.KeyAuthWithoutParents.Key == "api-key"
					},
				),
			).
			Return(
				&sdkkonnectops.CreateKeyAuthWithConsumerResponse{
					KeyAuth: &sdkkonnectcomp.KeyAuth{
						ID: lo.ToPtr("key-auth-id"),
					},
				},
				nil,
			)

		t.Log("Creating KongConsumer")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				consumer := obj.(*configurationv1.KongConsumer)
				consumer.Credentials = []string{
					s.Name,
				}
			},
		)

		t.Log("Waiting for KongConsumer to be programmed")
		wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))
		wKeyCredential := setupWatch[configurationv1alpha1.KongCredentialAPIKeyList](t, ctx, cl, client.InNamespace(ns.Name))
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongCredentialAPIKey to be programmed")
		watchFor(t, ctx, wKeyCredential, apiwatch.Modified,
			objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongCredentialAPIKey](),
			"APIKey credential should get the Programmed condition",
		)
	})

	t.Run("ACL", func(t *testing.T) {
		consumerID := fmt.Sprintf("consumer-%d", rand.Int31n(1000))
		username := fmt.Sprintf("user-secret-credentials-%d", rand.Int31n(1000))
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		s := deploy.Secret(t, ctx, clientNamespaced,
			map[string][]byte{
				"group": []byte("acl-group"),
			},
			deploy.WithLabel("konghq.com/credential", konnect.KongCredentialTypeACL),
		)

		t.Log("Setting up SDK expectation on (managed) ACLs credentials creation")
		sdk.KongCredentialsACLSDK.EXPECT().
			CreateACLWithConsumer(
				mock.Anything,
				mock.MatchedBy(
					func(r sdkkonnectops.CreateACLWithConsumerRequest) bool {
						return r.ControlPlaneID == cp.GetKonnectID() &&
							r.ACLWithoutParents.Group == "acl-group"
					},
				),
			).
			Return(
				&sdkkonnectops.CreateACLWithConsumerResponse{
					ACL: &sdkkonnectcomp.ACL{
						ID: lo.ToPtr("acl-id"),
					},
				},
				nil,
			)

		t.Log("Creating KongConsumer")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				consumer := obj.(*configurationv1.KongConsumer)
				consumer.Credentials = []string{
					s.Name,
				}
			},
		)
		var l configurationv1alpha1.KongCredentialACLList
		l.GetItems()

		t.Log("Waiting for KongConsumer to be programmed")
		wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))
		wACL := setupWatch[configurationv1alpha1.KongCredentialACLList](t, ctx, cl, client.InNamespace(ns.Name))

		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongCredentialACL to be programmed")
		watchFor(t, ctx, wACL, apiwatch.Modified,
			objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongCredentialACL](),
			"ACL credential should get the Programmed condition",
		)
	})

	t.Run("JWT", func(t *testing.T) {
		consumerID := fmt.Sprintf("consumer-%d", rand.Int31n(1000))
		username := fmt.Sprintf("user-secret-credentials-%d", rand.Int31n(1000))
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer (due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		s := deploy.Secret(t, ctx, clientNamespaced,
			map[string][]byte{
				"algorithm":      []byte("RS256"),
				"key":            []byte("jwt-key"),
				"rsa_public_key": []byte("rsa-public-key"),
			},
			deploy.WithLabel("konghq.com/credential", konnect.KongCredentialTypeJWT),
		)

		t.Log("Setting up SDK expectation on (managed) JWTs credentials creation")
		sdk.KongCredentialsJWTSDK.EXPECT().
			CreateJwtWithConsumer(
				mock.Anything,
				mock.MatchedBy(
					func(r sdkkonnectops.CreateJwtWithConsumerRequest) bool {
						return r.ControlPlaneID == cp.GetKonnectID() &&
							*r.JWTWithoutParents.Algorithm == "RS256" &&
							*r.JWTWithoutParents.Key == "jwt-key" &&
							*r.JWTWithoutParents.RsaPublicKey == "rsa-public-key"
					},
				),
			).
			Return(
				&sdkkonnectops.CreateJwtWithConsumerResponse{
					Jwt: &sdkkonnectcomp.Jwt{
						ID: lo.ToPtr("jwt-id"),
					},
				},
				nil,
			)
		t.Log("Creating KongConsumer")
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				consumer := obj.(*configurationv1.KongConsumer)
				consumer.Credentials = []string{
					s.Name,
				}
			},
		)

		t.Log("Waiting for KongConsumer to be programmed")
		wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))
		wJWT := setupWatch[configurationv1alpha1.KongCredentialJWTList](t, ctx, cl, client.InNamespace(ns.Name))
		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongCredentialJWT to be programmed")
		watchFor(t, ctx, wJWT, apiwatch.Modified,
			objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongCredentialJWT](),
			"JWT credential should get the Programmed condition",
		)
	})

	t.Run("HMAC", func(t *testing.T) {
		consumerID := fmt.Sprintf("consumer-%d", rand.Int31n(1000))
		username := fmt.Sprintf("user-secret-credentials-%d", rand.Int31n(1000))
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

		t.Log("Setting up SDK expectations on KongConsumer creation")
		sdk.ConsumersSDK.EXPECT().
			CreateConsumer(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(input sdkkonnectcomp.Consumer) bool {
					return input.Username != nil && *input.Username == username
				}),
			).Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr(consumerID),
			},
		}, nil)

		t.Log("Setting up SDK expectation on possibly updating KongConsumer ( due to asynchronous nature of updates between KongConsumer and KongConsumerGroup)")
		sdk.ConsumersSDK.EXPECT().
			UpsertConsumer(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertConsumerRequest) bool {
				return r.ConsumerID == consumerID
			})).
			Return(&sdkkonnectops.UpsertConsumerResponse{}, nil).
			Maybe()

		t.Log("Setting up SDK expectation on KongConsumerGroups listing")
		sdk.ConsumersSDK.EXPECT().
			ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
				ConsumerID:     consumerID,
				ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
			}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{}, nil)

		s := deploy.Secret(t, ctx, clientNamespaced,
			map[string][]byte{
				"username": []byte(username),
				"secret":   []byte("hmac-secret"),
			},
			deploy.WithLabel("konghq.com/credential", konnect.KongCredentialTypeHMAC),
		)

		t.Log("Setting up SDK expectation on (managed) HMAC credentials creation")
		sdk.KongCredentialsHMACSDK.EXPECT().
			CreateHmacAuthWithConsumer(
				mock.Anything,
				mock.MatchedBy(
					func(r sdkkonnectops.CreateHmacAuthWithConsumerRequest) bool {
						return r.ControlPlaneID == cp.GetKonnectID() &&
							r.HMACAuthWithoutParents.Username == username &&
							*r.HMACAuthWithoutParents.Secret == "hmac-secret"
					},
				),
			).
			Return(
				&sdkkonnectops.CreateHmacAuthWithConsumerResponse{
					HMACAuth: &sdkkonnectcomp.HMACAuth{
						ID: lo.ToPtr("hmac-auth-id"),
					},
				},
				nil,
			)
		t.Log("Creating KongConsumer")
		wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl, client.InNamespace(ns.Name))
		wHMAC := setupWatch[configurationv1alpha1.KongCredentialHMACList](t, ctx, cl, client.InNamespace(ns.Name))
		createdConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				consumer := obj.(*configurationv1.KongConsumer)
				consumer.Credentials = []string{
					s.Name,
				}
			},
		)
		t.Log("Waiting for KongConsumer to be programmed")

		watchFor(t, ctx, wConsumer, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdConsumer),
				objectHasConditionProgrammedSetToTrue[*configurationv1.KongConsumer](),
			),
			"KongConsumer's Programmed condition should be true eventually",
		)

		t.Log("Waiting for KongCredentialHMAC to be programmed")
		watchFor(t, ctx, wHMAC, apiwatch.Modified,
			objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongCredentialHMAC](),
			"HMAC credential should get the Programmed condition",
		)
	})
}
