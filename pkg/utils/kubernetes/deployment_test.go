package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentRolloutComplete(t *testing.T) {
	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		want       bool
	}{
		{
			name: "fully rolled out",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    2,
					AvailableReplicas:  2,
				},
			},
			want: true,
		},
		{
			name: "stale observed generation",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 3},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    2,
					AvailableReplicas:  2,
				},
			},
			want: false,
		},
		{
			name: "rollout in progress: old replica still available, new one not yet",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					Replicas:           2,
					UpdatedReplicas:    1,
					AvailableReplicas:  2,
				},
			},
			want: false,
		},
		{
			name: "rollout in progress: not all replicas available yet",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1,
					Replicas:           2,
					UpdatedReplicas:    2,
					AvailableReplicas:  1,
				},
			},
			want: false,
		},
		{
			name: "nil spec.replicas defaults to 1, fully rolled out",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: nil},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1,
					Replicas:           1,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			want: true,
		},
		{
			name:       "zero-value deployment is not rolled out",
			deployment: &appsv1.Deployment{},
			want:       false,
		},
		{
			name: "explicit spec.replicas=0: not complete even though every counter matches",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(0))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1,
					Replicas:           0,
					UpdatedReplicas:    0,
					AvailableReplicas:  0,
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeploymentRolloutComplete(tc.deployment))
		})
	}
}
