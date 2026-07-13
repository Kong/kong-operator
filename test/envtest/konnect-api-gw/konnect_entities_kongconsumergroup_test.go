package envtest

import (
	"fmt"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKongConsumerGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []envtest.Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1beta1.KongConsumerGroup](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1beta1.KongConsumerGroup](&metricsmocks.MockRecorder{}),
		),
	}
	envtest.StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	cWatch := envtest.SetupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("should create, update and delete ConsumerGroup successfully", func(t *testing.T) {
		const (
			cgID          = "consumer-id"
			cgName        = "consumer-group"
			updatedCGName = "consumer-group-updated"
		)
		t.Log("Setting up SDK expectations on KongConsumerGroup creation")
		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroup) bool {
					return cg.Name == cgName
				}),
			).Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: new(cgID),
			},
		}, nil,
		)

		t.Log("Creating KongConsumerGroup")
		cg := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		envtest.WatchFor(t, ctx, cWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != cg.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, consts.WaitTime, consts.TickTime)

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

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on KongConsumerGroup deletion")
		sdk.ConsumerGroupSDK.EXPECT().
			DeleteConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), cgID).
			Return(&sdkkonnectops.DeleteConsumerGroupResponse{}, nil)

		t.Log("Deleting KongConsumerGroup")
		require.NoError(t, cl.Delete(ctx, cg))

		require.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, apierrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(cg), cg),
				))
			}, consts.WaitTime, consts.TickTime,
		)

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, consts.WaitTime, consts.TickTime)
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
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroup) bool {
					return cg.Name == cgName
				}),
			).Return(
			nil,
			&sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       consts.ErrBodyDataConstraintError,
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
							ID: new(cgID),
						},
					},
				},
			},
			nil,
		)

		t.Log("Creating KongConsumerGroup")
		deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		envtest.WatchFor(t, ctx, cWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			return c.GetKonnectID() == cgID && k8sutils.IsProgrammed(c)
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id   = "abc-12345"
			name = "name-1"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := envtest.SetupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongConsumerGroup creation")
		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.ConsumerGroup) bool {
					return req.Name == name
				}),
			).
			Return(
				&sdkkonnectops.CreateConsumerGroupResponse{
					ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
						ID:   new(id),
						Name: name,
					},
				},
				nil,
			)

		created := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = name
			},
		)
		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, consts.WaitTime, consts.TickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, envtest.ConditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("ConsumerGroup didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for KongConsumerGroup to be get Programmed and ControlPlaneRefValid conditions with status=False")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.ConditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongConsumerGroup didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})

	t.Run("Adopting an existing consumer group", func(t *testing.T) {
		cgID := uuid.NewString()
		cgName := "adopted-consumer-group"

		w := envtest.SetupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating consumer groups")
		sdk.ConsumerGroupSDK.EXPECT().GetConsumerGroup(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.GetConsumerGroupRequest) bool {
				return req.ConsumerGroupID == cgID &&
					req.ControlPlaneID == cp.GetKonnectID() &&
					req.ListConsumers == nil
			}),
		).Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
					Name: cgName,
					ID:   new(cgID),
				},
			},
		}, nil)
		sdk.ConsumerGroupSDK.EXPECT().UpsertConsumerGroup(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertConsumerGroupRequest) bool {
				return req.ConsumerGroupID == cgID && req.ControlPlaneID == cp.GetKonnectID()
			}),
		).Return(&sdkkonnectops.UpsertConsumerGroupResponse{}, nil)

		t.Log("Creating a KongConsumerGroup to adopt the existing consumer group")
		createdConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectAdoptOptions[*configurationv1beta1.KongConsumerGroup](commonv1alpha1.AdoptModeOverride, cgID),
		)

		t.Logf("Waiting for KongConsumerGroup %s to be programmed and set Konnect ID", client.ObjectKeyFromObject(createdConsumerGroup))
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(cg *configurationv1beta1.KongConsumerGroup) bool {
			return cg.Name == createdConsumerGroup.Name &&
				k8sutils.IsProgrammed(cg) &&
				cg.GetKonnectID() == cgID
		},
			fmt.Sprintf("KongConsumerGroup didn't get Programmed status condition or didn't get the correct Konnect ID (%s) assigned", cgID),
		)

		t.Log("Setting up SDK expectations for consumer group deletion")
		sdk.ConsumerGroupSDK.EXPECT().DeleteConsumerGroup(mock.Anything, cp.GetKonnectID(), cgID).Return(nil, nil)

		t.Logf("Deleting KongConsumerGroup %s and waiting for it to disappear", client.ObjectKeyFromObject(createdConsumerGroup))
		require.NoError(t, clientNamespaced.Delete(ctx, createdConsumerGroup))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdConsumerGroup, consts.WaitTime, consts.TickTime)
	})
}
