package controllers

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
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
						{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"},
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
						{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"},
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
						{Name: "CONTROLLER_PUBLISH_SERVICE", Value: "test-ns/kong-proxy"},
						{Name: "CONTROLLER_KONG_ADMIN_URL", Value: "https://kong-proxy.test-ns.svc:8444"},
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
						{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"},
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
						{Name: "CONTROLLER_PUBLISH_SERVICE", Value: "test-ns/kong-proxy"},
						{Name: "CONTROLLER_KONG_ADMIN_URL", Value: "https://kong-proxy.test-ns.svc:8444"},
					},
				},
			},
		},
		{
			name: "has_dataplane_env_unchanged",
			spec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"},
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
						{Name: "CONTROLLER_PUBLISH_SERVICE", Value: "test-ns/kong-proxy"},
						{Name: "CONTROLLER_KONG_ADMIN_URL", Value: "https://kong-proxy.test-ns.svc:8444"},
					},
				},
			},
			namespace:            "test-ns",
			dataplaceServiceName: "kong-proxy",
			changed:              false,
			newSpec: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"},
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
						{Name: "CONTROLLER_PUBLISH_SERVICE", Value: "test-ns/kong-proxy"},
						{Name: "CONTROLLER_KONG_ADMIN_URL", Value: "https://kong-proxy.test-ns.svc:8444"},
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		index := i
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := setControlPlaneDefaults(tc.spec, tc.namespace, tc.dataplaceServiceName, map[string]struct{}{})
			require.Equalf(t, tc.changed, changed,
				"should return the same value for test case %d:%s", index, tc.name)
			require.Truef(t, reflect.DeepEqual(tc.spec, tc.newSpec),
				"the updated spec should be equal to expected for test case %d:%s\nexpected: %+v\nactual: %+v",
				index, tc.name, tc.newSpec, tc.spec)
		})
	}
}
