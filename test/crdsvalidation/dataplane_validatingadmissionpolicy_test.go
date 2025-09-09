package crdsvalidation

import (
	"path"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"

	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/envtest"
	"github.com/kong/kong-operator/test/helpers/helm"
)

const (
	// ChartPath is the path relative to the project to the kong-operator chart
	ChartPath = "charts/kong-operator"

	// ValidationPolicy_ contain validation policies path relative to the kong-operator chart
	ValidationPolicyDataplane = "templates/validation-policy-dataplane.yaml"
	ValidationPolicyKonnect   = "templates/validation-policy-konnect.yaml"
)

var sharedEventuallyConfig = common.EventuallyConfig{
	Timeout: 15 * time.Second,
	Period:  100 * time.Millisecond,
}

func TestKonnectValidationAdmissionPolicy(t *testing.T) {

	var (
		ctx              = t.Context()
		scheme           = scheme.Get()
		cfg, ns          = envtest.Setup(t, ctx, scheme)
		commonObjectMeta = metav1.ObjectMeta{
			GenerateName: "dp-",
			Namespace:    ns.Name,
		}
	)

	logger := zapr.NewLogger(zap.New(zapcore.NewNopCore()))
	// Prevents controller-runtime from logging
	// [controller-runtime] log.SetLogger(...) was never called; logs will not be displayed.
	ctrl.SetLogger(logger)

	wc := &common.WarningCollector{}
	cfg.WarningHandler = wc

	chartPath := path.Join(test.ProjectRootPath(), ChartPath)
	templates := []string{
		ValidationPolicyKonnect,
	}

	helm.ApplyTemplate(t, cfg, chartPath, templates)

	t.Run("static autoscale", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration]{
			{
				Name: "deprecate message with static autoscale type",
				TestObject: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
					ObjectMeta: commonObjectMeta,
					Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
						DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
							{
								Provider: "aws",
								Region:   "us-west-2",
								NetworkRef: commonv1alpha1.ObjectRef{
									Type:      commonv1alpha1.ObjectRefTypeKonnectID,
									KonnectID: lo.ToPtr("kpat_ABCDEFGHILMNOPQRSTUVZabcdefghilmnopqrstuvz0123456"),
								},
								Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
									Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeStatic,
									Static: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleStatic{
										InstanceType: "small",
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				WarningCollector:              wc,
				ExpectedWarningMessage:        lo.ToPtr("Value \"static\" in spec.dataplane_groups.autoscale.type is deprecated, use \"automatic\" instead."),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}

func TestDataPlaneValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	var (
		ctx              = t.Context()
		scheme           = scheme.Get()
		cfg, ns          = envtest.Setup(t, ctx, scheme)
		commonObjectMeta = metav1.ObjectMeta{
			GenerateName: "dp-",
			Namespace:    ns.Name,
		}
	)

	chartPath := path.Join(test.ProjectRootPath(), ChartPath)
	templates := []string{
		"templates/validation-policy-dataplane.yaml",
	}

	helm.ApplyTemplate(t, cfg, chartPath, templates)

	t.Run("ports", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "not providing spec fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec:       operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("DataPlane requires an image to be set on proxy container"),
			},
			{
				Name: "providing correct ingress service ports and KONG_PORT_MAPS env succeeds",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_PROXY_LISTEN",
															Value: "0.0.0.0:8000 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
														},
														{
															Name:  "KONG_PORT_MAPS",
															Value: "80:8000,443:8443",
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
												Name:       "http",
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
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "providing incorrect ingress service ports and KONG_PORT_MAPS env fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_PROXY_LISTEN",
															Value: "0.0.0.0:8000 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
														},
														{
															Name:  "KONG_PORT_MAPS",
															Value: "80:8000,443:8443",
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
												Name: "http",
												Port: 80,
												// No matching port in KONG_PORT_MAPS
												TargetPort: intstr.FromInt(8001),
											},
											{
												Name:       "http",
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
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("is forbidden: ValidatingAdmissionPolicy 'ports.dataplane.gateway-operator.konghq.com' with binding 'binding-ports.dataplane.gateway-operator.konghq.com' denied request: Each port from spec.network.services.ingress.ports has to have an accompanying port in KONG_PORT_MAPS env"),
			},
			{
				Name: "providing correct ingress service ports and KONG_PROXY_LISTEN env succeeds",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_PROXY_LISTEN",
															Value: "0.0.0.0:8000 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
														},
														{
															Name:  "KONG_PORT_MAPS",
															Value: "80:8000,443:8443",
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
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "providing incorrect ingress service ports and KONG_PROXY_LISTEN env fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
													Env: []corev1.EnvVar{
														{
															Name:  "KONG_PROXY_LISTEN",
															Value: "0.0.0.0:8000 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
														},
														{
															Name:  "KONG_PORT_MAPS",
															Value: "80:8000,443:8443",
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
												Name: "http",
												Port: 80,
												// No matching port in KONG_PROXY_LISTEN
												TargetPort: intstr.FromInt(8001),
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
				ExpectedErrorMessage:          lo.ToPtr("is forbidden: ValidatingAdmissionPolicy 'ports.dataplane.gateway-operator.konghq.com' with binding 'binding-ports.dataplane.gateway-operator.konghq.com' denied request: Each port from spec.network.services.ingress.ports has to have an accompanying port in KONG_PORT_MAPS env"),
			},
			{
				Name: "providing network services ingress options without ports does not fail",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										ServiceOptions: operatorv1beta1.ServiceOptions{
											Annotations: map[string]string{
												"a": "b",
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
			{
				Name: "providing network services ingress ports without matching envs does not fail (legacy webhook behavior)",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
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
							Network: operatorv1beta1.DataPlaneNetworkOptions{
								Services: &operatorv1beta1.DataPlaneServices{
									Ingress: &operatorv1beta1.DataPlaneServiceOptions{
										Ports: []operatorv1beta1.DataPlaneServicePort{
											{
												Name: "http",
												Port: 80,
												// No matching port in KONG_PORT_MAPS
												TargetPort: intstr.FromInt(8001),
											},
										},
									},
								},
							},
						},
					},
				},
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
