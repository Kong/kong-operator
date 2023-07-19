//go:build integration_tests

package integration

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/consts"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataPlaneValidation(t *testing.T) {
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
		dataplane        *operatorv1beta1.DataPlane
		validatingOK     bool
		conditionMessage string
	}{
		{
			name: "reconciler:validating_error_with_empty_deplyoptions",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
			},
			validatingOK:     false,
			conditionMessage: "DataPlane requires an image",
		},

		{
			name: "reconciler:database_postgres_not_supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_DATABASE",
														Value: "postgres",
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
			},
			validatingOK:     false,
			conditionMessage: "database backend postgres of DataPlane not supported currently",
		},

		{
			name: "reconciler:database_xxx_not_supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_DATABASE",
														Value: "xxx",
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
			},
			validatingOK:     false,
			conditionMessage: "database backend xxx of DataPlane not supported currently",
		},
		{
			name: "reconciler:validator_ok_with_db=off_from_configmap",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
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
					},
				},
			},
			validatingOK: true,
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataplane, err := dataplaneClient.Create(ctx, tc.dataplane, metav1.CreateOptions{})
			require.NoErrorf(t, err, "should not return error when create dataplane for case %s", tc.name)

			if tc.validatingOK {
				t.Logf("%s: verifying deployments managed by the dataplane", t.Name())
				w, err := clients.K8sClient.AppsV1().Deployments(namespace.Name).Watch(ctx, metav1.ListOptions{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					LabelSelector: fmt.Sprintf("%s=%s", consts.GatewayOperatorControlledLabel, consts.DataPlaneManagedLabelValue),
				})
				require.NoError(t, err)
				t.Cleanup(func() { w.Stop() })
				for {
					select {
					case <-ctx.Done():
						t.Fatalf("context expired: %v", ctx.Err())
					case event := <-w.ResultChan():
						deployment, ok := event.Object.(*appsv1.Deployment)
						require.True(t, ok)
						if deployment.Status.AvailableReplicas < deployment.Status.ReadyReplicas {
							continue
						}
						if !lo.ContainsBy(deployment.OwnerReferences, func(or metav1.OwnerReference) bool {
							return or.UID == dataplane.UID
						}) {
							continue
						}

						return
					}
				}
			} else {
				t.Logf("%s: verifying DataPlane conditions", t.Name())
				w, err := dataplaneClient.Watch(ctx, metav1.ListOptions{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DataPlane",
						APIVersion: operatorv1alpha1.SchemeGroupVersion.String(),
					},
					FieldSelector: "metadata.name=" + tc.dataplane.Name,
				})
				require.NoError(t, err)
				t.Cleanup(func() { w.Stop() })
				for {
					select {
					case <-ctx.Done():
						t.Fatalf("context expired: %v", ctx.Err())
					case event := <-w.ResultChan():
						dataplane, ok := event.Object.(*operatorv1beta1.DataPlane)
						require.True(t, ok)

						var provisionCondition metav1.Condition
						for _, condition := range dataplane.Status.Conditions {
							if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) {
								provisionCondition = condition
								break
							}
						}
						t.Log("verifying conditions of invalid dataplanes")
						if provisionCondition.Status != metav1.ConditionFalse {
							t.Logf("provision condition status should be false")
							continue
						}
						if provisionCondition.Message != tc.conditionMessage {
							t.Logf("provision condition message should be the same as expected")
							continue
						}

						return
					}
				}
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
		dataplane *operatorv1beta1.DataPlane
		// empty if expect no error,
		errMsg string
	}{
		{
			name: "webhook:validating_ok",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
			},
			errMsg: "",
		},
		{
			name: "webhook:database_postgres_not_supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_DATABASE",
														Value: "postgres",
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
			},
			errMsg: "database backend postgres of DataPlane not supported currently",
		},
		{
			name: "webhook:database_xxx_not_supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_DATABASE",
														Value: "xxx",
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
			},
			errMsg: "database backend xxx of DataPlane not supported currently",
		},
		{
			name: "webhook:validator_ok_with_db=off_from_configmap",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
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
					},
				},
			},
			errMsg: "",
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)
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
