package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func testAIGWDPOwner() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		APIVersion: aigatewayv1alpha1.SchemeGroupVersion.String(),
		Kind:       "AIGatewayDataPlane",
		Name:       "my-owner",
		Namespace:  "test-ns",
		UID:        types.UID("test-uid"),
	}
}

func TestGenerateHPA(t *testing.T) {
	t.Run("returns error when scaling is nil", func(t *testing.T) {
		_, err := GenerateHPA(testAIGWDPOwner(), nil, "my-deploy")
		require.Error(t, err)
	})

	t.Run("sets scaleTargetRef to the given deployment name", func(t *testing.T) {
		hpa, err := GenerateHPA(testAIGWDPOwner(), &HPAScalingSpec{MaxReplicas: 3}, "my-deploy")
		require.NoError(t, err)
		assert.Equal(t, "my-deploy", hpa.Spec.ScaleTargetRef.Name)
		assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
		assert.Equal(t, "apps/v1", hpa.Spec.ScaleTargetRef.APIVersion)
	})

	t.Run("name and namespace match owner", func(t *testing.T) {
		owner := testAIGWDPOwner()
		hpa, err := GenerateHPA(owner, &HPAScalingSpec{MaxReplicas: 3}, "my-deploy")
		require.NoError(t, err)
		assert.Equal(t, owner.Name, hpa.Name)
		assert.Equal(t, owner.Namespace, hpa.Namespace)
	})

	t.Run("propagates scaling fields", func(t *testing.T) {
		minReplicas := int32(2)
		scaling := &HPAScalingSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: new(int32(50)),
						},
					},
				},
			},
		}
		hpa, err := GenerateHPA(testAIGWDPOwner(), scaling, "my-deploy")
		require.NoError(t, err)
		assert.Equal(t, int32(2), *hpa.Spec.MinReplicas)
		assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)
		assert.Len(t, hpa.Spec.Metrics, 1)
	})

	t.Run("sets owner reference", func(t *testing.T) {
		owner := testAIGWDPOwner()
		hpa, err := GenerateHPA(owner, &HPAScalingSpec{MaxReplicas: 3}, "my-deploy")
		require.NoError(t, err)
		require.Len(t, hpa.OwnerReferences, 1)
		assert.Equal(t, owner.Name, hpa.OwnerReferences[0].Name)
		assert.Equal(t, owner.UID, hpa.OwnerReferences[0].UID)
	})

	t.Run("sets app label and managed-by label", func(t *testing.T) {
		owner := testAIGWDPOwner()
		hpa, err := GenerateHPA(owner, &HPAScalingSpec{MaxReplicas: 3}, "my-deploy")
		require.NoError(t, err)
		assert.Equal(t, owner.Name, hpa.Labels["app"])
		assert.Equal(t, consts.AIGatewayDataPlaneManagedByLabelValue, hpa.Labels[consts.GatewayOperatorManagedByLabel])
	})
}
