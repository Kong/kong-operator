package dataplane

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestValidateDeployOptions(t *testing.T) {
	defaultObjects := func() []client.Object {
		return []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm"},
				Data: map[string]string{
					"off": "off",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret"},
				// fake client does not encode fields in StringData to Data,
				// so here we should usebase64 encoded value in Data.
				Data: map[string][]byte{
					"postgres": []byte(base64.StdEncoding.EncodeToString([]byte("postgres"))),
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm-2"},
				// fake client does not encode fields in StringData to Data,
				// so here we should usebase64 encoded value in Data.
				Data: map[string]string{
					"KONG_DATABASE": "xxx",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret-2"},
				// fake client does not encode fields in StringData to Data,
				// so here we should usebase64 encoded value in Data.
				Data: map[string][]byte{
					"DATABASE": []byte(base64.StdEncoding.EncodeToString([]byte("xxx"))),
				},
			},
		}
	}

	b := fakeclient.NewClientBuilder()
	b.WithObjects(defaultObjects()...)

	testCases := []struct {
		msg       string
		dataplane *operatorv1beta1.DataPlane
		hasError  bool
		errMsg    string
	}{
		{
			msg: "dataplane with dbmode=off should be valid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off",
					Namespace: "default",
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
														Name:  consts.EnvVarKongDatabase,
														Value: "off",
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
			hasError: false,
		},
		{
			msg: "dataplane with empty dbmode should be valid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off",
					Namespace: "default",
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
														Name:  consts.EnvVarKongDatabase,
														Value: "",
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
			hasError: false,
		},
		{
			msg: "dataplane with dbmode=postgres should be invalid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres",
					Namespace: "default",
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
														Name:  consts.EnvVarKongDatabase,
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
			hasError: true,
			errMsg:   "database backend postgres of DataPlane not supported currently",
		},
		{
			msg: "dataplane with arbitrary dbmode should be invalid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres",
					Namespace: "default",
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
														Name:  consts.EnvVarKongDatabase,
														Value: "xxx",
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
			hasError: true,
			errMsg:   "database backend xxx of DataPlane not supported currently",
		},
		{
			msg: "dataplane with dbmode=off (from configmap) should be valid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-cm",
					Namespace: "default",
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
														Name: consts.EnvVarKongDatabase,
														ValueFrom: &corev1.EnvVarSource{
															ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
																LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm"},
																Key:                  "off",
															},
														},
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
			hasError: false,
		},
		{
			msg: "dataplane with dbmode=postgres (from secret) should be invalid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres-in-secret",
					Namespace: "default",
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
														Name: consts.EnvVarKongDatabase,
														ValueFrom: &corev1.EnvVarSource{
															SecretKeyRef: &corev1.SecretKeySelector{
																LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
																Key:                  "postgres",
															},
														},
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
			hasError: true,
			errMsg:   "database backend postgres of DataPlane not supported currently",
		},
		{
			msg: "dataplane with dbmode=xxx (from configmap in envFrom) should be invalid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-cm",
					Namespace: "default",
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
												EnvFrom: []corev1.EnvFromSource{
													{
														Prefix: "",
														ConfigMapRef: &corev1.ConfigMapEnvSource{
															LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm-2"},
														},
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
			hasError: true,
			errMsg:   "database backend xxx of DataPlane not supported currently",
		},
		{
			msg: "dataplane with dbmode=xxx (from secret in envFrom) should be invalid",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-secret",
					Namespace: "default",
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
												EnvFrom: []corev1.EnvFromSource{
													{
														Prefix: "KONG_",
														SecretRef: &corev1.SecretEnvSource{
															LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret-2"},
														},
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
			hasError: true,
			errMsg:   "database backend xxx of DataPlane not supported currently",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.msg, func(t *testing.T) {
			v := &Validator{
				c: b.Build(),
			}
			err := v.Validate(tc.dataplane)
			if !tc.hasError {
				require.NoError(t, err, tc.msg)
			} else {
				require.EqualError(t, err, tc.errMsg, tc.msg)
			}
		})
	}
}

func TestValidateUpdate(t *testing.T) {
	b := fakeclient.NewClientBuilder()

	oldOptions := &operatorv1beta1.DataPlaneOptions{
		Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
			DeploymentOptions: operatorv1beta1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: consts.DataPlaneProxyContainerName,
								Env: []corev1.EnvVar{
									{
										Name:  consts.EnvVarKongDatabase,
										Value: "off",
									},
								},
								Image: consts.DefaultDataPlaneImage,
							},
						},
					},
				},
			},
		},
	}
	options := oldOptions.DeepCopy()
	options.Deployment.PodTemplateSpec.Spec.Containers = append(options.Deployment.PodTemplateSpec.Spec.Containers,
		corev1.Container{
			Name:  "test-container",
			Image: "test-image",
		},
	)

	testCases := []struct {
		msg          string
		dataplane    *operatorv1beta1.DataPlane
		oldDataplane *operatorv1beta1.DataPlane
		hasError     bool
		err          error
	}{
		{
			msg: "no promotion in progress",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-promotion",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: nil,
				},
			},
			oldDataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-promotion",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *oldOptions,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: nil,
				},
			},
			hasError: false,
		},
		{
			msg: "promotion starts",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
							},
						},
					},
				},
			},
			oldDataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *oldOptions,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutAwaitingPromotion),
							},
						},
					},
				},
			},
			hasError: true,
			err:      ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion,
		},
		{
			msg: "promotion in progress but no spec change is applied",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
					Annotations: map[string]string{
						"new": "value",
					},
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
							},
						},
					},
				},
			},
			oldDataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options, // The same just being applied
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
							},
						},
					},
				},
			},
			hasError: false,
		},
		{
			msg: "promotion in progress",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
							},
						},
					},
				},
			},
			oldDataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-in-progress",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *oldOptions,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionFalse,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionInProgress),
							},
						},
					},
				},
			},
			hasError: true,
			err:      ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion,
		},
		{
			msg: "promotion complete",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-complete",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *options,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionTrue,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionDone),
							},
						},
					},
				},
			},
			oldDataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "promotion-complete",
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: *oldOptions,
				},
				Status: operatorv1beta1.DataPlaneStatus{
					RolloutStatus: &operatorv1beta1.DataPlaneRolloutStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(consts.DataPlaneConditionTypeRolledOut),
								Status: metav1.ConditionTrue,
								Reason: string(consts.DataPlaneConditionReasonRolloutPromotionDone),
							},
						},
					},
				},
			},
			hasError: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.msg, func(t *testing.T) {
			v := &Validator{
				c: b.Build(),
			}
			err := v.ValidateUpdate(tc.dataplane, tc.oldDataplane)
			if !tc.hasError {
				require.NoError(t, err, tc.msg)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}
