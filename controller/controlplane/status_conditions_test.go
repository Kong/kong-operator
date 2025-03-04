package controlplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	kcfgcontrolplane "github.com/kong/kubernetes-configuration/api/gateway-operator/controlplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestMarkAsProvisioned(t *testing.T) {
	t.Run("controlplane", func(t *testing.T) {
		createControlPlane := func() *operatorv1beta1.ControlPlane {
			return &operatorv1beta1.ControlPlane{}
		}

		testCases := []struct {
			name              string
			controlplane      func() *operatorv1beta1.ControlPlane
			expectedCondition metav1.Condition
		}{
			{
				name: "ControlPlane gets a Provisioned Condition with Status True",
				controlplane: func() *operatorv1beta1.ControlPlane {
					return createControlPlane()
				},
				expectedCondition: metav1.Condition{
					Type:    string(kcfgcontrolplane.ConditionTypeProvisioned),
					Reason:  string(kcfgcontrolplane.ConditionReasonPodsReady),
					Message: "pods for all Deployments are ready",
					Status:  metav1.ConditionTrue,
				},
			},
			{
				name: "ControlPlane gets a Provisioned Condition with Status True and correct ObservedGeneration",
				controlplane: func() *operatorv1beta1.ControlPlane {
					cp := createControlPlane()
					cp.Generation = 3
					return cp
				},
				expectedCondition: metav1.Condition{
					Type:               string(kcfgcontrolplane.ConditionTypeProvisioned),
					Reason:             string(kcfgcontrolplane.ConditionReasonPodsReady),
					Message:            "pods for all Deployments are ready",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 3,
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dp := tc.controlplane()
				markAsProvisioned(dp)
				cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(tc.expectedCondition.Type), dp)
				require.True(t, ok)
				assert.Equal(t, cond.Reason, tc.expectedCondition.Reason)
				assert.Equal(t, cond.Status, tc.expectedCondition.Status)
				assert.Equal(t, cond.Message, tc.expectedCondition.Message)
				assert.Equal(t, cond.ObservedGeneration, tc.expectedCondition.ObservedGeneration)
			})
		}
	})
}
