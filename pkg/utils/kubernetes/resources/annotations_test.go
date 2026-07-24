package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
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
			want:    "12bd99784adaef8c",
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
			want:    "2ffb680af68bda21",
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

// TestSpecHashIgnoresServiceOnlyTrafficFields ensures that the Service-only
// ServiceOptions fields tagged with hash:"ignore" (TrafficDistribution,
// InternalTrafficPolicy) do not influence the DataPlane Deployment spec-hash.
// These fields configure only the ingress Service, so setting them must not
// change the hash - otherwise existing Deployments are spuriously patched on
// operator upgrade (see PR #4627).
func TestSpecHashIgnoresServiceOnlyTrafficFields(t *testing.T) {
	// Base spec with the ingress ServiceOptions struct populated so that the
	// hashed fields are actually reached (a nil Network.Services short-circuits
	// before ServiceOptions is visited).
	base := operatorv1beta1.DataPlaneSpec{
		DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Network: operatorv1beta1.DataPlaneNetworkOptions{
				Services: &operatorv1beta1.DataPlaneServices{
					Ingress: &operatorv1beta1.DataPlaneServiceOptions{
						ServiceOptions: operatorv1beta1.ServiceOptions{
							Type: corev1.ServiceTypeLoadBalancer,
						},
					},
				},
			},
		},
	}

	withTrafficFields := base.DeepCopy()
	withTrafficFields.Network.Services.Ingress.TrafficDistribution = new("PreferSameZone")
	withTrafficFields.Network.Services.Ingress.InternalTrafficPolicy = new(corev1.ServiceInternalTrafficPolicyLocal)

	baseHash, err := CalculateHash(base)
	require.NoError(t, err)
	withHash, err := CalculateHash(*withTrafficFields)
	require.NoError(t, err)

	assert.Equal(t, baseHash, withHash,
		"TrafficDistribution and InternalTrafficPolicy must be excluded from the DataPlane spec-hash")

	// Sanity check: a non-ignored Service field (ExternalTrafficPolicy) DOES
	// change the hash, proving the comparison above is meaningful.
	withExternal := base.DeepCopy()
	withExternal.Network.Services.Ingress.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
	externalHash, err := CalculateHash(*withExternal)
	require.NoError(t, err)
	assert.NotEqual(t, baseHash, externalHash,
		"ExternalTrafficPolicy is part of the spec-hash baseline and must affect the hash")
}
