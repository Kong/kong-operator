package patch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
				Name:       "dp1",
				Generation: 1,
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
				Name:       "dp1",
				Generation: 1,
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
				Name:       "dp1",
				Generation: 2,
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
				Name:       "dp1",
				Generation: 1,
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
				Name:       "dp1",
				Generation: 1,
				Status:     operatorv1beta1.DataPlaneStatus{},
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
				Name:       "dp1",
				Generation: 1,
				Status:     operatorv1beta1.DataPlaneStatus{},
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
					return &apierrors.StatusError{
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
				Name:       "dp1",
				Generation: 1,
				Status:     operatorv1beta1.DataPlaneStatus{},
			},
			conditionType:    kcfgdataplane.ReadyType,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  kcfgkonnect.KonnectExtensionAppliedReason,
			conditionMessage: "Resource is available",
			expectedError:    true,
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					return &apierrors.StatusError{
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

func TestPatchStatusWithoutCondition(t *testing.T) {
	const removedConditionType = kcfgdataplane.ReadyType

	otherCondition := metav1.Condition{
		Type:               "OtherCondition",
		Status:             metav1.ConditionTrue,
		Reason:             string(kcfgkonnect.KonnectExtensionAppliedReason),
		Message:            "unrelated condition that must be preserved",
		ObservedGeneration: 1,
	}
	removedCondition := metav1.Condition{
		Type:               string(removedConditionType),
		Status:             metav1.ConditionFalse,
		Reason:             string(kcfgdataplane.DependenciesNotReadyReason),
		Message:            "stale condition that must be removed",
		ObservedGeneration: 1,
	}

	newDataPlane := func(conditions ...metav1.Condition) *operatorv1beta1.DataPlane {
		return &operatorv1beta1.DataPlane{
			Name:       "dp1",
			Generation: 1,
			Status: operatorv1beta1.DataPlaneStatus{
				Conditions: conditions,
			},
		}
	}

	tests := []struct {
		name string
		obj  *operatorv1beta1.DataPlane
		// removedAbsent asserts the condition is gone after the call.
		removedAbsent bool
		// remainingTypes lists condition types that must still be present after the call.
		remainingTypes  []string
		expectedResult  ctrl.Result
		expectedError   bool
		interceptorFunc interceptor.Funcs
	}{
		{
			name:           "condition present is removed and other conditions are preserved",
			obj:            newDataPlane(otherCondition, removedCondition),
			removedAbsent:  true,
			remainingTypes: []string{otherCondition.Type},
			expectedResult: ctrl.Result{},
		},
		{
			name:           "condition absent is a no-op without error",
			obj:            newDataPlane(otherCondition),
			removedAbsent:  true,
			remainingTypes: []string{otherCondition.Type},
			expectedResult: ctrl.Result{},
		},
		{
			name:           "conflict triggers requeue",
			obj:            newDataPlane(removedCondition),
			expectedResult: ctrl.Result{Requeue: true},
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					return &apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonConflict,
						},
					}
				},
			},
		},
		{
			name:          "other error is returned",
			obj:           newDataPlane(removedCondition),
			expectedError: true,
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					return &apierrors.StatusError{
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

			result, err := StatusWithoutCondition(
				ctx, cl, tt.obj, string(removedConditionType),
			)

			assert.Equal(t, tt.expectedResult, result)
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Re-read from the client (not the in-memory object) so the test
			// catches the empty-patch no-op bug rather than just the in-memory mutation.
			persisted := &operatorv1beta1.DataPlane{}
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(tt.obj), persisted))

			if tt.removedAbsent {
				_, ok := k8sutils.GetCondition(removedConditionType, persisted)
				assert.Falsef(t, ok, "condition %s should have been removed", removedConditionType)
			}
			for _, condType := range tt.remainingTypes {
				_, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(condType), persisted)
				assert.Truef(t, ok, "condition %s should have been preserved", condType)
			}
		})
	}
}

// TestStatusWithConditionStaleCacheDoesNotClobberNewerConditions verifies that a
// condition patch built from a stale cached object does not silently drop
// conditions persisted by more recent writes (e.g. Programmed set right after a
// create). A JSON merge patch replaces the whole conditions array, so the patch
// carries the optimistic-lock resourceVersion and conflicts against the newer
// server state; reconcilers handle that conflict by requeueing on a fresh cache.
func TestStatusWithConditionStaleCacheDoesNotClobberNewerConditions(t *testing.T) {
	const (
		liveRV  = "10"
		staleRV = "9"
	)

	// Live state on the "server": Programmed was persisted by a more recent
	// write than the one the stale cached copy observed.
	live := &operatorv1beta1.DataPlane{
		Name:            "dp1",
		ResourceVersion: liveRV,
		Generation:      1,
		Status: operatorv1beta1.DataPlaneStatus{
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed),
					ObservedGeneration: 1,
				},
			},
		},
	}

	// Stale cached copy: older resourceVersion, no Programmed condition.
	stale := live.DeepCopy()
	stale.ResourceVersion = staleRV
	stale.Status.Conditions = nil

	var conflictSeen bool
	cl := fake.NewClientBuilder().
		WithObjects(live).
		WithStatusSubresource(live).
		WithScheme(scheme.Get()).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context, cl client.Client, _ string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption,
			) error {
				data, err := patch.Data(obj)
				if err != nil {
					return err
				}
				var patchData struct {
					Metadata struct {
						ResourceVersion string `json:"resourceVersion"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal(data, &patchData); err != nil {
					return err
				}
				if patchData.Metadata.ResourceVersion == "" {
					t.Error("expected the patch to carry metadata.resourceVersion (optimistic lock)")
				}

				// Simulate the API server: reject the patch when its
				// resourceVersion does not match the live object.
				liveObj := &operatorv1beta1.DataPlane{}
				if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), liveObj); err != nil {
					return err
				}
				if patchData.Metadata.ResourceVersion != liveObj.ResourceVersion {
					conflictSeen = true
					return &apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonConflict,
						},
					}
				}
				return cl.Status().Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	result, err := StatusWithCondition(
		t.Context(), cl, stale,
		kcfgdataplane.ReadyType, metav1.ConditionTrue,
		kcfgkonnect.KonnectExtensionAppliedReason, "Resource is available",
	)

	// The stale patch must conflict, and the conflict must surface as a requeue
	// (the reconcilers' existing conflict handling) rather than a silent clobber.
	require.NoError(t, err)
	assert.True(t, conflictSeen, "expected the stale patch to conflict on resourceVersion")
	assert.Equal(t, ctrl.Result{Requeue: true}, result)

	// The condition persisted by the more recent write must survive.
	persisted := &operatorv1beta1.DataPlane{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(live), persisted))
	_, ok := k8sutils.GetCondition(
		kcfgconsts.ConditionType(konnectv1alpha1.KonnectEntityProgrammedConditionType), persisted,
	)
	assert.True(t, ok, "Programmed condition should not have been dropped by the stale patch")
}
