package route

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestHTTPRouteStatusUpdater(t *testing.T) {
	testCases := []struct {
		name                  string
		route                 gwtypes.HTTPRoute
		setupSharedStatus     func(*SharedRouteStatusMap)
		expectedConditions    map[string]expectedCondition
		expectedEnforceResult op.Result
	}{
		{
			name: "successful backends programmed with multiple gateways",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "gateway-1"},
							{Name: "gateway-2", Namespace: lo.ToPtr(gwtypes.Namespace("custom-namespace"))},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name: "service-1",
										},
									},
								},
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name:      "service-2",
											Namespace: lo.ToPtr(gwtypes.Namespace("service-namespace")),
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key1 := StatusMapKey(HTTPRouteKey, "test-namespace/test-route", "test-namespace/gateway-1")
				key2 := StatusMapKey(HTTPRouteKey, "test-namespace/test-route", "custom-namespace/gateway-2")

				statusMap.SharedStatus[key1] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/service-1":    {ServiceControllerInit: true, ProgrammedBackends: 1},
						"service-namespace/service-2": {ServiceControllerInit: true, ProgrammedBackends: 1},
					},
				}
				statusMap.SharedStatus[key2] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/service-1":    {ServiceControllerInit: true, ProgrammedBackends: 1},
						"service-namespace/service-2": {ServiceControllerInit: true, ProgrammedBackends: 1},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/gateway-1": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionTrue,
					conditionReason: ConditionReasonBackendsProgrammed,
				},
				"custom-namespace/gateway-2": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionTrue,
					conditionReason: ConditionReasonBackendsProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
		{
			name: "ParentRefs with empty (nil) namespace",
			route: func() gwtypes.HTTPRoute {
				route := gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "panic-test-route",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: "test-gateway"}, // This has nil Namespace.
							},
						},
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
											},
										},
									},
								},
							},
						},
					},
				}
				someNamespace := gwtypes.Namespace("some-namespace")
				route.Status.Parents = []gwtypes.RouteParentStatus{
					{
						ParentRef: gwtypes.ParentReference{
							Name:      "test-gateway",
							Namespace: &someNamespace, // Non-nil namespace.
						},
						ControllerName: gwtypes.GatewayController("kong.konghq.com/kong-operator"),
						Conditions:     []metav1.Condition{},
					},
				}
				return route
			}(),
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/panic-test-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/test-service": {ServiceControllerInit: true, ProgrammedBackends: 1},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/test-gateway": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionTrue,
					conditionReason: ConditionReasonBackendsProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
		{
			name: "backends not programmed failure",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failing-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "test-gateway"},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name: "failing-service",
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/failing-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/failing-service": {ServiceControllerInit: true, ProgrammedBackends: 0},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/test-gateway": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionFalse,
					conditionReason: ConditionReasonBackendsNotProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
		{
			name: "service not initiated edge case",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "uninitiated-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "test-gateway"},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name: "uninitiated-service",
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/uninitiated-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/uninitiated-service": {ServiceControllerInit: false, ProgrammedBackends: 0},
					},
				}
			},
			expectedConditions:    map[string]expectedCondition{},
			expectedEnforceResult: op.Noop,
		},
		{
			name: "backend with custom namespace (compute only)",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "test-gateway"},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name:      "test-service",
											Namespace: lo.ToPtr(gwtypes.Namespace("custom-namespace")),
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/test-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"custom-namespace/test-service": {
							ServiceControllerInit: true,
							ProgrammedBackends:    1,
						},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/test-gateway": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionTrue,
					conditionReason: ConditionReasonBackendsProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
		{
			name: "all backends programmed (compute only)",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "test-gateway"},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name: "test-service",
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/test-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/test-service": {
							ServiceControllerInit: true,
							ProgrammedBackends:    1,
						},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/test-gateway": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionTrue,
					conditionReason: ConditionReasonBackendsProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
		{
			name: "backends not programmed (compute only)",
			route: gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "test-gateway"},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									BackendRef: gwtypes.BackendRef{
										BackendObjectReference: gwtypes.BackendObjectReference{
											Name: "test-service",
										},
									},
								},
							},
						},
					},
				},
			},
			setupSharedStatus: func(statusMap *SharedRouteStatusMap) {
				key := StatusMapKey(HTTPRouteKey, "test-namespace/test-route", "test-namespace/test-gateway")
				statusMap.SharedStatus[key] = SharedRouteStatus{
					Services: map[string]ServiceControllerStatus{
						"test-namespace/test-service": {
							ServiceControllerInit: true,
							ProgrammedBackends:    0,
						},
					},
				}
			},
			expectedConditions: map[string]expectedCondition{
				"test-namespace/test-gateway": {
					conditionType:   ConditionTypeBackendsProgrammed,
					conditionStatus: metav1.ConditionFalse,
					conditionReason: ConditionReasonBackendsNotProgrammed,
				},
			},
			expectedEnforceResult: op.Updated,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithObjects(&tc.route).
				WithScheme(scheme.Get()).
				WithStatusSubresource(&tc.route).
				Build()
			logger := logr.Discard()
			sharedStatusMap := NewSharedStatusMap()
			tc.setupSharedStatus(sharedStatusMap)

			updater := newHTTPRouteStatusUpdater(tc.route, client, logger, sharedStatusMap)
			updater.ComputeStatus()

			require.Len(t, updater.parentProgrammedConditions, len(tc.expectedConditions), "Number of gateway conditions should match expected")
			for gatewayKey, expectedCond := range tc.expectedConditions {
				conditions, exists := updater.parentProgrammedConditions[gatewayKey]
				require.True(t, exists, "Expected conditions for gateway %s", gatewayKey)
				require.Len(t, conditions, 1, "Expected exactly one condition for gateway %s", gatewayKey)

				condition := conditions[0]
				require.Equal(t, expectedCond.conditionType, condition.Type, "Condition type mismatch for gateway %s", gatewayKey)
				require.Equal(t, expectedCond.conditionStatus, condition.Status, "Condition status mismatch for gateway %s", gatewayKey)
				require.Equal(t, expectedCond.conditionReason, condition.Reason, "Condition reason mismatch for gateway %s", gatewayKey)
			}

			result, err := updater.EnforceStatus(context.Background())

			require.NoError(t, err, "EnforceStatus should not return an error")
			require.Equal(t, tc.expectedEnforceResult, result, "EnforceStatus result should match expected")
		})
	}
}

// expectedCondition represents the expected condition values for testing.
type expectedCondition struct {
	conditionType   string
	conditionStatus metav1.ConditionStatus
	conditionReason string
}
