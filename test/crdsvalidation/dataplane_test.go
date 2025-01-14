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

func TestDataPlane(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	t.Run("spec", func(t *testing.T) {
		kcfgcrdsvalidation.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "not providing image fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
					Spec: operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "providing image succeeds",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "dp-",
						Namespace:    ns.Name,
					},
					Spec: operatorv1beta1.DataPlaneSpec{
						DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
							Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
								DeploymentOptions: operatorv1beta1.DeploymentOptions{
									PodTemplateSpec: &corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "proxy",
													Image: "kong:3.9",
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
		}.RunWithConfig(t, cfg, scheme.Get())
	})
}
