package crdsvalidation

import (
	"testing"

	"github.com/samber/lo"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/envtest"

	kcfgcrdsvalidation "github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestControlPlane(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	t.Run("spec", func(t *testing.T) {
		kcfgcrdsvalidation.TestCasesGroup[*operatorv1beta1.ControlPlane]{
			{
				Name: "not providing image fails",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "cp-",
						Namespace:    ns.Name,
					},
					Spec: operatorv1beta1.ControlPlaneSpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("ControlPlane requires an image to be set on controller container"),
			},
			{
				Name: "providing image succeeds",
				TestObject: &operatorv1beta1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "cp-",
						Namespace:    ns.Name,
					},
					Spec: operatorv1beta1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
							Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "controller",
												Image: "kong/kubernetes-ingress-controller:3.4.1",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme.Get())
	})
}
