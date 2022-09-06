//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
)

func TestDataplaneValidatingWebhook(t *testing.T) {
	var env environments.Environment
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer require.NoError(t, cleanupEnvironment(ctx, env))

	var clients *testutils.K8sClients
	env, clients = createEnvironment(t, ctx)

	testNamespace, cleaner := setup(t, ctx, env)
	defer func() {
		if t.Failed() {
			output, err := cleaner.DumpDiagnostics(ctx, t.Name())
			t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
			assert.NoError(t, err)
		}
		assert.NoError(t, cleaner.Cleanup(ctx))
	}()

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
			dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(testNamespace.Name)
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
