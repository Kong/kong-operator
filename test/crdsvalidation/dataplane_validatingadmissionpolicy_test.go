package crdsvalidation

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/envtest"
	"github.com/kong/gateway-operator/test/helpers/kustomize"

	kcfgcrdsvalidation "github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

const (
	// KustomizePathValidatingPolicies is the path to the Kustomize directory containing the validation policies.
	KustomizePathValidatingPolicies = "config/default/validating_policies/"
)

func TestDataPlaneValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	var (
		ctx              = context.Background()
		scheme           = scheme.Get()
		cfg, ns          = envtest.Setup(t, ctx, scheme)
		commonObjectMeta = metav1.ObjectMeta{
			GenerateName: "dp-",
			Namespace:    ns.Name,
		}
		sharedEventuallyConfig = kcfgcrdsvalidation.EventuallyConfig{
			Timeout: 5 * time.Second,
			Period:  100 * time.Millisecond,
		}
	)

	kustomize.Apply(ctx, t, cfg, KustomizePathValidatingPolicies)

	t.Run("ports", func(t *testing.T) {
		kcfgcrdsvalidation.TestCasesGroup[*operatorv1beta1.DataPlane]{
			{
				Name: "not providing spec fails",
				TestObject: &operatorv1beta1.DataPlane{
					ObjectMeta: commonObjectMeta,
					Spec:       operatorv1beta1.DataPlaneSpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("DataPlane requires an image to be set on proxy container"),
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
			},
			// TODO: ValidatingAdmissionPolicy rules have to be adjusted for this test case.
			{
				Name: "providing network services ingress ports without matching envs should fail",
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
				ExpectedErrorMessage:          lo.ToPtr("Each port from spec.network.services.ingress.ports has to have an accompanying port in KONG_PORT_MAPS env"),
				ExpectedErrorEventuallyConfig: sharedEventuallyConfig,
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
