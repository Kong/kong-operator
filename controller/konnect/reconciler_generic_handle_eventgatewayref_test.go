package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

type eventGatewayRefHandledObject interface {
	eventGatewayRefAccessor
	k8sutils.ConditionsAwareObject
	GetGatewayID() string
	SetGatewayID(string)
}

func TestHandleEventGatewayRef_HandlesGeneratedEventGatewayChildren(t *testing.T) {
	tests := []struct {
		name    string
		ent     client.Object
		updated client.Object
	}{
		{
			name: "event gateway listener",
			ent: &konnectv1alpha1.EventGatewayListener{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-listener",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.EventGatewayListenerSpec{
					GatewayRef: commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "event-gateway",
						},
					},
				},
			},
			updated: &konnectv1alpha1.EventGatewayListener{},
		},
		{
			name: "event data plane certificate",
			ent: &konnectv1alpha1.KonnectEventDataPlaneCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-cert",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
					GatewayRef: commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "event-gateway",
						},
					},
				},
			},
			updated: &konnectv1alpha1.KonnectEventDataPlaneCertificate{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &konnectv1alpha1.KonnectEventGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-gateway",
					Namespace: "default",
				},
				Status: konnectv1alpha1.KonnectEventGatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
					KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
						ID: "gateway-konnect-id",
					},
				},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithStatusSubresource(tc.ent).
				WithObjects(gateway, tc.ent).
				Build()

			res, err := handleEventGatewayRef(t.Context(), cl, tc.ent.(k8sutils.ConditionsAwareObject))
			require.NoError(t, err)
			require.True(t, res.IsZero())

			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(tc.ent), tc.updated))

			updated, ok := tc.updated.(eventGatewayRefHandledObject)
			require.True(t, ok)
			require.Equal(t, "gateway-konnect-id", updated.GetGatewayID())

			cond, ok := k8sutils.GetCondition(konnectv1alpha1.EventGatewayRefValidConditionType, updated)
			require.True(t, ok)
			require.Equal(t, metav1.ConditionTrue, cond.Status)
			require.Equal(t, konnectv1alpha1.EventGatewayRefReasonValid, cond.Reason)
		})
	}
}
