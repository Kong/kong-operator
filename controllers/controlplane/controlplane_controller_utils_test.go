package controlplane

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers/pkg/controlplane"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

func TestSetControlPlaneDefaults(t *testing.T) {
	testCases := []struct {
		name                        string
		spec                        *operatorv1alpha1.ControlPlaneOptions
		namespace                   string
		dataplaneIngressServiceName string
		changed                     bool
		newSpec                     *operatorv1alpha1.ControlPlaneOptions
	}{
		{
			name:    "no_envs_no_dataplane",
			spec:    &operatorv1alpha1.ControlPlaneOptions{},
			changed: true,
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
											Value: vars.ControllerName(),
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
			name:                        "no_envs_has_dataplane",
			spec:                        &operatorv1alpha1.ControlPlaneOptions{},
			changed:                     true,
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
											Value: vars.ControllerName(),
										},
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_CA_CERT_FILE",
											Value: "/var/cluster-certificate/ca.crt",
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
			name: "has_envs_and_dataplane",
			spec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name:  "TEST_ENV",
											Value: "test",
										},
									},
								},
							},
						},
					},
				},
			},
			changed:                     true,
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{Name: "TEST_ENV", Value: "test"},
										{
											Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
											Value: vars.ControllerName(),
										},
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_CA_CERT_FILE",
											Value: "/var/cluster-certificate/ca.crt",
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
			name: "has_dataplane_env_unchanged",
			spec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
											Value: vars.ControllerName(),
										},
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_CA_CERT_FILE",
											Value: "/var/cluster-certificate/ca.crt",
										},
									},
								},
							},
						},
					},
				},
			},
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			changed:                     false,
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
											Value: vars.ControllerName(),
										},
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_CA_CERT_FILE",
											Value: "/var/cluster-certificate/ca.crt",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		index := i
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := controlplane.SetDefaults(tc.spec, map[string]struct{}{}, controlplane.DefaultsArgs{
				DataPlanePodIP:              "1.2.3.4",
				Namespace:                   tc.namespace,
				DataplaneIngressServiceName: tc.dataplaneIngressServiceName,
				DataplaneAdminServiceName:   "kong-admin",
			})
			require.Equalf(t, tc.changed, changed,
				"should return the same value for test case %d:%s", index, tc.name)

			containerNewSpec := k8sutils.GetPodContainerByName(&tc.newSpec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
			require.NotNil(t, containerNewSpec)

			container := k8sutils.GetPodContainerByName(&tc.spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
			require.NotNil(t, container)

			for _, env := range containerNewSpec.Env {
				if env.Value != "" {
					actualValue := k8sutils.EnvValueByName(container.Env, env.Name)
					require.Equalf(t, env.Value, actualValue,
						"should have the same value of env %s", env.Name)
				}
				if env.ValueFrom != nil {
					actualValueFrom := k8sutils.EnvVarSourceByName(container.Env, env.Name)
					if !assert.Truef(t, reflect.DeepEqual(env.ValueFrom, actualValueFrom),
						"should have same valuefrom of env %s", env.Name) {
						t.Logf("diff:\n%s", cmp.Diff(env.ValueFrom, actualValueFrom))
					}
				}
			}
		})
	}
}

func TestControlPlaneSpecDeepEqual(t *testing.T) {
	testCases := []struct {
		name            string
		spec1           *operatorv1alpha1.ControlPlaneOptions
		spec2           *operatorv1alpha1.ControlPlaneOptions
		envVarsToIgnore []string
		equal           bool
	}{
		{
			name: "matching env vars, no ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
									},
								},
							},
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
									},
								},
							},
						},
					},
				},
			},
			equal: true,
		},
		{
			name: "matching env vars, with ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
									},
								},
							},
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
									},
								},
							},
						},
					},
				},
			},
			envVarsToIgnore: []string{
				"CONTROLLER_KONG_ADMIN_URL",
			},
			equal: true,
		},
		{
			name: "not matching env vars, no ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
									},
								},
							},
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
									},
								},
							},
						},
					},
				},
			},
			equal: false,
		},
		{
			name: "not matching env vars, with ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE",
											Value: "/var/cluster-certificate/tls.crt",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_URL",
											Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
										},
									},
								},
							},
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_PUBLISH_SERVICE",
											Value: "test-ns/kong-proxy",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
											Value: "/var/cluster-certificate/tls.key",
										},
									},
								},
							},
						},
					},
				},
			},
			envVarsToIgnore: []string{
				"CONTROLLER_KONG_ADMIN_URL",
			},
			equal: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.equal, controlplane.SpecDeepEqual(tc.spec1, tc.spec2, tc.envVarsToIgnore...))
		})
	}
}
