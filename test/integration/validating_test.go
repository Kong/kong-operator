//go:build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataplaneValidation(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	// create a configmap containing "KONG_DATABASE" key for envFroms
	configMap, err := clients.K8sClient.CoreV1().ConfigMaps(namespace.Name).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dataplane-configs",
			Namespace: namespace.Name,
		},
		Data: map[string]string{
			"KONG_DATABASE": "db_1",
			"database1":     "off",
			"database2":     "db_2",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "failed to create configmap")
	cleaner.Add(configMap)

	if runWebhookTests {
		testDataplaneValidatingWebhook(t, namespace)
	} else {
		testDataplaneReconcileValidation(t, namespace)
	}
}

// could only run one of webhook validation or validation in reconciling.
func testDataplaneReconcileValidation(t *testing.T, namespace *corev1.Namespace) {
	if runWebhookTests {
		t.Skip("run validating webhook tests instead of validating in reconciling")
	}

	testCases := []struct {
		name             string
		dataplane        *operatorv1alpha1.DataPlane
		validatingOK     bool
		conditionMessage string
	}{
		{
			name: "reconciler:validating_error_with_empty_deplyoptions",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
			},
			validatingOK:     false,
			conditionMessage: "DataPlanes requires a containerImage",
		},
		{
			name: "reconciler:database_postgres_not_supported",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{Name: "KONG_DATABASE", Value: "postgres"},
									},
									ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
									Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
								},
							},
						},
					},
				},
			},
			validatingOK:     false,
			conditionMessage: "database backend postgres of dataplane not supported currently",
		},

		{
			name: "reconciler:database_xxx_not_supported",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{Name: "KONG_DATABASE", Value: "xxx"},
									},
									ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
									Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
								},
							},
						},
					},
				},
			},
			validatingOK:     false,
			conditionMessage: "database backend xxx of dataplane not supported currently",
		},
		{
			name: "reconciler:validator_ok_with_db=off_from_configmap",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{
											Name: "KONG_DATABASE",
											ValueFrom: &corev1.EnvVarSource{
												ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{Name: "dataplane-configs"},
													Key:                  "database1",
												},
											},
										},
									},
									ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
									Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
								},
							},
						},
					},
				},
			},
			validatingOK: true,
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataplane, err := dataplaneClient.Create(ctx, tc.dataplane, metav1.CreateOptions{})
			require.NoErrorf(t, err, "should not return error when create dataplane for case %s", tc.name)
			require.Eventually(t, func() bool {
				dataplane, err = clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name).Get(ctx, dataplane.Name, metav1.GetOptions{})
				require.NoError(t, err)
				isScheduled := false
				for _, condition := range dataplane.Status.Conditions {
					if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) {
						isScheduled = true
					}
				}
				return isScheduled
			}, time.Minute, time.Second)

			var provisionCondition metav1.Condition
			for _, condition := range dataplane.Status.Conditions {
				if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) {
					provisionCondition = condition
					break
				}
			}

			if tc.validatingOK {
				t.Log("verifying deployments managed by the dataplane")
				require.Eventually(t, func() bool {
					deployments, err := k8sutils.ListDeploymentsForOwner(
						ctx,
						clients.MgrClient,
						dataplane.Namespace,
						dataplane.UID,
						client.MatchingLabels{
							consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
						},
					)
					require.NoError(t, err)
					return len(deployments) == 1 && deployments[0].Status.AvailableReplicas >= deployments[0].Status.ReadyReplicas
				}, time.Minute, time.Second)

			} else {
				t.Log("verifying conditions of invalid dataplanes")
				require.Equalf(t, metav1.ConditionFalse, provisionCondition.Status,
					"provision condition status should be false in case %s", tc.name)
				require.Equalf(t, tc.conditionMessage, provisionCondition.Message,
					"provision condition message should be the same as expected in case %s", tc.name)
			}
		})
	}
}

func testDataplaneValidatingWebhook(t *testing.T, namespace *corev1.Namespace) {
	if !runWebhookTests {
		t.Skip("skip running webhook tests")
	}

	testCases := []struct {
		name      string
		dataplane *operatorv1alpha1.DataPlane
		// empty if expect no error,
		errMsg string
	}{
		{
			name: "webhook:validating_ok",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
			},
			errMsg: "",
		},
		{
			name: "webhook:database_postgres_not_supported",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{Name: "KONG_DATABASE", Value: "postgres"},
									},
								},
							},
						},
					},
				},
			},
			errMsg: "database backend postgres of dataplane not supported currently",
		},
		{
			name: "webhook:database_xxx_not_supported",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{Name: "KONG_DATABASE", Value: "xxx"},
									},
								},
							},
						},
					},
				},
			},
			errMsg: "database backend xxx of dataplane not supported currently",
		},
		{
			name: "webhook:validator_ok_with_db=off_from_configmap",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1alpha1.DeploymentOptions{
								Pods: operatorv1alpha1.PodsOptions{
									Env: []corev1.EnvVar{
										{
											Name: "KONG_DATABASE",
											ValueFrom: &corev1.EnvVarSource{
												ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{Name: "dataplane-configs"},
													Key:                  "database1",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			errMsg: "",
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := dataplaneClient.Create(ctx, tc.dataplane, metav1.CreateOptions{})
			if tc.errMsg == "" {
				require.NoErrorf(t, err, "test case %s: should not return error", tc.name)
			} else {
				require.Containsf(t, err.Error(), tc.errMsg,
					"test case %s: error message should contain expected content", tc.name)
			}
		})
	}
}
