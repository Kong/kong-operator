package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers"
)

// TestDataPlanePortsValidatingAdmissionPolicy tests if the validating admission policy
// is indeed configured for integration tests suite. More specific tests for actual
// logic are in test/crdsvalidation/dataplane_validatingadmissionpolicy_test.go.
func TestDataPlanePortsValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	testCases := []struct {
		name            string
		dataPlane       *operatorv1beta1.DataPlane
		updateDataPlane func(*operatorv1beta1.DataPlane)
		errorContains   string
	}{
		{
			name: "DataPlane creation - KONG_PORT_MAPS missing port from ingress",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    namespace.Name,
					GenerateName: "dataplane-invalid-creation-test-",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: helpers.GetDefaultDataPlaneImage(),
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_PORT_MAPS",
														Value: "80:8000",
													},
												},
											},
										},
									},
								},
							},
						},
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									Ports: []operatorv1beta1.DataPlaneServicePort{
										{
											Name:       "http",
											Port:       80,
											TargetPort: intstr.FromInt(8000),
										},
										{
											Name:       "https",
											Port:       443,
											TargetPort: intstr.FromInt(8443),
										},
									},
								},
							},
						},
					},
				},
			},
			errorContains: "denied request: Each port from spec.network.services.ingress.ports has to have an accompanying port in KONG_PORT_MAPS env",
		},
		{
			name: "DataPlane update - adding mismatched port should fail",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    namespace.Name,
					GenerateName: "dataplane-invalid-update-test-",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: helpers.GetDefaultDataPlaneImage(),
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_PORT_MAPS",
														Value: "80:8000",
													},
												},
											},
										},
									},
								},
							},
						},
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									Ports: []operatorv1beta1.DataPlaneServicePort{
										{
											Name:       "http",
											Port:       80,
											TargetPort: intstr.FromInt(8000),
										},
									},
								},
							},
						},
					},
				},
			},
			// Add a mismatched port without updating KONG_PORT_MAPS.
			updateDataPlane: func(dp *operatorv1beta1.DataPlane) {
				dp.Spec.Network.Services.Ingress.Ports = append(
					dp.Spec.Network.Services.Ingress.Ports,
					operatorv1beta1.DataPlaneServicePort{
						Name:       "https",
						Port:       443,
						TargetPort: intstr.FromInt(8443),
					},
				)
			},
			errorContains: "denied request: Each port from spec.network.services.ingress.ports has to have an accompanying port in KONG_PORT_MAPS env",
		},
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tc.updateDataPlane != nil {
				var dpToUpdate *operatorv1beta1.DataPlane
				dpToUpdate, err = dataplaneClient.Create(GetCtx(), tc.dataPlane, metav1.CreateOptions{})
				require.NoError(t, err, "failed to create initial DataPlane")
				cleaner.Add(dpToUpdate)

				require.Eventually(t, func() bool {
					dpToUpdate, err = dataplaneClient.Get(GetCtx(), dpToUpdate.Name, metav1.GetOptions{})
					tc.updateDataPlane(dpToUpdate)
					_, err = dataplaneClient.Update(GetCtx(), dpToUpdate, metav1.UpdateOptions{})
					return !apierrors.IsConflict(err)
				}, 10*time.Second, 100*time.Millisecond)

			} else {
				_, err = dataplaneClient.Create(GetCtx(), tc.dataPlane, metav1.CreateOptions{})
			}
			require.Error(t, err, "expected error when submitting DataPlane")
			require.True(t, apierrors.IsInvalid(err), "error should be of type Invalid, got: %v", err)
			require.Contains(t, err.Error(), tc.errorContains, "error message should contain expected substring")
		})
	}
}
