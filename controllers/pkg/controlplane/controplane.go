package controlplane

import (
	"fmt"
	"os"
	"reflect"

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
	ControlPlaneName            string
	DataplaneIngressServiceName string
	DataplaneAdminServiceName   string
	ManagedByGateway            bool
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
			publishServiceNN := k8stypes.NamespacedName{Namespace: args.Namespace, Name: args.DataplaneIngressServiceName}.String()
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_PUBLISH_SERVICE") != publishServiceNN {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_PUBLISH_SERVICE", publishServiceNN)
				changed = true
			}
		}
	}

	if args.Namespace != "" && args.DataplaneAdminServiceName != "" {
		dataPlaneAdminServiceNN := k8stypes.NamespacedName{Namespace: args.Namespace, Name: args.DataplaneAdminServiceName}.String()
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_SVC"]; !isOverrideDisabled {
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_SVC") != dataPlaneAdminServiceNN {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_SVC", dataPlaneAdminServiceNN)
				changed = true
			}
		}

		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES"]; !isOverrideDisabled {
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES") != consts.DataPlaneAdminServicePortName {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES", consts.DataPlaneAdminServicePortName)
				changed = true
			}
		}
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY"]; !isOverrideDisabled {
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY") != consts.DataPlaneServiceDNSDiscoveryStrategy {
			container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY", consts.DataPlaneServiceDNSDiscoveryStrategy)
			changed = true
		}
	}

	// If the controlplane is managed by a gateway, the controlplane may take some time to properly connect to the dataplane,
	// as the controlplane and the dataplane are deployed together. For this reason, we set the env var CONTROLLER_KONG_ADMIN_INIT_RETRY_DELAY
	// to 5s (the default value is 1s) to:
	// - reduce spamming of "retrying connection to the dataplane i/60";
	// - avoid crash of the controlplane pod when the dataplane is particularly slow to start (it happens quite rarely).
	if args.ManagedByGateway {
		if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_INIT_RETRY_DELAY"]; !isOverrideDisabled {
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_INIT_RETRY_DELAY") != consts.DataPlaneInitRetryDelay {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_INIT_RETRY_DELAY", consts.DataPlaneInitRetryDelay)
				changed = true
			}
		}
	}

	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE"]; !isOverrideDisabled {
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE") != consts.TLSCRTPath {
			container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE", consts.TLSCRTPath)
			changed = true
		}
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE"]; !isOverrideDisabled {
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE") != consts.TLSKeyPath {
			container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE", consts.TLSKeyPath)
			changed = true
		}
	}
	if _, isOverrideDisabled := dontOverride["CONTROLLER_KONG_ADMIN_CA_CERT_FILE"]; !isOverrideDisabled {
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_KONG_ADMIN_CA_CERT_FILE") != consts.TLSCACRTPath {
			container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_KONG_ADMIN_CA_CERT_FILE", consts.TLSCACRTPath)
			changed = true
		}
	}

	if args.ControlPlaneName != "" {
		electionID := fmt.Sprintf("%s.konghq.com", args.ControlPlaneName)
		if _, isOverrideDisabled := dontOverride["CONTROLLER_ELECTION_ID"]; !isOverrideDisabled {
			if k8sutils.EnvValueByName(container.Env, "CONTROLLER_ELECTION_ID") != electionID {
				container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_ELECTION_ID", electionID)
				changed = true
			}
		}
	}

	k8sutils.SetPodContainer(podSpec, container)

	return changed
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
