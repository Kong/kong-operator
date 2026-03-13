package envtest

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

var konnectEventGatewayTestCases = []konnectEntityReconcilerTestCase{
	{
		name:    "should create event gateway successfully",
		enabled: true,
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectEventGateway(t, ctx, cl, auth, func(obj client.Object) {
				eg := obj.(*konnectv1alpha1.KonnectEventGateway)
				eg.Name = "eg-1"
				eg.Spec.CreateGatewayRequest.Name = "eg-1"
			})
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			sdk.EventGatewaySDK.EXPECT().
				CreateEventGateway(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateGatewayRequest) bool {
						return req.Name == "eg-1"
					}),
				).
				Return(&sdkkonnectops.CreateEventGatewayResponse{
					EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
						ID:   "eg-id-1",
						Name: "eg-1",
					},
				}, nil)

			sdk.EventGatewaySDK.EXPECT().
				UpdateEventGateway(mock.Anything, "eg-id-1", mock.Anything).
				Return(&sdkkonnectops.UpdateEventGatewayResponse{
					EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
						ID:   "eg-id-1",
						Name: "eg-1",
					},
				}, nil).
				// NOTE: UpdateEventGateway may be called on subsequent reconciles after the
				// initial create sets the Konnect ID.
				Maybe()
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			eg := &konnectv1alpha1.KonnectEventGateway{}
			require.NoError(t,
				cl.Get(ctx, k8stypes.NamespacedName{Namespace: ns.Name, Name: "eg-1"}, eg),
			)
			assert.Equal(t, "eg-id-1", eg.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(eg.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(eg, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on event gateway",
			)
		},
	},
	{
		name:    "receiving HTTP Conflict 409 on creation results in lookup by UID and setting Konnect ID",
		enabled: true,
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectEventGateway(t, ctx, cl, auth, func(obj client.Object) {
				eg := obj.(*konnectv1alpha1.KonnectEventGateway)
				eg.Name = "eg-conflict"
				eg.Spec.CreateGatewayRequest.Name = "eg-conflict"
			})
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			sdk.EventGatewaySDK.EXPECT().
				CreateEventGateway(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateGatewayRequest) bool {
						return req.Name == "eg-conflict"
					}),
				).
				Return(nil, &sdkkonnecterrs.ConflictError{})

			sdk.EventGatewaySDK.EXPECT().
				ListEventGateways(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.ListEventGatewaysRequest) bool {
						return req.Filter != nil &&
							req.Filter.Name != nil &&
							req.Filter.Name.Contains == "eg-conflict"
					}),
				).
				RunAndReturn(func(ctx context.Context, req sdkkonnectops.ListEventGatewaysRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewaysResponse, error) {
					var eg konnectv1alpha1.KonnectEventGateway
					if err := cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "eg-conflict"}, &eg); err != nil {
						return nil, err
					}
					return &sdkkonnectops.ListEventGatewaysResponse{
						ListEventGatewaysResponse: &sdkkonnectcomp.ListEventGatewaysResponse{
							Data: []sdkkonnectcomp.EventGatewayInfo{
								{
									ID:   "eg-existing-id",
									Name: "eg-conflict",
									Labels: map[string]string{
										ops.KubernetesUIDLabelKey: string(eg.GetUID()),
									},
								},
							},
						},
					}, nil
				})

			sdk.EventGatewaySDK.EXPECT().
				UpdateEventGateway(mock.Anything, "eg-existing-id", mock.Anything).
				Return(&sdkkonnectops.UpdateEventGatewayResponse{
					EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
						ID:   "eg-existing-id",
						Name: "eg-conflict",
					},
				}, nil).
				Maybe()
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			eg := &konnectv1alpha1.KonnectEventGateway{}
			require.NoError(t,
				cl.Get(ctx, k8stypes.NamespacedName{Namespace: ns.Name, Name: "eg-conflict"}, eg),
			)
			assert.Equal(t, "eg-existing-id", eg.Status.ID, "ID should be adopted from the existing Konnect entity")
			assert.True(t, conditionsContainProgrammedTrue(eg.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(eg, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on event gateway",
			)
		},
	},
	{
		name:    "network error sets Programmed condition to False",
		enabled: true,
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectEventGateway(t, ctx, cl, auth, func(obj client.Object) {
				eg := obj.(*konnectv1alpha1.KonnectEventGateway)
				eg.Name = "eg-no-connectivity"
				eg.Spec.CreateGatewayRequest.Name = "eg-no-connectivity"
			})
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			networkErr := &url.Error{
				Op:  "Post",
				URL: "https://us.api.konghq.com/v1/event-gateways",
				Err: errors.New("dial tcp: lookup us.api.konghq.com: no such host"),
			}
			sdk.EventGatewaySDK.EXPECT().
				CreateEventGateway(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateGatewayRequest) bool {
						return req.Name == "eg-no-connectivity"
					}),
				).
				Return(nil, networkErr)
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			eg := &konnectv1alpha1.KonnectEventGateway{}
			require.NoError(t,
				cl.Get(ctx, k8stypes.NamespacedName{Namespace: ns.Name, Name: "eg-no-connectivity"}, eg),
			)
			assert.True(t, conditionsContainProgrammedFalse(eg.Status.Conditions),
				"Programmed condition should be set to False due to network error",
			)
			assert.True(t,
				conditionsContainProgrammedWithReason(
					eg.Status.Conditions,
					konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed,
				),
				"Programmed condition reason should indicate KonnectAPIOpFailed",
			)
		},
	},
	{
		name:    "unresolved APIAuth ref sets both APIAuthResolvedRef and Programmed conditions to False",
		enabled: true,
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			fakeAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "nonexistent-auth"},
			}
			deploy.KonnectEventGateway(t, ctx, cl, fakeAuth, func(obj client.Object) {
				eg := obj.(*konnectv1alpha1.KonnectEventGateway)
				eg.Name = "eg-unresolved-auth"
				eg.Spec.CreateGatewayRequest.Name = "eg-unresolved-auth"
			})
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			// No SDK calls expected. Reconciler returns early when auth ref is not found.
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			eg := &konnectv1alpha1.KonnectEventGateway{}
			require.NoError(t,
				cl.Get(ctx, k8stypes.NamespacedName{Namespace: ns.Name, Name: "eg-unresolved-auth"}, eg),
			)
			assert.True(t, lo.ContainsBy(eg.Status.Conditions, func(c metav1.Condition) bool {
				return c.Type == konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType &&
					c.Status == metav1.ConditionFalse &&
					c.Reason == konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound
			}), "APIAuthResolvedRef condition should be False with RefNotFound reason")
			assert.True(t, conditionsContainProgrammedFalse(eg.Status.Conditions),
				"Programmed condition should be set to False when APIAuth ref is not found",
			)
			assert.True(t,
				conditionsContainProgrammedWithReason(
					eg.Status.Conditions,
					konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists,
				),
				"Programmed condition reason should indicate ConditionWithStatusFalseExists",
			)
		},
	},
}

// TestKonnectEventGateway_CrossNamespaceRefFlow verifies the full lifecycle of a
// KonnectEventGateway that references a KonnectAPIAuthConfiguration in another
// namespace. It runs in two phases:
//
//  1. Without a KongReferenceGrant the gateway should have
//     APIAuthResolvedRef=False (RefNotPermitted) and Programmed=False.
//  2. After a KongReferenceGrant and the referenced KonnectAPIAuthConfiguration
//     are created in the auth namespace, the reconciler should recover and
//     eventually set Programmed=True with a Konnect ID.
func TestKonnectEventGateway_CrossNamespaceRefFlow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, _ := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	cl := mgr.GetClient()
	factory := sdkmocks.NewMockSDKFactory(t)

	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, cl,
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectEventGateway](&metricsmocks.MockRecorder{})))

	egNs := deploy.Namespace(t, ctx, cl)
	authNs := deploy.Namespace(t, ctx, cl)

	const (
		egName   = "eg-xns-flow"
		authName = "auth-in-other-ns"
		egID     = "eg-xns-id"
	)

	// Reference a KonnectAPIAuthConfiguration in authNs by name.
	// The auth and grant do not yet exist. The cross-namespace grant check runs first.
	fakeAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: authName},
	}
	deploy.KonnectEventGateway(t, ctx, cl, fakeAuth, func(obj client.Object) {
		eg := obj.(*konnectv1alpha1.KonnectEventGateway)
		eg.Name = egName
		eg.Namespace = egNs.Name
		eg.Spec.CreateGatewayRequest.Name = egName
		eg.Spec.KonnectConfiguration.Namespace = &authNs.Name
	})

	t.Log("Phase 1: no grant, expecting RefNotPermitted and Programmed=False")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		eg := &konnectv1alpha1.KonnectEventGateway{}
		require.NoError(collect,
			cl.Get(ctx,
				k8stypes.NamespacedName{
					Namespace: egNs.Name,
					Name:      egName,
				},
				eg,
			),
		)
		assert.True(collect, lo.ContainsBy(eg.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType &&
				c.Status == metav1.ConditionFalse &&
				c.Reason == konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotPermitted
		}), "APIAuthResolvedRef condition should be False with RefNotPermitted reason")
		assert.True(collect, conditionsContainProgrammedFalse(eg.Status.Conditions),
			"Programmed condition should be set to False when cross-namespace ref is not permitted",
		)
	}, 10*time.Second, 200*time.Millisecond)

	t.Log("Phase 2: create auth + grant, expecting reconciliation to succeed")

	// Set up SDK expectations for the successful reconciliation after the grant is in place.
	factory.SDK.EventGatewaySDK.EXPECT().
		CreateEventGateway(mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateGatewayRequest) bool {
				return req.Name == egName
			}),
		).
		Return(&sdkkonnectops.CreateEventGatewayResponse{
			EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
				ID:   egID,
				Name: egName,
			},
		}, nil)

	factory.SDK.EventGatewaySDK.EXPECT().
		UpdateEventGateway(mock.Anything, egID, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayResponse{
			EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
				ID:   egID,
				Name: egName,
			},
		}, nil).
		// NOTE: UpdateEventGateway may be called on subsequent reconciles after the
		// initial create sets the Konnect ID.
		Maybe()

	// Create a KonnectAPIAuthConfiguration with a valid status in authNs.
	authCl := client.NewNamespacedClient(cl, authNs.Name)
	auth := deploy.KonnectAPIAuthConfiguration(t, ctx, authCl, func(obj client.Object) {
		o := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		o.GenerateName = ""
		o.Name = authName
	})
	auth.Status.Conditions = []metav1.Condition{{
		Type:               konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
		ObservedGeneration: auth.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, cl.Status().Update(ctx, auth))

	// Create KongReferenceGrant in authNs allowing egNs/KonnectEventGateway to reference
	// authNs/KonnectAPIAuthConfiguration.
	deploy.KongReferenceGrant(t, ctx, authCl,
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:      configurationv1alpha1.Kind("KonnectEventGateway"),
			Namespace: configurationv1alpha1.Namespace(egNs.Name),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:  configurationv1alpha1.Kind("KonnectAPIAuthConfiguration"),
		}),
	)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		eg := &konnectv1alpha1.KonnectEventGateway{}
		require.NoError(collect,
			cl.Get(ctx,
				k8stypes.NamespacedName{
					Namespace: egNs.Name,
					Name:      egName,
				},
				eg,
			),
		)
		assert.Equal(collect, egID, eg.Status.ID,
			"Konnect ID should be set after grant is in place",
		)
		assert.True(collect, conditionsContainProgrammedTrue(eg.Status.Conditions),
			"Programmed condition should be set to True after grant is in place",
		)
		assert.True(collect, controllerutil.ContainsFinalizer(eg, konnect.KonnectCleanupFinalizer),
			"Finalizer should be set on event gateway",
		)
	}, 10*time.Second, 200*time.Millisecond)
}
