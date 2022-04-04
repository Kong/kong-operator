package controllers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions
// -----------------------------------------------------------------------------

func setControlPlaneDefaults(controlplane *operatorv1alpha1.ControlPlane) {
	controlplane.Spec.Env = append(controlplane.Spec.Env, corev1.EnvVar{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"}) // TODO: for poc only, don't release with this https://github.com/Kong/gateway-operator/issues/7
	controlplane.Spec.Env = append(controlplane.Spec.Env, corev1.EnvVar{
		Name: "POD_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.namespace",
			},
		},
	})
	controlplane.Spec.Env = append(controlplane.Spec.Env, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.name",
			},
		},
	})

	if controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane != "" {
		controlplane.Spec.Env = updateEnv(controlplane.Spec.Env, "CONTROLLER_PUBLISH_SERVICE", fmt.Sprintf("%s/svc-%s", controlplane.Namespace, *controlplane.Spec.DataPlane))
		controlplane.Spec.Env = updateEnv(controlplane.Spec.Env, "CONTROLLER_KONG_ADMIN_URL", fmt.Sprintf("https://svc-%s.%s.svc:%d", *controlplane.Spec.DataPlane, controlplane.Namespace, defaultKongAdminPort))
	}
}

func updateEnv(envVars []corev1.EnvVar, name, val string) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	for _, envVar := range envVars {
		if envVar.Name != name {
			newEnvVars = append(newEnvVars, envVar)
		}
	}

	newEnvVars = append(newEnvVars, corev1.EnvVar{
		Name:  name,
		Value: val,
	})

	return newEnvVars
}

func generateNewDeploymentForControlPlane(controlplane *operatorv1alpha1.ControlPlane) *appsv1.Deployment {
	var controlplaneImage string
	if controlplane.Spec.ContainerImage != nil {
		controlplaneImage = *controlplane.Spec.ContainerImage
		if controlplane.Spec.Version != nil {
			controlplaneImage = fmt.Sprintf("%s:%s", controlplaneImage, *controlplane.Spec.Version)
		}
	} else {
		controlplaneImage = consts.DefaultControlPlaneImage // TODO: https://github.com/Kong/gateway-operator/issues/20
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplane.Namespace,
			Name:      controlplane.Name, // TODO: https://github.com/Kong/gateway-operator/issues/21
			Labels: map[string]string{
				"app": controlplane.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": controlplane.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": controlplane.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "default", // TODO: https://github.com/Kong/gateway-operator/issues/28
					Containers: []corev1.Container{{
						Name:            "controller",
						Env:             controlplane.Spec.Env,
						EnvFrom:         controlplane.Spec.EnvFrom,
						Image:           controlplaneImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Lifecycle: &corev1.Lifecycle{
							PreStop: &corev1.LifecycleHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"/bin/sh",
										"-c",
										"kong quit",
									},
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "health",
								ContainerPort: 10254,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						LivenessProbe: &corev1.Probe{
							FailureThreshold:    3,
							InitialDelaySeconds: 5,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/healthz",
									Port:   intstr.FromInt(10254),
									Scheme: corev1.URISchemeHTTP,
								},
							},
						},
						ReadinessProbe: &corev1.Probe{
							FailureThreshold:    3,
							InitialDelaySeconds: 5,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/readyz",
									Port:   intstr.FromInt(10254),
									Scheme: corev1.URISchemeHTTP,
								},
							},
						},
					}},
				},
			},
		},
	}
	return deployment
}

// -----------------------------------------------------------------------------
// Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func labelObjForControlPlane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.ControlPlaneManagedLabelValue
	obj.SetLabels(labels)
}
