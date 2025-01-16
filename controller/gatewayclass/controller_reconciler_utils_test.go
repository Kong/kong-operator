package gatewayclass

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

func TestGetAcceptedCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, gatewayv1.Install(scheme))
	assert.NoError(t, operatorv1beta1.AddToScheme(scheme))

	tests := []struct {
		name           string
		gwc            *gatewayv1.GatewayClass
		existingObjs   []runtime.Object
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedMsg    string
	}{
		{
			name: "ParametersRef is nil",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: nil,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: string(gatewayv1.GatewayClassReasonAccepted),
			expectedMsg:    "GatewayClass is accepted",
		},
		{
			name: "Invalid ParametersRef Group and kind",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{

					ParametersRef: &gatewayv1.ParametersReference{
						Group:     "invalid.group",
						Kind:      "InvalidKind",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
						Name:      "invalid",
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "ParametersRef must reference a gateway-operator.konghq.com/GatewayConfiguration",
		},
		{
			name: "ParametersRef Namespace is nil",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group: gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:  "GatewayConfiguration",
						Name:  "no-namespace",
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "ParametersRef must reference a namespaced resource",
		},
		{
			name: "GatewayConfiguration does not exist",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:      "GatewayConfiguration",
						Name:      "nonexistent",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "The referenced GatewayConfiguration does not exist",
		},
		{
			name: "Valid ParametersRef",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:      "GatewayConfiguration",
						Name:      "valid-config",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
					},
				},
			},
			existingObjs: []runtime.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-config",
						Namespace: "default",
					},
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: string(gatewayv1.GatewayClassReasonAccepted),
			expectedMsg:    "GatewayClass is accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			condition, err := getAcceptedCondition(ctx, cl, tt.gwc)
			assert.NoError(t, err)
			assert.NotNil(t, condition)
			assert.Equal(t, tt.expectedStatus, condition.Status)
			assert.Equal(t, tt.expectedReason, condition.Reason)
			assert.Equal(t, tt.expectedMsg, condition.Message)
		})
	}
}
