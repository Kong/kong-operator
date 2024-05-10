package resources

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/pretty"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgapiscorev1 "k8s.io/kubernetes/pkg/apis/core/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
)

func TestStrategicMergePatchPodTemplateSpec(t *testing.T) {
	makeControlPlaneDeployment := func() (*appsv1.Deployment, error) {
		cp := &operatorv1beta1.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "cp-1",
			},
		}
		d, err := GenerateNewDeploymentForControlPlane(GenerateNewDeploymentForControlPlaneParams{
			ControlPlane:                   cp,
			ControlPlaneImage:              consts.DefaultControlPlaneImage,
			ServiceAccountName:             "kong-sa",
			AdminMTLSCertSecretName:        "kong-cert-secret",
			AdmissionWebhookCertSecretName: "kong-admission-cert-secret",
		})
		if err != nil {
			return nil, err
		}
		d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env,
			[]corev1.EnvVar{
				{
					Name:  "ENV1",
					Value: "VALUE1",
				},
				{
					Name:  "ENV2",
					Value: "VALUE2",
				},
				{
					Name:  "ENV3",
					Value: "VALUE3",
				},
			}...,
		)

		SetDefaultsPodTemplateSpec(&d.Spec.Template)
		return d, nil
	}

	clusterCertificateVolume := func() corev1.Volume {
		v := corev1.Volume{
			Name: consts.ClusterCertificateVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "kong-cert-secret",
					Items: []corev1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "tls.crt",
						},
						{
							Key:  "tls.key",
							Path: "tls.key",
						},
						{
							Key:  "ca.crt",
							Path: "ca.crt",
						},
					},
				},
			},
		}

		pkgapiscorev1.SetDefaults_Volume(&v)
		return v
	}

	clusterCertificateVolumeMount := func() corev1.VolumeMount {
		return corev1.VolumeMount{
			Name:      consts.ClusterCertificateVolume,
			MountPath: consts.ClusterCertificateVolumeMountPath,
			ReadOnly:  true,
		}
	}
	admissionWebhookVolumeMount := func() corev1.VolumeMount {
		return corev1.VolumeMount{
			Name:      consts.ControlPlaneAdmissionWebhookVolumeName,
			ReadOnly:  true,
			MountPath: consts.ControlPlaneAdmissionWebhookVolumeMountPath,
		}
	}

	testcases := []struct {
		Name     string
		Patch    *corev1.PodTemplateSpec
		Expected func() corev1.PodTemplateSpec
	}{
		{
			Name:  "empty patch doesn't change anything",
			Patch: &corev1.PodTemplateSpec{},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				return d.Spec.Template
			},
		},
		{
			Name: "add pod labels",
			Patch: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"label1": "value1",
						"label2": "value2",
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Labels["label1"] = "value1"
				d.Spec.Template.Labels["label2"] = "value2"
				return d.Spec.Template
			},
		},
		{
			Name: "patch the controller image",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  consts.ControlPlaneControllerContainerName,
							Image: "alpine",
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Image = "alpine"
				d.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
				return d.Spec.Template
			},
		},
		{
			Name: "add env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									// Prepend
									Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
									Value: "not-your-usual-admin-port-name",
								},
								{
									// Prepend
									Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
									Value: "not-your-usual-controller-name",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES",
						Value: "not-your-usual-admin-port-name",
					},
					{
						Name:  "CONTROLLER_GATEWAY_API_CONTROLLER_NAME",
						Value: "not-your-usual-controller-name",
					},
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "overwrite env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "overwrite 1 base env value and add 1 new env var",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
								{
									Name:  "CUSTOM_ENV",
									Value: "custom-env-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "CUSTOM_ENV",
						Value: "custom-env-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "add 1 new env var and overwrite 1 base env value",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "CUSTOM_ENV",
									Value: "custom-env-value",
								},
								{
									Name:  "ENV1",
									Value: "custom-value",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "CUSTOM_ENV",
						Value: "custom-env-value",
					},
					{
						Name:  "ENV1",
						Value: "custom-value",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				return d.Spec.Template
			},
		},
		{
			Name: "patch volume and volume mount prepends them",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "volume1",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "controller",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume1",
									MountPath: "/volume1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "volume1",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
							},
						},
					},
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
				}

				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "volume1",
						MountPath: "/volume1",
					},
					clusterCertificateVolumeMount(),
					admissionWebhookVolumeMount(),
				}

				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "patch volume and volume mount restating the base works",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "volume1",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "controller",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume1",
									MountPath: "/volume1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "volume1",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(1000, resource.DecimalSI),
							},
						},
					},
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
				}

				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "volume1",
						MountPath: "/volume1",
					},
					clusterCertificateVolumeMount(),
					admissionWebhookVolumeMount(),
				}

				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "ENV1",
							Value: "VALUE1",
						},
					},
				}
				d.Spec.Template.Spec.Containers = []corev1.Container{
					sidecarContainer,
					d.Spec.Template.Spec.Containers[0],
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar and a volume",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "new_volume",
									MountPath: "/volume",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "new_volume",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/host/path",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "ENV1",
							Value: "VALUE1",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "new_volume",
							MountPath: "/volume",
						},
					},
				}
				d.Spec.Template.Spec.Containers = append([]corev1.Container{sidecarContainer}, d.Spec.Template.Spec.Containers...)
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "new_volume",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/host/path",
								Type: lo.ToPtr(corev1.HostPathUnset),
							},
						},
					},
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar and change controller's image",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "SIDECAR_ENV1",
									Value: "SIDECAR_VALUE1",
								},
							},
						},
						{
							Name:  "controller",
							Image: "custom:1.0",
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)

				require.Len(t, d.Spec.Template.Spec.Containers, 1)
				d.Spec.Template.Spec.Containers[0].Image = "custom:1.0"

				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "SIDECAR_ENV1",
							Value: "SIDECAR_VALUE1",
						},
					},
				}
				d.Spec.Template.Spec.Containers = []corev1.Container{
					sidecarContainer,
					d.Spec.Template.Spec.Containers[0],
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "append a sidecar and change controller's image, define sidecar second",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "controller",
							Image: "custom:1.0",
						},
						{
							Name:  "sidecar",
							Image: "alpine",
							Command: []string{
								"sleep", "1000",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "SIDECAR_ENV1",
									Value: "SIDECAR_VALUE1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)

				require.Len(t, d.Spec.Template.Spec.Containers, 1)
				d.Spec.Template.Spec.Containers[0].Image = "custom:1.0"

				sidecarContainer := corev1.Container{
					Name:  "sidecar",
					Image: "alpine",
					Command: []string{
						"sleep", "1000",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "SIDECAR_ENV1",
							Value: "SIDECAR_VALUE1",
						},
					},
				}
				d.Spec.Template.Spec.Containers = []corev1.Container{
					d.Spec.Template.Spec.Containers[0],
					sidecarContainer,
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "change affinity",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": "random_app",
										},
									},
								},
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "metadata.name",
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													"abc",
													"def",
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
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "random_app",
									},
								},
							},
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "metadata.name",
											Operator: metav1.LabelSelectorOpIn,
											Values: []string{
												"abc",
												"def",
											},
										},
									},
								},
							},
						},
					},
				}

				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "restating the base volumes and volume mounts does not work",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							// NOTE: This was required in 1.2.x and older versions of the operator.
							// Now this only checks that this approach still works.
							Name: consts.ClusterCertificateVolume,
							// 1.3.x introduced a change in how strategic merge patch is applied for PodTemplateSpec
							// and the only discovered "breaking change" is that volume entries missing the
							// volume source will result in removing the volume source from the base.
							// Including the volume source in the patch (even empty like below) will keep the volume source.
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{},
							},
						},
						{
							// NOTE: This was required in 1.2.x and older versions of the operator.
							// Now this only checks that this approach still works.
							Name: consts.ControlPlaneAdmissionWebhookVolumeName,
							// 1.3.x introduced a change in how strategic merge patch is applied for PodTemplateSpec
							// and the only discovered "breaking change" is that volume entries missing the
							// volume source will result in removing the volume source from the base.
							// Including the volume source in the patch (even empty like below) will keep the volume source.
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{},
							},
						},
						{
							Name: "volume1",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "configmap-1",
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "controller",
							VolumeMounts: []corev1.VolumeMount{
								{
									// NOTE: This was required in 1.2.x and older versions of the operator.
									// Now this only checks that this approach still works.
									Name:      consts.ClusterCertificateVolume,
									MountPath: consts.ClusterCertificateVolumeMountPath,
								},
								{
									// NOTE: This was required in 1.2.x and older versions of the operator.
									// Now this only checks that this approach still works.
									Name:      consts.ControlPlaneAdmissionWebhookVolumeName,
									MountPath: consts.ControlPlaneAdmissionWebhookVolumeMountPath,
								},
								{
									Name:      "volume1",
									MountPath: "/volume1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				volume := corev1.Volume{
					Name: "volume1",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "configmap-1",
							},
						},
					},
				}

				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
					volume,
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					clusterCertificateVolumeMount(),
					admissionWebhookVolumeMount(),
					{
						Name:      "volume1",
						MountPath: "/volume1",
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "append a secret volume and volume mount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "controller",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "new_volume",
									MountPath: "/new_volume",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "new_volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "secret-1",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				volume := corev1.Volume{
					Name: "new_volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "secret-1",
						},
					},
				}

				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					volume,
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "new_volume",
						MountPath: "/new_volume",
					},
					clusterCertificateVolumeMount(),
					admissionWebhookVolumeMount(),
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)
				return d.Spec.Template
			},
		},
		{
			Name: "add hostPath volume and volumeMount",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "hostpath-volumemount",
									MountPath: "/var/log/hostpath",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "hostpath-volume",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "hostpath-volume",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/var/log",
								Type: lo.ToPtr(corev1.HostPathUnset),
							},
						},
					},
					clusterCertificateVolume(),
					controlPlaneAdmissionWebhookCertificateVolume("kong-admission-cert-secret"),
				}
				d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "hostpath-volumemount",
						MountPath: "/var/log/hostpath",
					},
					clusterCertificateVolumeMount(),
					admissionWebhookVolumeMount(),
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
		{
			Name: "add envs with fieldRef prepends it",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
		{
			Name: "add envs with fieldRef re-stating the base values",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
								{
									Name:  "ENV2",
									Value: "VALUE2",
								},
								{
									Name:  "ENV3",
									Value: "VALUE3",
								},
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
		{
			Name: "add env without restating the base prepends the new env",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
		{
			Name: "add env with restating the base first, appends the new env",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
								{
									Name:  "ENV2",
									Value: "VALUE2",
								},
								{
									Name:  "ENV3",
									Value: "VALUE3",
								},
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV2",
						Value: "VALUE2",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
		{
			Name: "add env and change the order of the env vars",
			Patch: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.ControlPlaneControllerContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "ENV1",
									Value: "VALUE1",
								},
								{
									Name:  "ENV3",
									Value: "VALUE3",
								},
								{
									Name: "LIMIT",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.cpu",
										},
									},
								},
								{
									Name:  "ENV2",
									Value: "XXX",
								},
							},
						},
					},
				},
			},
			Expected: func() corev1.PodTemplateSpec {
				d, err := makeControlPlaneDeployment()
				require.NoError(t, err)
				d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
					{
						Name:  "ENV1",
						Value: "VALUE1",
					},
					{
						Name:  "ENV3",
						Value: "VALUE3",
					},
					{
						Name: "LIMIT",
						ValueFrom: &corev1.EnvVarSource{
							ResourceFieldRef: &corev1.ResourceFieldSelector{
								Resource: "limits.cpu",
								Divisor:  *resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
					{
						Name:  "ENV2",
						Value: "XXX",
					},
				}
				SetDefaultsPodTemplateSpec(&d.Spec.Template)

				return d.Spec.Template
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			d, err := makeControlPlaneDeployment()
			require.NoError(t, err)
			result, err := StrategicMergePatchPodTemplateSpec(&d.Spec.Template, tc.Patch)
			require.NoError(t, err)

			// NOTE: We're using cmp.Diff here because assert.Equal has issues
			// comparing resource.Quantity. We could write custom logic for this
			// but cmp.Diff seems to do the job just fine.
			diff := cmp.Diff(*result, tc.Expected())
			if !assert.Empty(t, diff) {
				b := bytes.Buffer{}
				require.NoError(t, json.NewEncoder(&b).Encode(result))
				t.Logf("result:\n%s", pretty.Pretty(b.Bytes()))
			}
		})
	}
}
