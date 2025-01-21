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
