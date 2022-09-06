package controllers

import (
	"fmt"
	"os"
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

// setControlPlaneDefaults updates the environment variables of control plane
// and returns true if env field is changed.
func setControlPlaneDefaults(
	spec *operatorv1alpha1.ControlPlaneDeploymentOptions,
	namespace string, dataplaneServiceName string,
	dontOverride map[string]struct{},
) bool {
	changed := false

	// set env POD_NAMESPACE. should be always from `metadata.namespace` of pod.
	envSourceMetadataNamespace := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			APIVersion: "v1",
			FieldPath:  "metadata.namespace",
		},
	}
	if !reflect.DeepEqual(envSourceMetadataNamespace, envVarSourceByName(spec.Env, "POD_NAMESPACE")) {
		spec.Env = updateEnvSource(spec.Env, "POD_NAMESPACE", envSourceMetadataNamespace)
		changed = true
	}

	// set env POD_NAME. should be always from `metadata.name` of pod.
	envSourceMetadataName := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			APIVersion: "v1",
			FieldPath:  "metadata.name",
		},
	}
	if !reflect.DeepEqual(envSourceMetadataName, envVarSourceByName(spec.Env, "POD_NAME")) {
		spec.Env = updateEnvSource(spec.Env, "POD_NAME", envSourceMetadataName)
		changed = true
	}

	if namespace != "" && dataplaneServiceName != "" {
		if _, isOverrideDisabled := dontOverride["CONTROLLER_PUBLISH_SERVICE"]; !isOverrideDisabled {
			publishService := controllerPublishService(dataplaneServiceName, namespace)
			if envValueByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE") != publishService {
				spec.Env = updateEnv(spec.Env, "CONTROLLER_PUBLISH_SERVICE", controllerPublishService(dataplaneServiceName, namespace))
				changed = true
			}
		}
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_URL"]; !isOverrideDisabled {
			kongAdminURL := controllerKongAdminURL(dataplaneServiceName, namespace)
			if envValueByName(spec.Env, "CONTROLLER_KONG_ADMIN_URL") != kongAdminURL {
				spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_URL", kongAdminURL)
				changed = true
			}
		}
	}

	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE"]; !isOverrideDisabled {
		spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE", "/var/cluster-certificate/tls.crt")
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE"]; !isOverrideDisabled {
		spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE", "/var/cluster-certificate/tls.key")
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_CA_CERT_FILE"]; !isOverrideDisabled {
		spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_CA_CERT_FILE", "/var/cluster-certificate/ca.crt")
	}

	return changed
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

// envVarSourceByName returns the ValueFrom of the first env var with the given name.
// returns nil if env var is not found, or does not have a ValueFrom field.
func envVarSourceByName(env []corev1.EnvVar, name string) *corev1.EnvVarSource {
	for _, envVar := range env {
		if envVar.Name == name {
			return envVar.ValueFrom
		}
	}
	return nil
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

// updateEnvSource updates env var with `name` to come from `envSource`.
func updateEnvSource(envVars []corev1.EnvVar, name string, envSource *corev1.EnvVarSource) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	for _, envVar := range envVars {
		if envVar.Name != name {
			newEnvVars = append(newEnvVars, envVar)
		}
	}

	newEnvVars = append(newEnvVars, corev1.EnvVar{
		Name:      name,
		ValueFrom: envSource,
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

func generateControlPlaneImage(controlplane *operatorv1alpha1.ControlPlane) string {

	if controlplane.Spec.ContainerImage != nil {
		controlplaneImage := *controlplane.Spec.ContainerImage
		if controlplane.Spec.Version != nil {
			controlplaneImage = fmt.Sprintf("%s:%s", controlplaneImage, *controlplane.Spec.Version)
		}
		return controlplaneImage
	}

	if relatedKongControllerImage := os.Getenv("RELATED_IMAGE_KONG_CONTROLLER"); relatedKongControllerImage != "" {
		// RELATED_IMAGE_KONG_CONTROLLER is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongControllerImage
	}

	return consts.DefaultControlPlaneImage // TODO: https://github.com/Kong/gateway-operator/issues/20
}

func generateNewDeploymentForControlPlane(controlplane *operatorv1alpha1.ControlPlane, serviceAccountName,
	certSecretName string) *appsv1.Deployment {
	controlplaneImage := generateControlPlaneImage(controlplane)

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
					ServiceAccountName: serviceAccountName,
					Volumes: []corev1.Volume{
						{
							Name: "cluster-certificate",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: certSecretName,
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
						},
					},
					Containers: []corev1.Container{{
						Name:            consts.ControlPlaneControllerContainerName,
						Env:             controlplane.Spec.Env,
						EnvFrom:         controlplane.Spec.EnvFrom,
						Image:           controlplaneImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "cluster-certificate",
								ReadOnly:  true,
								MountPath: "/var/cluster-certificate",
							},
						},
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

func addLabelForControlPlane(obj client.Object) {
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
