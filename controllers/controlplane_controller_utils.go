package controllers

import (
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions
// -----------------------------------------------------------------------------

func setControlPlaneDefaults(spec *operatorv1alpha1.ControlPlaneDeploymentOptions, namespace string, dataplaneServiceName string, dontOverride map[string]struct{}) {
	spec.Env = append(spec.Env, corev1.EnvVar{Name: "CONTROLLER_KONG_ADMIN_TLS_SKIP_VERIFY", Value: "true"}) // TODO: for poc only, don't release with this https://github.com/Kong/gateway-operator/issues/7
	spec.Env = append(spec.Env, corev1.EnvVar{
		Name: "POD_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.namespace",
			},
		},
	})
	spec.Env = append(spec.Env, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.name",
			},
		},
	})

	if namespace != "" && dataplaneServiceName != "" {
		if _, isOverrideDisabled := dontOverride["CONTROLLER_PUBLISH_SERVICE"]; !isOverrideDisabled {
			spec.Env = updateEnv(spec.Env, "CONTROLLER_PUBLISH_SERVICE", controllerPublishService(dataplaneServiceName, namespace))
		}
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_URL"]; !isOverrideDisabled {
			spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_URL", controllerKongAdminURL(dataplaneServiceName, namespace))
		}
	}
}

func setControlPlaneEnvOnDataPlaneChange(
	spec *operatorv1alpha1.ControlPlaneDeploymentOptions,
	namespace string,
	dataplaneServiceName string,
) bool {
	var changed bool

	dataplaneIsSet := spec.DataPlane != nil && *spec.DataPlane != ""
	if dataplaneIsSet {
		newPublishServiceValue := controllerPublishService(dataplaneServiceName, namespace)
		if envValueByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE") != newPublishServiceValue {
			spec.Env = updateEnv(spec.Env, "CONTROLLER_PUBLISH_SERVICE", newPublishServiceValue)
			changed = true
		}
		newKongAdminURL := controllerKongAdminURL(dataplaneServiceName, namespace)
		if envValueByName(spec.Env, "CONTROLLER_KONG_ADMIN_URL") != newKongAdminURL {
			spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_URL", newKongAdminURL)
			changed = true
		}
	} else {
		if envValueByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE") != "" {
			spec.Env = rejectEnvByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE")
			changed = true
		}
		if envValueByName(spec.Env, "CONTROLLER_KONG_ADMIN_URL") != "" {
			spec.Env = rejectEnvByName(spec.Env, "CONTROLLER_KONG_ADMIN_URL")
			changed = true
		}
	}

	return changed
}

func controllerKongAdminURL(dataplaneName, dataplaneNamespace string) string {
	return fmt.Sprintf("https://%s.%s.svc:%d",
		dataplaneName, dataplaneNamespace, dataplaneutils.DefaultKongAdminPort)
}

func controllerPublishService(dataplaneName, dataplaneNamespace string) string {
	return fmt.Sprintf("%s/%s", dataplaneNamespace, dataplaneName)
}

// envValueByName returns the value of the first env var with the given name.
// If no env var with the given name is found, an empty string is returned.
func envValueByName(env []corev1.EnvVar, name string) string {
	for _, envVar := range env {
		if envVar.Name == name {
			return envVar.Value
		}
	}
	return ""
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

// rejectEnvByName returns a copy of the given env vars,
// but with the env vars with the given name removed.
func rejectEnvByName(envVars []corev1.EnvVar, name string) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	for _, envVar := range envVars {
		if envVar.Name != name {
			newEnvVars = append(newEnvVars, envVar)
		}
	}
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
			Namespace:    controlplane.Namespace,
			GenerateName: fmt.Sprintf("%s-%s-", consts.ControlPlanePrefix, controlplane.Name),
			Labels: map[string]string{
				"app": controlplane.Name,
			},
			OwnerReferences: []metav1.OwnerReference{k8sutils.GenerateOwnerReferenceForObject(controlplane)},
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
// ControlPlane - Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func labelObjForControlPlane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.ControlPlaneManagedLabelValue
	obj.SetLabels(labels)
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func controlplaneSpecDeepEqual(spec1, spec2 *operatorv1alpha1.ControlPlaneDeploymentOptions) bool {
	if !deploymentOptionsDeepEqual(&spec1.DeploymentOptions, &spec2.DeploymentOptions) {
		return false
	}

	if !reflect.DeepEqual(spec1.DataPlane, spec2.DataPlane) {
		return false
	}

	return true
}
