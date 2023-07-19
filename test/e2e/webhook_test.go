//go:build e2e_tests

package e2e

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestDataplaneValidatingWebhook(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// createEnvironment will queue up environment cleanup if necessary
	// and dumping diagnostics if the test fails.
	e := createEnvironment(t, ctx)
	clients, testNamespace := e.Clients, e.Namespace

	testCases := []struct {
		name      string
		dataplane *operatorv1beta1.DataPlane
		errMsg    string
	}{
		{
			name: "validating_error",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace.Name,
					Name:      uuid.NewString(),
				},
			},
			errMsg: "DataPlane requires an image",
		},
		{
			name: "database_postgres_not_supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_DATABASE",
														Value: "postgres",
													},
												},
												Image: consts.DefaultDataPlaneImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},

			errMsg: "database backend postgres of DataPlane not supported currently",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataplaneClient := clients.OperatorClient.ApisV1beta1().DataPlanes(testNamespace.Name)
			_, err := dataplaneClient.Create(ctx, tc.dataplane, metav1.CreateOptions{})
			if tc.errMsg == "" {
				require.NoErrorf(t, err, "test case %s: should not return error when creating dataplane", tc.name)
			} else {
				require.Error(t, err, "test case %s: should return error", tc.name)
				require.Containsf(t, err.Error(), tc.errMsg, "test case %s: error message should contain expected content", tc.name)
			}
		})
	}
}
