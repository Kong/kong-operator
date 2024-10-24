package ops

import (
	"context"
	"testing"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

type deleteTestCase[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
] struct {
	name          string
	entity        TEnt
	sdkFunc       func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper
	expectedError string
}

func TestDelete(t *testing.T) {
	testCasesForKonnectGatewayControlPlane := []deleteTestCase[
		konnectv1alpha1.KonnectGatewayControlPlane,
		*konnectv1alpha1.KonnectGatewayControlPlane,
	]{
		{
			name: "no Konnect ID and no Programmed status condition - delete is not called",
			entity: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
			},
		},
		{
			name: "no Konnect ID and Programmed=False status condition - delete is not called",
			entity: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
		{
			name: "Konnect ID and Programmed=True status condition",
			entity: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
							Status: metav1.ConditionTrue,
						},
					},
					KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
						ID: "12345",
					},
				},
			},
			sdkFunc: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper {
				sdk.ControlPlaneSDK.
					EXPECT().
					DeleteControlPlane(mock.Anything, "12345").
					Return(
						&sdkkonnectops.DeleteControlPlaneResponse{},
						nil,
					).
					Once()
				return sdk
			},
		},
	}

	testDelete(t, testCasesForKonnectGatewayControlPlane)
}

func testDelete[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](t *testing.T, testcases []deleteTestCase[T, TEnt],
) {
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				Build()

			sdk := sdkmocks.NewMockSDKWrapperWithT(t)
			if tc.sdkFunc != nil {
				sdk = tc.sdkFunc(t, sdk)
			}

			err := Delete(context.Background(), sdk, fakeClient, tc.entity)
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
				return
			}

			require.NoError(t, err)
		})
	}
}
