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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/controller/konnect/ops"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongConsumerGroup(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1beta1.KongConsumerGroup](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1beta1.KongConsumerGroup](&metricsmocks.MockRecorder{}),
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
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroup) bool {
					return cg.Name == cgName
				}),
			).Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr(cgID),
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
		watchFor(t, ctx, cWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != cg.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)

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

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)

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

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)
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
				Body:       ErrBodyDataConstraintError,
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
		deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		watchFor(t, ctx, cWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			return c.GetKonnectID() == cgID && k8sutils.IsProgrammed(c)
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		const (
			cgID   = "cg-with-konnectid-cp-ref-id"
			cgName = "cg-with-konnectid-cp-ref"
		)
		t.Log("Setting up SDK expectations on KongConsumerGroup creation")
		sdk.ConsumerGroupSDK.EXPECT().
			CreateConsumerGroup(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(cg sdkkonnectcomp.ConsumerGroup) bool {
					return cg.Name == cgName
				}),
			).Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr(cgID),
			},
		}, nil,
		)

		t.Log("Creating KongConsumerGroup with ControlPlaneRef type=konnectID")
		cg := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cg := obj.(*configurationv1beta1.KongConsumerGroup)
				cg.Spec.Name = cgName
			},
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongConsumerGroup to be programmed")
		watchFor(t, ctx, cWatch, apiwatch.Modified, func(c *configurationv1beta1.KongConsumerGroup) bool {
			if c.GetName() != cg.GetName() {
				return false
			}
			if c.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongConsumerGroup's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id   = "abc-12345"
			name = "name-1"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

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
						ID:   lo.ToPtr(id),
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
		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumerGroupSDK, waitTime, tickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("ConsumerGroup didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for KongConsumerGroup to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongConsumerGroup didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})

	t.Run("Adopting an existing consumer group", func(t *testing.T) {
		cgID := uuid.NewString()
		cgName := "adopted-consumer-group"

		w := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating consumer groups")
		sdk.ConsumerGroupSDK.EXPECT().GetConsumerGroup(
			mock.Anything,
			cgID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
					Name: cgName,
					ID:   lo.ToPtr(cgID),
				},
			},
		}, nil)
		sdk.ConsumerGroupSDK.EXPECT().UpsertConsumerGroup(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertConsumerGroupRequest) bool {
				return req.ConsumerGroupID == cgID && req.ControlPlaneID == cp.GetKonnectID()
			}),
		).Return(nil, nil)

		t.Log("Creating a KongConsumerGroup to adopt the existing consumer group")
		createdConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectAdoptOptions[*configurationv1beta1.KongConsumerGroup](commonv1alpha1.AdoptModeOverride, cgID),
		)

		t.Logf("Waiting for KongConsumerGroup %s to be programmed and set Konnect ID", client.ObjectKeyFromObject(createdConsumerGroup))
		watchFor(t, ctx, w, apiwatch.Modified, func(cg *configurationv1beta1.KongConsumerGroup) bool {
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
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdConsumerGroup, waitTime, tickTime)
	})
}
