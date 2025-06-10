package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/gateway-operator/controller/pkg/controlplane"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

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
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
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
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
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
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test",
						},
					},
				},
			},
			equal: true,
		},
		{
			name: "different watch namespaces yield unequal specs",
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
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
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
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2", "ns3"},
				},
			},
			equal: false,
		},
		{
			name: "the same watch namespaces yield equal specs",
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
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
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
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{"ns1", "ns2"},
				},
			},
			equal: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.equal, controlplane.SpecDeepEqual(tc.spec1, tc.spec2, tc.envVarsToIgnore...))
		})
	}
}
