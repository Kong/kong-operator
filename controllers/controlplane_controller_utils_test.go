package controllers

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/pkg/vars"
)

func TestSetControlPlaneDefaults(t *testing.T) {
	testCases := []struct {
		name                      string
		spec                      *operatorv1alpha1.ControlPlaneOptions
		namespace                 string
		dataplaneProxyServiceName string
		changed                   bool
		newSpec                   *operatorv1alpha1.ControlPlaneOptions
	}{
		{
			name:    "no envs no dataplane",
			spec:    &operatorv1alpha1.ControlPlaneOptions{},
			changed: true,
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
		{
			name:                      "no_envs_has_dataplane",
			spec:                      &operatorv1alpha1.ControlPlaneOptions{},
			changed:                   true,
			namespace:                 "test-ns",
			dataplaneProxyServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
		{
			name: "has_envs_and_dataplane",
			spec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
							{Name: "TEST_ENV", Value: "test"},
						},
					},
				},
			},
			changed:                   true,
			namespace:                 "test-ns",
			dataplaneProxyServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
		{
			name: "has_dataplane_env_unchanged",
			spec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
			namespace:                 "test-ns",
			dataplaneProxyServiceName: "kong-proxy",
			changed:                   false,
			newSpec: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
	}

	for i, tc := range testCases {
		index := i
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := setControlPlaneDefaults(tc.spec, map[string]struct{}{}, controlPlaneDefaultsArgs{
				dataPlanePodIP:            "1.2.3.4",
				namespace:                 tc.namespace,
				dataplaneProxyServiceName: tc.dataplaneProxyServiceName,
				dataplaneAdminServiceName: "kong-admin",
			})
			require.Equalf(t, tc.changed, changed,
				"should return the same value for test case %d:%s", index, tc.name)
			for _, env := range tc.newSpec.Deployment.Pods.Env {
				if env.Value != "" {
					actualValue := envValueByName(tc.spec.Deployment.Pods.Env, env.Name)
					require.Equalf(t, env.Value, actualValue,
						"should have the same value of env %s", env.Name)
				}
				if env.ValueFrom != nil {
					actualValueFrom := envVarSourceByName(tc.spec.Deployment.Pods.Env, env.Name)
					require.Truef(t, reflect.DeepEqual(env.ValueFrom, actualValueFrom),
						"should have same valuefrom of env %s", env.Name)
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
					Pods: operatorv1alpha1.PodsOptions{
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
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
			equal: true,
		},
		{
			name: "matching env vars, with ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
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
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
			envVarsToIgnore: []string{
				"CONTROLLER_KONG_ADMIN_URL",
			},
			equal: true,
		},
		{
			name: "not matching env vars, no ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
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
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
			equal: false,
		},
		{
			name: "not matching env vars, with ignored vars",
			spec1: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
							{
								Name:  "CONTROLLER_PUBLISH_SERVICE",
								Value: "test-ns/kong-proxy",
							},
							{
								Name:  "CONTROLLER_KONG_ADMIN_URL",
								Value: "https://1-2-3-4.kong-admin.test-ns.svc:8444",
							},
							{
								Name:  "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE",
								Value: "/var/cluster-certificate/tls.key",
							},
						},
					},
				},
			},
			spec2: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
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
			envVarsToIgnore: []string{
				"CONTROLLER_KONG_ADMIN_URL",
			},
			equal: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.equal, controlplaneSpecDeepEqual(tc.spec1, tc.spec2, tc.envVarsToIgnore...))
		})
	}
}
