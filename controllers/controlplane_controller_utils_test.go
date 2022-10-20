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
		name                 string
		spec                 *operatorv1alpha1.ControlPlaneDeploymentOptions
		namespace            string
		dataplaceServiceName string
		changed              bool
		newSpec              *operatorv1alpha1.ControlPlaneDeploymentOptions
	}{
		{
			name:    "no_envs_no_dataplane",
			spec:    &operatorv1alpha1.ControlPlaneDeploymentOptions{},
			changed: true,
			newSpec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
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
							Value: vars.ControllerName,
						},
					},
				},
			},
		},
		{
			name:                 "no_envs_has_dataplane",
			spec:                 &operatorv1alpha1.ControlPlaneDeploymentOptions{},
			changed:              true,
			namespace:            "test-ns",
			dataplaceServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
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
							Value: vars.ControllerName,
						},
						{
							Name:  "CONTROLLER_PUBLISH_SERVICE",
							Value: "test-ns/kong-proxy",
						},
						{
							Name:  "CONTROLLER_KONG_ADMIN_URL",
							Value: "https://kong-proxy.test-ns.svc:8444",
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
		{
			name: "has_envs_and_dataplane",
			spec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "TEST_ENV", Value: "test"},
					},
				},
			},
			changed:              true,
			namespace:            "test-ns",
			dataplaceServiceName: "kong-proxy",
			newSpec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
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
							Value: vars.ControllerName,
						},
						{
							Name:  "CONTROLLER_PUBLISH_SERVICE",
							Value: "test-ns/kong-proxy",
						},
						{
							Name:  "CONTROLLER_KONG_ADMIN_URL",
							Value: "https://kong-proxy.test-ns.svc:8444",
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
		{
			name: "has_dataplane_env_unchanged",
			spec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
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
							Value: vars.ControllerName,
						},
						{
							Name:  "CONTROLLER_PUBLISH_SERVICE",
							Value: "test-ns/kong-proxy",
						},
						{
							Name:  "CONTROLLER_KONG_ADMIN_URL",
							Value: "https://kong-proxy.test-ns.svc:8444",
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
			namespace:            "test-ns",
			dataplaceServiceName: "kong-proxy",
			changed:              false,
			newSpec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
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
							Value: vars.ControllerName,
						},
						{
							Name:  "CONTROLLER_PUBLISH_SERVICE",
							Value: "test-ns/kong-proxy",
						},
						{
							Name:  "CONTROLLER_KONG_ADMIN_URL",
							Value: "https://kong-proxy.test-ns.svc:8444",
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
	}

	for i, tc := range testCases {
		index := i
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed, err := setControlPlaneDefaults(tc.spec, tc.namespace, tc.dataplaceServiceName, map[string]struct{}{}, false)
			require.NoError(t, err)
			require.Equalf(t, tc.changed, changed,
				"should return the same value for test case %d:%s", index, tc.name)
			for _, env := range tc.newSpec.Env {
				if env.Value != "" {
					actualValue := envValueByName(tc.spec.Env, env.Name)
					require.Equalf(t, env.Value, actualValue,
						"should have the same value of env %s", env.Name)
				}
				if env.ValueFrom != nil {
					actualValueFrom := envVarSourceByName(tc.spec.Env, env.Name)
					require.Truef(t, reflect.DeepEqual(env.ValueFrom, actualValueFrom),
						"should have same valuefrom of env %s", env.Name)
				}
			}
		})
	}
}
