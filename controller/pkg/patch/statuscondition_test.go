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

	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestPatchStatusWithCondition(t *testing.T) {
	tests := []struct {
		name string
		obj  interface {
			client.Object
			GetConditions() []metav1.Condition
			SetConditions([]metav1.Condition)
		}
		conditionType      consts.ConditionType
		conditionStatus    metav1.ConditionStatus
		conditionReason    consts.ConditionReason
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
							Type:               string(consts.ReadyType),
							Status:             metav1.ConditionTrue,
							Reason:             string(consts.ResolvedRefsReason),
							Message:            "Resource is available",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
			conditionMessage: "Resource is available",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(consts.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(consts.ResolvedRefsReason),
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
							Type:               string(consts.ReadyType),
							Status:             metav1.ConditionFalse,
							Reason:             string(consts.ResolvedRefsReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(consts.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(consts.ResolvedRefsReason),
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
							Type:               string(consts.ReadyType),
							Status:             metav1.ConditionTrue,
							Reason:             string(consts.ResolvedRefsReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(consts.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(consts.ResolvedRefsReason),
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
							Type:               string(consts.ReadyType),
							Status:             metav1.ConditionFalse,
							Reason:             string(consts.ResourceReadyReason),
							Message:            "",
							ObservedGeneration: 1,
						},
					},
				},
			},
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionFalse,
			conditionReason:  consts.DependenciesNotReadyReason,
			conditionMessage: "",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(consts.ReadyType),
					Status:             metav1.ConditionFalse,
					Reason:             string(consts.DependenciesNotReadyReason),
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
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
			conditionMessage: "Resource is available",
			expectedResult:   ctrl.Result{},
			expectedConditions: []metav1.Condition{
				{
					Type:               string(consts.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             string(consts.ResolvedRefsReason),
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
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
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
			conditionType:    consts.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  consts.ResolvedRefsReason,
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
			ctx := context.Background()
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
				actualCond, ok := k8sutils.GetCondition(consts.ConditionType(expectedCond.Type), tt.obj)
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
