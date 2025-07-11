package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

func TestSpecHash(t *testing.T) {
	tests := []struct {
		name    string
		opts    operatorv1beta1.DataPlaneSpec
		want    string
		wantErr bool
	}{
		{
			name:    "empty spec",
			opts:    operatorv1beta1.DataPlaneSpec{},
			want:    "1178898102b729b4",
			wantErr: false,
		},
		{
			name: "with podTemplateSpec",
			opts: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  "proxy",
											Image: "kong:3.9",
										},
									},
								},
							},
						},
					},
				},
			},
			want:    "966730c910ab53b",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := appsv1.Deployment{}
			err := AnnotateObjWithHash(&deployment, tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, deployment.Annotations[consts.AnnotationSpecHash])

			// Running twice yields the same result
			err = AnnotateObjWithHash(&deployment, tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, deployment.Annotations[consts.AnnotationSpecHash])
		})
	}
}
