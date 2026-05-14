package konnect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestHandleGeneratedTypeReferences(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "ignores entities without generated parent refs",
			run: func(t *testing.T) {
				ent := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc",
						Namespace: "default",
					},
				}

				r := &KonnectEntityReconciler[
					configurationv1alpha1.KongService, *configurationv1alpha1.KongService,
				]{
					Client: fake.NewClientBuilder().WithScheme(scheme.Get()).Build(),
				}

				stop, res, err := r.handleGeneratedTypeParentReferences(t.Context(), ent)

				require.NoError(t, err)
				assert.False(t, stop)
				assert.True(t, res.IsZero())
			},
		},
		{
			name: "continues when event gateway listener parent ref is resolved",
			run: func(t *testing.T) {
				ent := &konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "listener-policy",
						Namespace:  "default",
						Generation: 1,
					},
					Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
						EventGatewayListenerRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "listener",
							},
						},
					},
				}

				parent := &konnectv1alpha1.EventGatewayListener{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "listener",
						Namespace: "default",
					},
					Status: konnectv1alpha1.EventGatewayListenerStatus{
						Conditions: []metav1.Condition{{
							Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             "Programmed",
							ObservedGeneration: 1,
							LastTransitionTime: metav1.Now(),
						}},
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: "listener-konnect-id"},
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(ent).
					WithObjects(ent, parent).
					Build()

				r := &KonnectEntityReconciler[
					konnectv1alpha1.EventGatewayListenerPolicy, *konnectv1alpha1.EventGatewayListenerPolicy,
				]{
					Client: cl,
				}

				stop, res, err := r.handleGeneratedTypeParentReferences(t.Context(), ent)

				require.NoError(t, err)
				assert.False(t, stop)
				assert.True(t, res.IsZero())

				updated := &konnectv1alpha1.EventGatewayListenerPolicy{}
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updated))
				assert.Equal(t, "listener-konnect-id", updated.GetEventGatewayListenerID())

				cond, ok := k8sutils.GetCondition(
					kcfgconsts.ConditionType(ent.GetStatusConditionTypeParentRefValid()),
					updated,
				)
				require.True(t, ok)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, konnectv1alpha1.EventGatewayListenerRefReasonValid, cond.Reason)
			},
		},
		{
			name: "continues when event gateway parent ref is resolved",
			run: func(t *testing.T) {
				ent := &konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "backend-cluster",
						Namespace:  "default",
						Generation: 1,
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: gatewayRef("event-gateway"),
					},
				}

				parent := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "event-gateway",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						Conditions: []metav1.Condition{{
							Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
							Status:             metav1.ConditionTrue,
							Reason:             "Programmed",
							ObservedGeneration: 1,
							LastTransitionTime: metav1.Now(),
						}},
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: "gateway-konnect-id"},
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(ent).
					WithObjects(ent, parent).
					Build()

				r := &KonnectEntityReconciler[
					konnectv1alpha1.EventGatewayBackendCluster, *konnectv1alpha1.EventGatewayBackendCluster,
				]{Client: cl}

				stop, res, err := r.handleGeneratedTypeParentReferences(t.Context(), ent)

				require.NoError(t, err)
				assert.False(t, stop)
				assert.True(t, res.IsZero())

				updated := &konnectv1alpha1.EventGatewayBackendCluster{}
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updated))
				assert.Equal(t, "gateway-konnect-id", updated.GetGatewayID())

				cond, ok := k8sutils.GetCondition(
					kcfgconsts.ConditionType(ent.GetStatusConditionTypeParentRefValid()),
					updated,
				)
				require.True(t, ok)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, konnectv1alpha1.EventGatewayRefReasonValid, cond.Reason)
			},
		},
		{
			name: "stops and requeues when event gateway parent is not programmed",
			run: func(t *testing.T) {
				ent := &konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "backend-cluster",
						Namespace:  "default",
						Generation: 1,
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: gatewayRef("event-gateway"),
					},
				}

				parent := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "event-gateway",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						Conditions: []metav1.Condition{{
							Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
							Status:             metav1.ConditionFalse,
							Reason:             "Pending",
							ObservedGeneration: 1,
							LastTransitionTime: metav1.Now(),
						}},
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: "gateway-konnect-id"},
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(ent).
					WithObjects(ent, parent).
					Build()

				r := &KonnectEntityReconciler[
					konnectv1alpha1.EventGatewayBackendCluster, *konnectv1alpha1.EventGatewayBackendCluster,
				]{Client: cl}

				stop, res, err := r.handleGeneratedTypeParentReferences(t.Context(), ent)

				require.NoError(t, err)
				assert.True(t, stop)
				assert.Greater(t, res.RequeueAfter, time.Duration(0))

				updated := &konnectv1alpha1.EventGatewayBackendCluster{}
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updated))

				cond, ok := k8sutils.GetCondition(
					kcfgconsts.ConditionType(ent.GetStatusConditionTypeParentRefValid()),
					updated,
				)
				require.True(t, ok)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, konnectv1alpha1.EventGatewayRefReasonNotProgrammed, cond.Reason)
			},
		},
		{
			name: "removes cleanup finalizer when portal parent does not exist",
			run: func(t *testing.T) {
				ent := &konnectv1alpha1.PortalPage{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "portal-page",
						Namespace:  "default",
						Generation: 1,
						Finalizers: []string{KonnectCleanupFinalizer},
					},
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef("portal"),
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithStatusSubresource(ent).
					WithObjects(ent).
					Build()

				r := &KonnectEntityReconciler[
					konnectv1alpha1.PortalPage, *konnectv1alpha1.PortalPage,
				]{Client: cl}

				stop, res, err := r.handleGeneratedTypeParentReferences(t.Context(), ent)

				require.NoError(t, err)
				assert.True(t, stop)
				assert.True(t, res.IsZero())

				updated := &konnectv1alpha1.PortalPage{}
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updated))
				assert.Empty(t, updated.Finalizers)

				cond, ok := k8sutils.GetCondition(
					kcfgconsts.ConditionType(ent.GetStatusConditionTypeParentRefValid()),
					updated,
				)
				require.True(t, ok)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, konnectv1alpha1.PortalRefReasonInvalid, cond.Reason)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
