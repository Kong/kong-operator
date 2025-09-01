package ops

import (
	"io"
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	kcfgkonnect "github.com/kong/kubernetes-configuration/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

type createTestCase[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
] struct {
	name                  string
	entity                TEnt
	sdkFunc               func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper
	expectedErrorContains string
	assertions            func(t *testing.T, ent TEnt)
}

func TestCreate(t *testing.T) {
	testCasesForKonnectGatewayControlPlane := []createTestCase[
		konnectv1alpha2.KonnectGatewayControlPlane,
		*konnectv1alpha2.KonnectGatewayControlPlane,
	]{
		{
			name: "BadRequest error is not propagated to the caller but object's status condition is updated",
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						Name: "test-cp",
						Labels: map[string]string{
							"label": "very-long-label-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						},
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
			},
			sdkFunc: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper {
				sdk.ControlPlaneSDK.
					EXPECT().
					CreateControlPlane(
						mock.Anything,
						mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
							if req.Name != "test-cp" ||
								req.Labels == nil {
								return false
							}
							// NOTE: do not check the value as we're truncating the label values to
							// prevent them from being rejected by Konnect.
							_, ok := req.Labels["label"]
							return ok
						}),
					).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Title:  "Invalid Request",
							Detail: "Invalid Parameters",
							InvalidParameters: []sdkkonnectcomp.InvalidParameters{
								{
									InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
										Field:  "labels",
										Rule:   sdkkonnectcomp.InvalidRulesIsLabel.ToPointer(),
										Reason: "Label value exceeds maximum of 63 characters",
									},
								},
							},
						},
					).
					Once()
				return sdk
			},
			// No error returned, only object's status condition updated to prevent endless reconciliation
			// that operator cannot recover from (object's manifest needs to be changed).
			assertions: func(t *testing.T, ent *konnectv1alpha2.KonnectGatewayControlPlane) {
				require.Len(t, ent.Status.Conditions, 1)
				assert.Equal(t, metav1.ConditionFalse, ent.Status.Conditions[0].Status)
				assert.EqualValues(t, kcfgkonnect.KonnectEntitiesFailedToCreateReason, ent.Status.Conditions[0].Reason)
				assert.JSONEq(t,
					`{"status":400,"title":"Invalid Request","instance":"","detail":"Invalid Parameters","invalid_parameters":[{"field":"labels","rule":"is_label","reason":"Label value exceeds maximum of 63 characters","source":null}]}`,
					ent.Status.Conditions[0].Message)
			},
		},
		{
			name: "SDKError (data constraint error) is not propagated to the caller but object's status condition is updated",
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						Name: "test-cp",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
			},
			sdkFunc: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper {
				sdk.ControlPlaneSDK.
					EXPECT().
					CreateControlPlane(
						mock.Anything,
						mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
							return req.Name == "test-cp"
						}),
					).
					Return(
						nil,
						&sdkkonnecterrs.SDKError{
							Message:    "data constraint error",
							StatusCode: http.StatusBadRequest,
							Body: `{` +
								`"code": 3,` +
								`"message": "validation error",` +
								`"details": [` +
								`  {` +
								`      "@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",` +
								`      "type": "ERROR_TYPE_FIELD",` +
								`      "field": "tags[0]",` +
								`      "messages": [` +
								`        "length must be <= 128, but got 138"` +
								`      ]` +
								`  }` +
								`]` +
								`}`,
						},
					).
					Once()
				return sdk
			},
			// No error returned, only object's status condition updated to prevent endless reconciliation
			// that operator cannot recover from (object's manifest needs to be changed).
			assertions: func(t *testing.T, ent *konnectv1alpha2.KonnectGatewayControlPlane) {
				require.Len(t, ent.Status.Conditions, 1,
					"Expected one condition (Programmed) to be set",
				)
				assert.Equal(t, metav1.ConditionFalse, ent.Status.Conditions[0].Status,
					"Expected Programmed condition to be set to false",
				)
				assert.EqualValues(t, kcfgkonnect.KonnectEntitiesFailedToCreateReason, ent.Status.Conditions[0].Reason,
					"Expected Programmed condition's reason to be set to FailedToCreate",
				)
				assert.Equal(t,
					"data constraint error: Status 400\n"+
						`{"code": 3,"message": "validation error","details": [  {      "@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",      "type": "ERROR_TYPE_FIELD",      "field": "tags[0]",      "messages": [        "length must be <= 128, but got 138"      ]  }]}`,
					ent.Status.Conditions[0].Message,
					"Expected Programmed condition's message to be set to error message returned by Konnect API",
				)
			},
		},
		{
			name: "other types of errors are propagated to the caller and object's status condition is updated",
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						Name: "test-cp",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
			},
			sdkFunc: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper) *sdkmocks.MockSDKWrapper {
				sdk.ControlPlaneSDK.
					EXPECT().
					CreateControlPlane(
						mock.Anything,
						mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
							return req.Name == "test-cp"
						}),
					).
					Return(
						nil,
						io.ErrUnexpectedEOF,
					).
					Once()
				return sdk
			},
			expectedErrorContains: "unexpected EOF",
			assertions: func(t *testing.T, ent *konnectv1alpha2.KonnectGatewayControlPlane) {
				require.Len(t, ent.Status.Conditions, 1,
					"Expected one condition (Programmed) to be set",
				)
				assert.Equal(t, metav1.ConditionFalse, ent.Status.Conditions[0].Status,
					"Expected Programmed condition to be set to false",
				)
				assert.EqualValues(t, kcfgkonnect.KonnectEntitiesFailedToCreateReason, ent.Status.Conditions[0].Reason,
					"Expected Programmed condition's reason to be set to FailedToCreate",
				)
				assert.Equal(t,
					"failed to create KonnectGatewayControlPlane test-ns/test-cp: unexpected EOF",
					ent.Status.Conditions[0].Message,
					"Expected Programmed condition's message to be set to error message returned by Konnect API",
				)
			},
		},
	}

	testCreate(t, testCasesForKonnectGatewayControlPlane)
}

func testCreate[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](t *testing.T, testcases []createTestCase[T, TEnt],
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

			_, err := Create(t.Context(), sdk, fakeClient, &metricsmocks.MockRecorder{}, tc.entity)
			if tc.expectedErrorContains != "" {
				require.ErrorContains(t, err, tc.expectedErrorContains)
			} else {
				require.NoError(t, err)
			}

			if tc.assertions != nil {
				tc.assertions(t, tc.entity)
			}
		})
	}
}

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
		konnectv1alpha2.KonnectGatewayControlPlane,
		*konnectv1alpha2.KonnectGatewayControlPlane,
	]{
		{
			name: "no Konnect ID and no Programmed status condition - delete is not called",
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
			},
		},
		{
			name: "no Konnect ID and Programmed=False status condition - delete is not called",
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						Name: "test-cp",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
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
			entity: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "test-ns",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						Name: "test-cp",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
							Status: metav1.ConditionTrue,
						},
					},
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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

			err := Delete(t.Context(), sdk, fakeClient, &metricsmocks.MockRecorder{}, tc.entity)
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
				return
			}

			require.NoError(t, err)
		})
	}
}
