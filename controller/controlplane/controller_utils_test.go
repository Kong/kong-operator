package controlplane

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

func TestSetControlPlaneDefaults(t *testing.T) {
	testCases := []struct {
		name                        string
		spec                        *operatorv1beta1.ControlPlaneOptions
		namespace                   string
		dataplaneIngressServiceName string
		dataplaneAdminServiceName   string
		changed                     bool
		anonymousReportsEnabled     bool
		newSpec                     *operatorv1beta1.ControlPlaneOptions
	}{
		{
			name:                    "no_envs_no_dataplane_no_anonymous_reports",
			spec:                    &operatorv1beta1.ControlPlaneOptions{},
			changed:                 true,
			anonymousReportsEnabled: false,
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "false",
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
			name:                    "no_envs_no_dataplane_with_anonymous_reports",
			spec:                    &operatorv1beta1.ControlPlaneOptions{},
			changed:                 true,
			anonymousReportsEnabled: true,
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
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
			name:                        "no_envs_has_dataplane_no_anonymous_reports",
			spec:                        &operatorv1beta1.ControlPlaneOptions{},
			changed:                     true,
			anonymousReportsEnabled:     false,
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			dataplaneAdminServiceName:   "kong-admin",
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_KONG_ADMIN_SVC",
											Value: "test-ns/kong-admin",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
											Value: "admin",
										},
										{
											Name:  "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY",
											Value: consts.DataPlaneServiceDNSDiscoveryStrategy,
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "false",
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
			name:                        "no_envs_has_dataplane_with_anonymous_reports",
			spec:                        &operatorv1beta1.ControlPlaneOptions{},
			changed:                     true,
			anonymousReportsEnabled:     true,
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			dataplaneAdminServiceName:   "kong-admin",
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_KONG_ADMIN_SVC",
											Value: "test-ns/kong-admin",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
											Value: "admin",
										},
										{
											Name:  "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY",
											Value: consts.DataPlaneServiceDNSDiscoveryStrategy,
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
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
			name: "has_envs_and_dataplane_no_anonymous_reports",
			spec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
										},
									},
								},
							},
						},
					},
				},
			},
			changed:                     true,
			anonymousReportsEnabled:     false,
			namespace:                   "test-ns",
			dataplaneIngressServiceName: "kong-proxy",
			dataplaneAdminServiceName:   "kong-admin",
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{Name: "TEST_ENV", Value: "test"},
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_KONG_ADMIN_SVC",
											Value: "test-ns/kong-admin",
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "false",
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
			name: "has_dataplane_env_unchanged_with_anonymous_reports",
			spec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_KONG_ADMIN_SVC",
											Value: "test-ns/kong-admin",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
											Value: "admin",
										},
										{
											Name:  "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY",
											Value: consts.DataPlaneServiceDNSDiscoveryStrategy,
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
										},
										{
											Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
											Value: consts.ControlPlaneAdmissionWebhookEnvVarValue,
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
			dataplaneAdminServiceName:   "kong-admin",
			changed:                     false,
			anonymousReportsEnabled:     true,
			newSpec: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													APIVersion: "v1", FieldPath: "metadata.namespace",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
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
											Name:  "CONTROLLER_KONG_ADMIN_SVC",
											Value: "test-ns/kong-admin",
										},
										{
											Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
											Value: "admin",
										},
										{
											Name:  "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY",
											Value: consts.DataPlaneServiceDNSDiscoveryStrategy,
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
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
										},
										{
											Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
											Value: consts.ControlPlaneAdmissionWebhookEnvVarValue,
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
			changed := controlplane.SetDefaults(
				tc.spec,
				map[string]struct{}{},
				controlplane.DefaultsArgs{
					Namespace:                   tc.namespace,
					DataPlaneIngressServiceName: tc.dataplaneIngressServiceName,
					DataPlaneAdminServiceName:   tc.dataplaneAdminServiceName,
					AnonymousReportsEnabled:     tc.anonymousReportsEnabled,
					OwnedByGateway:              "",
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
		spec1           *operatorv1beta1.ControlPlaneOptions
		spec2           *operatorv1beta1.ControlPlaneOptions
		envVarsToIgnore []string
		equal           bool
	}{
		{
			name: "matching env vars, no ignored vars",
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
		{
			name: "not matching Extensions",
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
									},
								},
							},
						},
					},
				},
			},
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
									},
								},
							},
						},
					},
				},
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: false,
		},
		{
			name: "matching Extensions",
			spec1: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
								},
							},
						},
					},
				},
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			spec2: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
								},
							},
						},
					},
				},
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: true,
		},
		{
			name: "not matching Extensions, different length",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test2",
						},
					},
				},
			},
			equal: false,
		},
		{
			name: "matching Extensions, different order",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test2",
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test2",
						},
					},
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: false,
		},
		{
			name: "not matching Extensions, different names",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Extensions: []operatorv1alpha1.ExtensionRef{
					{
						NamespacedRef: operatorv1alpha1.NamespacedRef{
							Name: "test2",
						},
					},
				},
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
