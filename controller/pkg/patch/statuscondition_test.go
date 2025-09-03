package patch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	kcfgkonnect "github.com/kong/kubernetes-configuration/v2/api/konnect"

	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func TestPatchStatusWithCondition(t *testing.T) {
	tests := []struct {
		name string
		obj  interface {
			client.Object
			GetConditions() []metav1.Condition
			SetConditions([]metav1.Condition)
		}
		conditionType      kcfgconsts.ConditionType
		conditionStatus    metav1.ConditionStatus
		conditionReason    kcfgconsts.ConditionReason
		conditionMessage   string
		expectedResult     ctrl.Result
		expectedConditions []metav1.Condition
		expectedError      bool
		interceptorFunc    interceptor.Funcs
	}{
		{
			name: "condition is already set and as expected",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(kcfgdataplane.ReadyType),
							Status:             metav1.ConditionTrue,
							Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
							Message:            "Resource is available",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "Resource is available",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(kcfgdataplane.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
					Message:            "Resource is available",
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "condition needs to be updated due to different condition status",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(kcfgdataplane.ReadyType),
							Status:             metav1.ConditionFalse,
							Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(kcfgdataplane.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
					Message:            "",
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "condition needs to be updated due to different condition observed generation",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 2,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(kcfgdataplane.ReadyType),
							Status:             metav1.ConditionTrue,
							Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(kcfgdataplane.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
					Message:            "",
					ObservedGeneration: 2,
				},
			},
		},
		{
			name: "condition needs to be updated due to different condition reason",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(kcfgdataplane.ReadyType),
							Status:             metav1.ConditionFalse,
							Reason:             string(kcfgdataplane.ResourceReadyReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionFalse,
			conditionReason:  kcfgdataplane.DependenciesNotReadyReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(kcfgdataplane.ReadyType),
					Status:             metav1.ConditionFalse,
					Reason:             string(kcfgdataplane.DependenciesNotReadyReason),
					Message:            "",
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "new condition needs to be set on object without conditions",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "Resource is available",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(kcfgdataplane.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
					Message:            "Resource is available",
					ObservedGeneration: 1,
				},
			},
		},
		{
			name: "conflict triggers requeue",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "Resource is available",
			expectedResult: ctrl.Result{
				Requeue: true,
			},
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					return &k8serrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonConflict,
						},
					}
				},
			},
		},
		{
			name: "error",
			obj: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dp1",
					Generation: 1,
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "Resource is available",
			expectedError:    true,
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					return &k8serrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReason("unknown"),
						},
					}
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			cl := fake.NewClientBuilder().
				WithObjects(tt.obj).
				WithStatusSubresource(tt.obj).
				WithScheme(scheme.Get()).
				WithInterceptorFuncs(tt.interceptorFunc).
				Build()

			result, err := StatusWithCondition(
				ctx, cl, tt.obj,
				tt.conditionType, tt.conditionStatus, tt.conditionReason, tt.conditionMessage,
			)

			assert.Equal(t, tt.expectedResult, result)
			if tt.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			for _, expectedCond := range tt.expectedConditions {
				actualCond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(expectedCond.Type), tt.obj)
				if !ok {
					t.Fatalf("condition %s not found", expectedCond.Type)
				}
				assert.Equal(t, expectedCond.Status, actualCond.Status)
				assert.Equal(t, expectedCond.Reason, actualCond.Reason)
				assert.Equal(t, expectedCond.Message, actualCond.Message)
				assert.Equal(t, expectedCond.ObservedGeneration, actualCond.ObservedGeneration)
			}
		})
	}
}
