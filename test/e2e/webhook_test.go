//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

func TestDataplaneValidatingWebhook(t *testing.T) {
	t.Log("start tests")
	testNamespace, cleanup := createNamespaceForTest(t)
	defer cleanup()

	testCases := []struct {
		name      string
		dataplane *operatorv1alpha1.DataPlane
		errMsg    string
	}{
		{
			name: "validating_ok",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace.Name,
					Name:      uuid.NewString(),
				},
			},
			errMsg: "",
		},
		{
			name: "database_postgres_not_supported",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{Name: "KONG_DATABASE", Value: "postgres"},
							},
						},
					},
				},
			},

			errMsg: "database backend postgres of dataplane not supported currently",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataplaneClient := operatorClient.ApisV1alpha1().DataPlanes(testNamespace.Name)
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
