package controlplane

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8scompare "github.com/kong/gateway-operator/internal/utils/kubernetes/compare"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/vars"
)

// DefaultsArgs contains the parameters to pass to setControlPlaneDefaults
type DefaultsArgs struct {
	Namespace                   string
	DataPlanePodIP              string
	DataplaneIngressServiceName string
	DataplaneAdminServiceName   string
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions
// -----------------------------------------------------------------------------

// SetDefaults updates the environment variables of control plane
// and returns true if env field is changed.
func SetDefaults(
	spec *operatorv1alpha1.ControlPlaneOptions,
	dontOverride map[string]struct{},
	args DefaultsArgs,
) bool {
	changed := false

	// set env POD_NAMESPACE. should be always from `metadata.namespace` of pod.
	envSourceMetadataNamespace := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			APIVersion: "v1",
			FieldPath:  "metadata.namespace",
		},
	}
	if spec.Deployment.PodTemplateSpec == nil {
		spec.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	podSpec := &spec.Deployment.PodTemplateSpec.Spec
	container := k8sutils.GetPodContainerByName(podSpec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		container = &corev1.Container{
			Name: consts.ControlPlaneControllerContainerName,
		}
	}

	if !reflect.DeepEqual(envSourceMetadataNamespace, k8sutils.EnvVarSourceByName(container.Env, "POD_NAMESPACE")) {
		container.Env = k8sutils.UpdateEnvSource(container.Env, "POD_NAMESPACE", envSourceMetadataNamespace)
		changed = true
	}

	// set env POD_NAME. should be always from `metadata.name` of pod.
	envSourceMetadataName := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			APIVersion: "v1",
			FieldPath:  "metadata.name",
		},
	}
	if !reflect.DeepEqual(envSourceMetadataName, k8sutils.EnvVarSourceByName(container.Env, "POD_NAME")) {
		container.Env = k8sutils.UpdateEnvSource(container.Env, "POD_NAME", envSourceMetadataName)
		changed = true
	}

	if ctrlName := vars.ControllerName(); k8sutils.EnvValueByName(container.Env, "CONTROLLER_GATEWAY_API_CONTROLLER_NAME") != ctrlName {
		container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_GATEWAY_API_CONTROLLER_NAME", ctrlName)
		changed = true
	}

	if args.Namespace != "" && args.DataplaneIngressServiceName != "" {
		if _, isOverrideDisabled := dontOverride["CONTROLLER_PUBLISH_SERVICE"]; !isOverrideDisabled {
			publishService := k8stypes.NamespacedName{Namespace: args.Namespace, Name: args.DataplaneIngressServiceName}.String()
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_PUBLISH_SERVICE") != publishService {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_PUBLISH_SERVICE", publishService)
				changed = true
			}
		}
	}

	if args.DataPlanePodIP != "" && args.DataplaneAdminServiceName != "" {
		adminURL := controllerKongAdminURL(args.DataPlanePodIP, args.DataplaneAdminServiceName, args.Namespace)
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_URL"]; !isOverrideDisabled {
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_URL") != adminURL {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_URL", adminURL)
				changed = true
			}
		}
	}

	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE"]; !isOverrideDisabled {
		container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE", "/var/cluster-certificate/tls.crt")
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE"]; !isOverrideDisabled {
		container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE", "/var/cluster-certificate/tls.key")
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_CA_CERT_FILE"]; !isOverrideDisabled {
		container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_CA_CERT_FILE", "/var/cluster-certificate/ca.crt")
	}

	// PodSpec.Containers contains values not pointers so we cannot append here
	// and change the container later on because that won't change the value
	// in the PodSpec.
	// To work around this append here to include all the modifications.
	if changed {
		podSpec.Containers = append(podSpec.Containers, *container)
	}

	return changed
}

func controllerKongAdminURL(podIP, adminServiceName, podNamespace string) string {
	return fmt.Sprintf("https://%s.%s.%s.svc:%d",
		strings.ReplaceAll(podIP, ".", "-"), adminServiceName, podNamespace, consts.DataPlaneAdminAPIPort)
}

// GenerateImage returns the image to use for the control plane.
func GenerateImage(opts *operatorv1alpha1.ControlPlaneOptions, validators ...versions.VersionValidationOption) (string, error) {
	container := k8sutils.GetPodContainerByName(&opts.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		// This is just a safeguard against running the operator without an admission webhook
		// (which would prevent admission of a ControlPlane without an image specified)
		// to prevent panics.
		return "", fmt.Errorf("unsupported ControlPlane without image")
	}
	if container.Image != "" {
		for _, v := range validators {
			supported, err := v(container.Image)
			if err != nil {
				return "", err
			}
			if !supported {
				return "", fmt.Errorf("unsupported ControlPlane image %s", container.Image)
			}
		}
		return container.Image, nil
	}

	if relatedKongControllerImage := os.Getenv("RELATED_IMAGE_KONG_CONTROLLER"); relatedKongControllerImage != "" {
		// RELATED_IMAGE_KONG_CONTROLLER is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongControllerImage, nil
	}

	return consts.DefaultControlPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator/issues/20
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------
func SpecDeepEqual(spec1, spec2 *operatorv1alpha1.ControlPlaneOptions, envVarsToIgnore ...string) bool {
	if !k8scompare.DeploymentOptionsV1AlphaDeepEqual(&spec1.Deployment, &spec2.Deployment, envVarsToIgnore...) ||
		!reflect.DeepEqual(spec1.DataPlane, spec2.DataPlane) {
		return false
	}

	return true
}
