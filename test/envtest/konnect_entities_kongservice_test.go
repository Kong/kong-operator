package envtest

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKongService(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongService](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongService](&metricsmocks.MockRecorder{}),
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

	t.Run("adding, patching and deleting KongService", func(t *testing.T) {
		const (
			upstreamID = "kup-12345"
			serviceID  = "service-12345"
			host       = "example.com"
			port       = int64(8081)
		)

		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Service) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{
					Service: &sdkkonnectcomp.ServiceOutput{
						ID: lo.ToPtr(serviceID),
					},
				},
				nil,
			)

		t.Log("Creating a KongService")
		createdService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.Host = host
			},
		)

		t.Log("Waiting for Service to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(kt *configurationv1alpha1.KongService) bool {
			return kt.GetKonnectID() == serviceID && k8sutils.IsProgrammed(kt)
		}, "KongService didn't get Programmed status condition or didn't get the correct (service-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.ServicesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Service update")
		sdk.ServicesSDK.EXPECT().
			UpsertService(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertServiceRequest) bool {
					return req.ServiceID == serviceID && req.Service.Port != nil && *req.Service.Port == port
				}),
			).
			Return(&sdkkonnectops.UpsertServiceResponse{}, nil)

		t.Log("Patching KongService")
		serviceToPatch := createdService.DeepCopy()
		serviceToPatch.Spec.Port = port
		require.NoError(t, clientNamespaced.Patch(ctx, serviceToPatch, client.MergeFrom(createdService)))

		t.Log("Waiting for Service to be patched")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdService),
				objectMatchesKonnectID[*configurationv1alpha1.KongService](serviceID),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongService](),
				func(s *configurationv1alpha1.KongService) bool {
					return s.Spec.Port == port
				},
			),
			"KongService didn't get patched",
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.ServicesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Service deletion")
		sdk.ServicesSDK.EXPECT().
			DeleteService(
				mock.Anything,
				cp.GetKonnectID(),
				serviceID,
			).
			Return(&sdkkonnectops.DeleteServiceResponse{}, nil)

		t.Log("Deleting KongService")
		require.NoError(t, clientNamespaced.Delete(ctx, createdService))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdService, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.ServicesSDK, waitTime, tickTime)
	})

	t.Run("trying to attach KongService to KonnectGatewayControlPlane of type KIC fails (due to CP being read only)", func(t *testing.T) {
		const (
			upstreamID = "kup-kic-12345"
			serviceID  = "service-12345"
			host       = "example.com"
			port       = int64(8081)
		)

		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth,
			deploy.KonnectGatewayControlPlaneType(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController),
		)
		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		errBody := `{
					"code": 7,
					"message": "usage constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"messages": [
								"operation not permitted on KIC cluster"
							]
						}
					]
				}`
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Service) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{},
				sdkkonnecterrs.NewSDKError("API error occurred", 403, errBody, nil),
			)

		t.Log("Creating a KongService with ControlPlaneRef type=konnectNamespacedRef")
		createdService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.Host = host
			},
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.ServicesSDK, waitTime, tickTime)

		t.Log("Waiting for Service to get the Programmed condition set to False")
		watchFor(t, ctx, w, apiwatch.Modified, func(kt *configurationv1alpha1.KongService) bool {
			if kt.GetName() != createdService.GetName() {
				return false
			}
			if kt.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				kt.GetControlPlaneRef().KonnectNamespacedRef == nil ||
				kt.GetControlPlaneRef().KonnectNamespacedRef.Name != cp.GetName() {
				return false
			}

			c, ok := k8sutils.GetCondition("Programmed", kt)
			if !ok {
				return false
			}
			return c.Status == metav1.ConditionFalse && c.Reason == "FailedToCreate"
		}, "KongService should get the Programmed condition set to status=False due to using invalid (KIC) ControlPlaneRef")
	})

	t.Run("should handle konnectID control plane references", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		const (
			upstreamID = "kup-12345"
			serviceID  = "service-12345"
			host       = "example.com"
			port       = int64(8081)
		)

		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Service) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{
					Service: &sdkkonnectcomp.ServiceOutput{
						ID: lo.ToPtr(serviceID),
					},
				},
				nil,
			)

		t.Log("Creating a KongService with ControlPlaneRef type=konnectID")
		createdService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.Host = host
			},
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for Service to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(kt *configurationv1alpha1.KongService) bool {
			if kt.GetName() != createdService.GetName() {
				return false
			}
			if kt.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return kt.GetKonnectID() == serviceID && k8sutils.IsProgrammed(kt)
		}, "KongService didn't get Programmed status condition or didn't get the correct (service-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.ServicesSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id   = "service-12345"
			host = "example.com"
			port = int64(8081)
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Service) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{
					Service: &sdkkonnectcomp.ServiceOutput{
						ID: lo.ToPtr(id),
					},
				},
				nil,
			)

		t.Log("Creating a KongService")
		created := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.Host = host
			},
		)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("KongService didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for Service to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongService didn't get Programmed and/or ControlPlaneRefValid status condition set to False")
	})

	t.Run("detaching and reattaching the referenced CP correctly removes and readds the konnect cleanup finalizer", func(t *testing.T) {
		const (
			id   = "abc-12345678"
			name = "name-3"
			host = "example2.com"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Service) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{
					Service: &sdkkonnectcomp.ServiceOutput{
						ID: lo.ToPtr(id),
					},
				},
				nil,
			)

		created := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.Host = host
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
				assertNot(objectHasFinalizer[*configurationv1alpha1.KongService](konnect.KonnectCleanupFinalizer)),
				conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			),
			"Object didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)

		id2 := uuid.New().String()
		t.Log("Setting up SDK expectations on KongConsumer update (after KonnectGatewayControlPlane deletion)")
		sdk.ServicesSDK.EXPECT().
			UpsertService(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertServiceRequest) bool {
				return r.ServiceID == id && r.Service.Host == host
			})).
			Return(&sdkkonnectops.UpsertServiceResponse{
				Service: &sdkkonnectcomp.ServiceOutput{
					ID: lo.ToPtr(id2),
				},
			}, nil)

		cp = deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth,
			func(obj client.Object) {
				cpNew := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
				cpNew.Name = cp.Name
			},
		)

		t.Log("Waiting for object to be get Programmed with status=True and konnect cleanup finalizer re added")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.KongService](),
				objectHasFinalizer[*configurationv1alpha1.KongService](konnect.KonnectCleanupFinalizer),
			),
			"Object didn't get Programmed set to True",
		)
	})
}
