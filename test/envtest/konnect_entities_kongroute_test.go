package envtest

import (
	"fmt"
	"slices"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKongRoute(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongRoute](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongRoute](&metricsmocks.MockRecorder{}),
		),
	)

	ns2 := deploy.Namespace(t, ctx, mgr.GetClient())
	t.Log("Setting up clients")
	clientOptions := client.Options{
		Scheme: scheme.Get(),
	}

	cl, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	cl2, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)
	clientNamespaced2 := client.NewNamespacedClient(mgr.GetClient(), ns2.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
	svc := deploy.KongServiceWithID(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
	)

	t.Run("adding, patching and deleting KongRoute", func(t *testing.T) {
		const routeID = "route-12345"

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Route creation")
		sdk.RoutesSDK.EXPECT().
			CreateRoute(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Route) bool {
					return slices.Equal(req.RouteJSON.Paths, []string{"/path"})
				}),
			).
			Return(
				&sdkkonnectops.CreateRouteResponse{
					Route: &sdkkonnectcomp.Route{
						RouteJSON: &sdkkonnectcomp.RouteJSON{
							ID: new(routeID),
						},
					},
				},
				nil,
			)

		t.Log("Creating a KongRoute")
		createdRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(svc),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongRoute)
				s.Spec.Paths = []string{"/path"}
			},
		)

		t.Log("Waiting for Route to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			if r.GetName() != createdRoute.GetName() {
				return false
			}
			return r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		}, "KongRoute didn't get Programmed status condition or didn't get the correct (route-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Route update")
		sdk.RoutesSDK.EXPECT().
			UpsertRoute(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertRouteRequest) bool {
					return req.RouteID == routeID &&
						slices.Equal(req.Route.RouteJSON.Paths, []string{"/path"}) &&
						req.Route.RouteJSON.PreserveHost != nil && *req.Route.RouteJSON.PreserveHost == true
				}),
			).
			Return(&sdkkonnectops.UpsertRouteResponse{}, nil)

		t.Log("Patching KongRoute")
		routeToPatch := createdRoute.DeepCopy()
		routeToPatch.Spec.PreserveHost = new(true)
		require.NoError(t, clientNamespaced.Patch(ctx, routeToPatch, client.MergeFrom(createdRoute)))

		t.Log("Waiting for Route to get the update")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			if r.GetName() != createdRoute.GetName() {
				return false
			}
			if r.Spec.PreserveHost == nil || !*r.Spec.PreserveHost {
				return false
			}
			return r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		}, "KongRoute didn't get patched with PreserveHost=true")

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Route deletion")
		sdk.RoutesSDK.EXPECT().
			DeleteRoute(
				mock.Anything,
				cp.GetKonnectID(),
				routeID,
			).
			Return(&sdkkonnectops.DeleteRouteResponse{}, nil)

		t.Log("Deleting KongRoute")
		require.NoError(t, clientNamespaced.Delete(ctx, createdRoute))

		eventually.WaitForObjectToNotExist(t, ctx, cl, createdRoute, waitTime, tickTime)
	})

	t.Run("Adopting a route attached to a service", func(t *testing.T) {
		routeID := uuid.NewString()
		routeName := "test-adoption-" + uuid.NewString()[:8]

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating routes")
		sdk.RoutesSDK.EXPECT().GetRoute(
			mock.Anything,
			routeID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetRouteResponse{
			Route: &sdkkonnectcomp.Route{
				Type: sdkkonnectcomp.RouteTypeRouteJSON,
				RouteJSON: &sdkkonnectcomp.RouteJSON{
					ID:    &routeID,
					Name:  &routeName,
					Paths: []string{"/path"},
					Service: &sdkkonnectcomp.RouteJSONService{
						ID: new(svc.GetKonnectID()),
					},
				},
			},
		}, nil)
		sdk.RoutesSDK.EXPECT().UpsertRoute(
			mock.Anything,
			mock.MatchedBy(
				func(req sdkkonnectops.UpsertRouteRequest) bool {
					return req.RouteID == routeID
				},
			),
		).Return(nil, nil)

		t.Logf("Creating a KongRoute to adopt the existing route (ID: %s)", routeID)
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(svc),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongRoute](commonv1alpha1.AdoptModeOverride, routeID),
		)

		t.Logf("Waiting for the KongRoute %s/%s to get KonnectID and marked as programmed", ns.Name, createdRoute.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			return r.Name == createdRoute.Name && r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		},
			fmt.Sprintf("KongRoute did not get programmed and set KonnectID to %s", routeID),
		)

		t.Log("Setting up SDK expectations for route deletion")
		sdk.RoutesSDK.EXPECT().DeleteRoute(mock.Anything, cp.GetKonnectID(), routeID).Return(nil, nil)

		t.Logf("Deleting KongRoute %s/%s", ns.Name, createdRoute.Name)
		require.NoError(t, clientNamespaced.Delete(ctx, createdRoute))

		eventually.WaitForObjectToNotExist(t, ctx, cl, createdRoute, waitTime, tickTime)
	})

	t.Run("Adopting a standalone route", func(t *testing.T) {
		routeID := uuid.NewString()
		routeName := "test-adoption-" + uuid.NewString()[:8]

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating routes")
		sdk.RoutesSDK.EXPECT().GetRoute(
			mock.Anything,
			routeID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetRouteResponse{
			Route: &sdkkonnectcomp.Route{
				Type: sdkkonnectcomp.RouteTypeRouteJSON,
				RouteJSON: &sdkkonnectcomp.RouteJSON{
					ID:    &routeID,
					Name:  &routeName,
					Paths: []string{"/path"},
				},
			},
		}, nil)
		sdk.RoutesSDK.EXPECT().UpsertRoute(
			mock.Anything,
			mock.MatchedBy(
				func(req sdkkonnectops.UpsertRouteRequest) bool {
					return req.RouteID == routeID
				},
			),
		).Return(nil, nil)

		t.Logf("Creating a KongRoute to adopt the existing route (ID: %s)", routeID)
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongRoute](commonv1alpha1.AdoptModeOverride, routeID),
		)

		t.Logf("Waiting for the KongRoute %s/%s to get KonnectID and marked as programmed", ns.Name, createdRoute.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			return r.Name == createdRoute.Name && r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		},
			fmt.Sprintf("KongRoute did not get programmed and set KonnectID to %s", routeID),
		)
	})

	t.Run("adopting a route with not matched service should fail", func(t *testing.T) {
		routeID := uuid.NewString()
		routeName := "test-adoption-" + uuid.NewString()[:8]

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting routes")
		sdk.RoutesSDK.EXPECT().GetRoute(
			mock.Anything,
			routeID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetRouteResponse{
			Route: &sdkkonnectcomp.Route{
				Type: sdkkonnectcomp.RouteTypeRouteJSON,
				RouteJSON: &sdkkonnectcomp.RouteJSON{
					ID:    &routeID,
					Name:  &routeName,
					Paths: []string{"/path"},
					Service: &sdkkonnectcomp.RouteJSONService{
						ID: new("another-service-id"),
					},
				},
			},
		}, nil)

		t.Logf("Creating a KongRoute to adopt the existing route (ID: %s)", routeID)
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(svc),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongRoute](commonv1alpha1.AdoptModeOverride, routeID),
		)

		t.Logf("Waiting for the KongRoute %s/%s to be marked as not programmed and not adopted", ns.Name, createdRoute.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			return r.Name == createdRoute.Name &&
				conditionsContainProgrammedFalse(r.GetConditions()) &&
				lo.ContainsBy(r.GetConditions(), func(c metav1.Condition) bool {
					return c.Type == konnectv1alpha1.KonnectEntityAdoptedConditionType &&
						c.Status == metav1.ConditionFalse
				})
		},
			fmt.Sprintf("KongRoute did not get programmed and set KonnectID to %s", routeID),
		)

	})

	t.Run("Cross namespace ref KongRoute -> KonnectNamespacedRefControlPlane yields ResolvedRefs=False without KongReferenceGrant", func(t *testing.T) {
		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Don't setting SDK expectations on KongRoute creation as we do not expect any operations to be made upstream")

		t.Log("Creating a KongRoute with ControlPlaneRef type=konnectID")
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
		)

		t.Log("Waiting for Route to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, w, apiwatch.Modified, func(kr *configurationv1alpha1.KongRoute) bool {
			if kr.GetName() != createdRoute.GetName() {
				return false
			}

			cpRef := kr.GetControlPlaneRef()
			if cpRef == nil {
				return false
			}

			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp.GetName() ||
				cpRef.KonnectNamespacedRef.Namespace != cp.GetNamespace() {
				return false
			}
			return k8sutils.HasConditionFalse(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, kr)
		}, "KongRoute didn't get ResolvedRefs status condition set to False")
	})

	t.Run("Cross namespace ref KongRoute -> KonnectNamespacedRefControlPlane yields ResolvedRefs=True with valid KongReferenceGrant", func(t *testing.T) {
		t.SkipNow()
		const (
			id = "route-1234566"
		)

		var paths = []string{"/path"}

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Setting up SDK expectations on Route creation")
		sdk.RoutesSDK.EXPECT().
			CreateRoute(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Route) bool {
					return slices.Equal(req.RouteJSON.Paths, paths)
				}),
			).
			Return(
				&sdkkonnectops.CreateRouteResponse{
					Route: &sdkkonnectcomp.Route{
						RouteJSON: &sdkkonnectcomp.RouteJSON{
							ID: new(id),
						},
					},
				},
				nil,
			)

		_ = deploy.KongReferenceGrant(t, ctx, clientNamespaced,
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongRoute",
				Namespace: configurationv1alpha1.Namespace(ns2.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
				Kind:  "KonnectGatewayControlPlane",
			}),
		)

		t.Log("Creating a KongRoute with ControlPlaneRef type=konnectID")
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
			func(obj client.Object) {
				r := obj.(*configurationv1alpha1.KongRoute)
				r.Spec.Paths = paths
			},
		)

		t.Log("Waiting for Route to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, w, apiwatch.Modified, func(kr *configurationv1alpha1.KongRoute) bool {
			if kr.GetName() != createdRoute.GetName() {
				return false
			}

			cpRef := kr.GetControlPlaneRef()
			if cpRef == nil {
				return false
			}

			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp.GetName() ||
				cpRef.KonnectNamespacedRef.Namespace != cp.GetNamespace() {
				return false
			}
			return k8sutils.HasConditionTrue(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, kr)
		}, "KongRoute didn't get ResolvedRefs status condition set to True")
		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)
	})

	t.Run("Cross namespace ref KongRoute -> KongService yields ResolvedRefs=False without KongReferenceGrant", func(t *testing.T) {
		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Don't set SDK expectations on KongRoute creation as we do not expect any operations to be made upstream")

		t.Log("Creating a KongRoute with cross-namespace ServiceRef without KongReferenceGrant")
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
			func(obj client.Object) {
				r := obj.(*configurationv1alpha1.KongRoute)
				r.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
					Type: configurationv1alpha1.ServiceRefNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name:      svc.GetName(),
						Namespace: new(ns.Name),
					},
				}
			},
		)

		t.Log("Waiting for Route to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, w, apiwatch.Modified, func(kr *configurationv1alpha1.KongRoute) bool {
			if kr.GetName() != createdRoute.GetName() {
				return false
			}

			svcRef := kr.Spec.ServiceRef
			if svcRef == nil || svcRef.NamespacedRef == nil {
				return false
			}

			if svcRef.Type != configurationv1alpha1.ServiceRefNamespacedRef ||
				svcRef.NamespacedRef.Name != svc.GetName() ||
				svcRef.NamespacedRef.Namespace == nil ||
				*svcRef.NamespacedRef.Namespace != ns.Name {
				return false
			}
			return k8sutils.HasConditionFalse(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, kr)
		}, "KongRoute didn't get ResolvedRefs status condition set to False")
	})

	t.Run("Cross namespace ref KongRoute -> KongService yields ResolvedRefs=True with valid KongReferenceGrant", func(t *testing.T) {
		t.SkipNow()
		const routeID = "route-cross-ns-svc-12345"

		var paths = []string{"/cross-ns-path"}

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Setting up SDK expectations on Route creation")
		sdk.RoutesSDK.EXPECT().
			CreateRoute(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Route) bool {
					return slices.Equal(req.RouteJSON.Paths, paths)
				}),
			).
			Return(
				&sdkkonnectops.CreateRouteResponse{
					Route: &sdkkonnectcomp.Route{
						RouteJSON: &sdkkonnectcomp.RouteJSON{
							ID: new(routeID),
						},
					},
				},
				nil,
			)

		t.Log("Creating KongReferenceGrant to allow cross-namespace reference from KongRoute to KonnectGatewayControlPlane")
		krgCP := deploy.KongReferenceGrant(t, ctx, clientNamespaced,
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongRoute",
				Namespace: configurationv1alpha1.Namespace(ns2.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
				Kind:  "KonnectGatewayControlPlane",
			}),
		)
		t.Cleanup(func() {
			require.NoError(t, client.IgnoreNotFound(clientNamespaced.Delete(ctx, krgCP)))
		})

		t.Log("Creating KongReferenceGrant to allow cross-namespace reference from KongRoute to KongService")
		krgSvc := deploy.KongReferenceGrant(t, ctx, clientNamespaced,
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongRoute",
				Namespace: configurationv1alpha1.Namespace(ns2.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:  "KongService",
			}),
		)
		t.Cleanup(func() {
			require.NoError(t, client.IgnoreNotFound(clientNamespaced.Delete(ctx, krgSvc)))
		})

		t.Log("Creating a KongRoute with cross-namespace ServiceRef")
		createdRoute := deploy.KongRoute(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
			func(obj client.Object) {
				r := obj.(*configurationv1alpha1.KongRoute)
				r.Spec.Paths = paths
				r.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
					Type: configurationv1alpha1.ServiceRefNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name:      svc.GetName(),
						Namespace: new(ns.Name),
					},
				}
			},
		)

		t.Log("Waiting for Route to get ResolvedRefs condition with status=True and be Programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(kr *configurationv1alpha1.KongRoute) bool {
			if kr.GetName() != createdRoute.GetName() {
				return false
			}

			svcRef := kr.Spec.ServiceRef
			if svcRef == nil || svcRef.NamespacedRef == nil {
				return false
			}

			if svcRef.Type != configurationv1alpha1.ServiceRefNamespacedRef ||
				svcRef.NamespacedRef.Name != svc.GetName() ||
				svcRef.NamespacedRef.Namespace == nil ||
				*svcRef.NamespacedRef.Namespace != ns.Name {
				return false
			}
			return k8sutils.HasConditionTrue(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, kr) &&
				kr.GetKonnectID() == routeID && k8sutils.IsProgrammed(kr)
		}, "KongRoute didn't get ResolvedRefs status condition set to True or wasn't Programmed")

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)
	})
}
