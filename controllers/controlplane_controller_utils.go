package controllers

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/blang/semver/v4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/vars"
)

// controlPlaneDefaultsArgs contains the parameters to pass to setControlPlaneDefaults
type controlPlaneDefaultsArgs struct {
	namespace                 string
	dataPlanePodIP            string
	dataplaneProxyServiceName string
	dataplaneAdminServiceName string
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions
// -----------------------------------------------------------------------------

// setControlPlaneDefaults updates the environment variables of control plane
// and returns true if env field is changed.
func setControlPlaneDefaults(
	spec *operatorv1alpha1.ControlPlaneDeploymentOptions,
	dontOverride map[string]struct{},
	devMode bool,
	args controlPlaneDefaultsArgs,
) (bool, error) {
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

	if envValueByName(spec.Env, "CONTROLLER_GATEWAY_API_CONTROLLER_NAME") != vars.ControllerName() {
		spec.Env = updateEnv(spec.Env, "CONTROLLER_GATEWAY_API_CONTROLLER_NAME", vars.ControllerName())
		changed = true
	}

	if args.namespace != "" && args.dataplaneProxyServiceName != "" {
		if _, isOverrideDisabled := dontOverride["CONTROLLER_PUBLISH_SERVICE"]; !isOverrideDisabled {
			publishService := controllerPublishService(args.dataplaneProxyServiceName, args.namespace)
			if envValueByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE") != publishService {
				spec.Env = updateEnv(spec.Env, "CONTROLLER_PUBLISH_SERVICE", controllerPublishService(args.dataplaneProxyServiceName, args.namespace))
				changed = true
			}
		}
	}

	if args.dataPlanePodIP != "" && args.dataplaneAdminServiceName != "" {
		adminURL := controllerKongAdminURL(args.dataPlanePodIP, args.dataplaneAdminServiceName, args.namespace)
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_URL"]; !isOverrideDisabled {
			if envValueByName(spec.Env, "CONTROLLER_KONG_ADMIN_URL") != adminURL {
				spec.Env = updateEnv(spec.Env, "CONTROLLER_KONG_ADMIN_URL", adminURL)
				changed = true
			}
		}
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !devMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneSupported)
	}
	controlPlaneImage, err := generateControlPlaneImage(spec, versionValidationOptions...)
	if err != nil {
		return false, err
	}
	if controlPlaneNeedEnableGatewayFeature(controlPlaneImage) {
		envFeatureGates := envValueByName(spec.Env, "CONTROLLER_FEATURE_GATES")
		if !strings.Contains(envFeatureGates, "Gateway=true") {
			envFeatureGates = envFeatureGates + ",Gateway=true"
			envFeatureGates = strings.TrimPrefix(envFeatureGates, ",")
			spec.Env = updateEnv(spec.Env, "CONTROLLER_FEATURE_GATES", envFeatureGates)
			changed = true
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

	return changed, nil
}

// controlPlaneNeedEnableGatewayFeature returns true if Gateway Feature needs
// to be enabled for a controlplane, false otherwise.
func controlPlaneNeedEnableGatewayFeature(image string) bool {
	parts := strings.Split(image, ":")
	// get tag of the image by last part of ":" separated parts.
	// for example kong/kubernetes-ingresss-controller:2.5.0
	tag := parts[len(parts)-1]
	version, err := semver.ParseTolerant(tag)
	// if we failed to get version from image tag, assume that we need to enable Gateway feature gate.
	if err != nil {
		return true
	}
	// for versions <= 2.6, we need to enable Gateway feature gate.
	if version.LT(semver.Version{Major: 2, Minor: 6}) {
		return true
	}

	return false
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
	} else {
		if envValueByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE") != "" {
			spec.Env = rejectEnvByName(spec.Env, "CONTROLLER_PUBLISH_SERVICE")
			changed = true
		}
	}

	return changed
}

func controllerKongAdminURL(podIP, adminServiceName, podNamespace string) string {
	return fmt.Sprintf("https://%s.%s.%s.svc:%d",
		strings.ReplaceAll(podIP, ".", "-"), adminServiceName, podNamespace, dataplaneutils.DefaultKongAdminPort)
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

func generateControlPlaneImage(opts *operatorv1alpha1.ControlPlaneDeploymentOptions, validators ...versions.VersionValidationOption) (string, error) {
	if opts.ContainerImage != nil {
		controlplaneImage := *opts.ContainerImage
		if opts.Version != nil {
			controlplaneImage = fmt.Sprintf("%s:%s", controlplaneImage, *opts.Version)
		}
		for _, v := range validators {
			if !v(controlplaneImage) {
				return "", fmt.Errorf("unsupported ControlPlane image %s", controlplaneImage)
			}
		}
		return controlplaneImage, nil
	}

	if relatedKongControllerImage := os.Getenv("RELATED_IMAGE_KONG_CONTROLLER"); relatedKongControllerImage != "" {
		// RELATED_IMAGE_KONG_CONTROLLER is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongControllerImage, nil
	}

	return consts.DefaultControlPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator/issues/20
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

func controlplaneSpecDeepEqual(spec1, spec2 *operatorv1alpha1.ControlPlaneDeploymentOptions, envVarsToIgnore ...string) bool {
	if !deploymentOptionsDeepEqual(&spec1.DeploymentOptions, &spec2.DeploymentOptions, envVarsToIgnore...) {
		return false
	}

	if !reflect.DeepEqual(spec1.DataPlane, spec2.DataPlane) {
		return false
	}

	return true
}
