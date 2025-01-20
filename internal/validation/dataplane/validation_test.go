package dataplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
)

func TestDataPlaneIngressServiceOptions(t *testing.T) {
	testCases := []struct {
		msg       string
		dataplane *operatorv1beta1.DataPlane
		hasError  bool
		errMsg    string
	}{
		{
			msg: "dataplane with ingress service options but KONG_PORT_MAPS and KONG_PROXY_LISTEN not specified should be valid",
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
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
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
										{Name: "http", Port: int32(80), TargetPort: intstr.FromInt(8080)},
									},
								},
							},
						},
					},
				},
			},
			hasError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.msg, func(t *testing.T) {
			b := fakeclient.NewClientBuilder()
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
		oldDataPlane *operatorv1beta1.DataPlane
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
			oldDataPlane: &operatorv1beta1.DataPlane{
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
			oldDataPlane: &operatorv1beta1.DataPlane{
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
			oldDataPlane: &operatorv1beta1.DataPlane{
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
			oldDataPlane: &operatorv1beta1.DataPlane{
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
			oldDataPlane: &operatorv1beta1.DataPlane{
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
		t.Run(tc.msg, func(t *testing.T) {
			v := &Validator{
				c: b.Build(),
			}
			err := v.ValidateUpdate(tc.dataplane, tc.oldDataPlane)
			if !tc.hasError {
				require.NoError(t, err, tc.msg)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}
