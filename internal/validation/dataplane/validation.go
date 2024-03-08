package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// Validator validates DataPlane objects.
type Validator struct {
	c client.Client
}

// NewValidator creates a DataPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{c: c}
}

// ValidateUpdate validates a DataPlane object change upon an update event.
func (v *Validator) ValidateUpdate(dataplane, oldDataPlane *operatorv1beta1.DataPlane) error {
	return v.ValidateIfRolloutInProgress(dataplane, oldDataPlane)
}

// Validate validates a DataPlane object and return the first validation error found.
func (v *Validator) Validate(dataplane *operatorv1beta1.DataPlane) error {
	err := v.ValidateDataPlaneDeploymentOptions(dataplane.Namespace, &dataplane.Spec.Deployment.DeploymentOptions)
	if err != nil {
		return err
	}

	if err := v.ValidateDataPlaneDeploymentRollout(dataplane.Spec.Deployment.Rollout); err != nil {
		return err
	}

	if dataplane.Spec.Network.Services != nil && dataplane.Spec.Network.Services.Ingress != nil &&
		dataplane.Spec.Deployment.PodTemplateSpec != nil {
		proxyContainer := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		if err := v.ValidateDataPlaneIngressServiceOptions(dataplane.Namespace, dataplane.Spec.Network.Services.Ingress, proxyContainer); err != nil {
			return err
		}
	}

	return nil
}

// ValidateDataPlaneDeploymentRollout validates the Rollout field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentRollout(rollout *operatorv1beta1.Rollout) error {
	if rollout != nil && rollout.Strategy.BlueGreen != nil && rollout.Strategy.BlueGreen.Promotion.Strategy == operatorv1beta1.AutomaticPromotion {
		// Can't use AutomaticPromotion just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1006.
		return errors.New("DataPlane AutomaticPromotion cannot be used yet")
	}

	if rollout != nil && rollout.Strategy.BlueGreen != nil &&
		rollout.Strategy.BlueGreen.Resources.Plan.Deployment == operatorv1beta1.RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout {
		// Can't use DeleteOnPromotionRecreateOnRollout just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1010.
		return errors.New("DataPlane Deployment resource plan DeleteOnPromotionRecreateOnRollout cannot be used yet")
	}

	return nil
}

func (v *Validator) ValidateIfRolloutInProgress(dataplane, oldDataPlane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.RolloutStatus == nil {
		return nil
	}

	// If no rollout condition exists, the rollout is not started yet
	c, exists := k8sutils.GetCondition(consts.DataPlaneConditionTypeRolledOut, dataplane.Status.RolloutStatus)
	if !exists {
		return nil
	}

	// If the promotion is in progress and the spec is changed in the update
	// then reject the change.
	if c.Reason == string(consts.DataPlaneConditionReasonRolloutPromotionInProgress) &&
		!cmp.Equal(dataplane.Spec, oldDataPlane.Spec) {
		return ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion
	}

	return nil
}

// ValidateDataPlaneDeploymentOptions validates the DeploymentOptions field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentOptions(namespace string, opts *operatorv1beta1.DeploymentOptions) error {
	if opts == nil || opts.PodTemplateSpec == nil {
		// Can't use empty DeploymentOptions because we still require users
		// to provide an image
		// Related: https://github.com/Kong/gateway-operator/issues/754.
		return errors.New("DataPlane requires an image")
	}

	// Until https://github.com/Kong/gateway-operator/issues/20 is resolved we
	// require DataPlanes that they are provided with image and version set.
	// Related: https://github.com/Kong/gateway-operator/issues/754.
	container := k8sutils.GetPodContainerByName(&opts.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return fmt.Errorf("couldn't find proxy container in DataPlane spec")
	}

	if container.Image == "" {
		return errors.New("DataPlane requires an image")
	}

	// validate db mode.
	dbMode, _, err := k8sutils.GetEnvValueFromContainer(context.Background(), container, namespace, consts.EnvVarKongDatabase, v.c)
	if err != nil {
		return err
	}

	// only support dbless mode.
	if dbMode != "" && dbMode != "off" {
		return fmt.Errorf("database backend %s of DataPlane not supported currently", dbMode)
	}

	return nil
}

// ValidateDataPlaneIngressServiceOptions validates spec.serviceOptions of given DataPlane.
func (v *Validator) ValidateDataPlaneIngressServiceOptions(
	namespace string, opts *operatorv1beta1.DataPlaneServiceOptions, proxyContainer *corev1.Container,
) error {
	if len(opts.Ports) > 0 {
		kongPortMaps, hasKongPortMaps, err := k8sutils.GetEnvValueFromContainer(context.Background(), proxyContainer, namespace, "KONG_PORT_MAPS", v.c)
		if err != nil {
			return err
		}
		kongProxyListen, hasProxyListen, err := k8sutils.GetEnvValueFromContainer(context.Background(), proxyContainer, namespace, "KONG_PROXY_LISTEN", v.c)
		if err != nil {
			return err
		}

		var portNumberMap map[int32]int32 = make(map[int32]int32, 0)
		if hasKongPortMaps {
			portNumberMap, err = parseKongPortMaps(kongPortMaps)
			if err != nil {
				return err
			}

		}

		var listenPortNumbers []int32 = make([]int32, 0)
		if hasProxyListen {
			listenPortNumbers, err = parseKongProxyListenPortNumbers(kongProxyListen)
			if err != nil {
				return err
			}

		}

		for _, port := range opts.Ports {
			targetPortNumber, err := getTargetPortNumber(port.TargetPort, proxyContainer)
			if err != nil {
				return fmt.Errorf("failed to get target port of port %d (port name %s) of ingress service: %w",
					port.Port, port.Name, err)
			}
			if hasKongPortMaps && portNumberMap[port.Port] != targetPortNumber {
				return fmt.Errorf("KONG_PORT_MAPS specified but target port %s not properly set", port.TargetPort.String())
			}
			if hasProxyListen && !lo.Contains(listenPortNumbers, targetPortNumber) {
				return fmt.Errorf("target port %s not included in KONG_PROXY_LISTEN", port.TargetPort.String())
			}
		}
	}

	return nil
}

func getTargetPortNumber(targetPort intstr.IntOrString, container *corev1.Container) (int32, error) {
	switch targetPort.Type {
	case intstr.Int:
		return targetPort.IntVal, nil
	case intstr.String:
		for _, containerPort := range container.Ports {
			if containerPort.Name == targetPort.StrVal {
				return containerPort.ContainerPort, nil
			}
		}
		return 0, fmt.Errorf("port %s not found in container", targetPort.StrVal)
	}

	return 0, fmt.Errorf("unknown targetPort Type: %v", targetPort.Type)
}

// parseKongPortMaps parses port maps specified in `proxy_maps` configuration.
// and returns a map with expose port -> listening port.
// For example, "80:8000,443:8443" will be parsed into map{80:8000,443:8443}.
func parseKongPortMaps(kongPortMapEnv string) (map[int32]int32, error) {
	portMaps := strings.Split(kongPortMapEnv, ",")
	portNumberMap := map[int32]int32{}
	for _, port := range portMaps {
		parts := strings.SplitN(port, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("port map item %s cannot be parsed into 'port:port' format", port)
		}
		servicePort, err := strconv.ParseInt(parts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("port %s cannot be parsed into number: %w", parts[0], err)
		}
		targetPort, err := strconv.ParseInt(parts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("port %s cannot be parsed into number: %w", parts[1], err)
		}
		portNumberMap[int32(servicePort)] = int32(targetPort)
	}
	return portNumberMap, nil
}

// parseKongProxyListenPortNumbers parses `proxy_listen` configuration to listening ports.
// It returns the list of listening port numbers.  For example,
// `"0.0.0.0:8000 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384`
// will be parsed into []int32{8000,8443}.
func parseKongProxyListenPortNumbers(kongProxyListenEnv string) ([]int32, error) {
	listenAddresses := strings.Split(kongProxyListenEnv, ",")
	retPorts := make([]int32, 0, len(listenAddresses))
	for _, addr := range listenAddresses {
		addr = strings.Trim(addr, " ")
		// The splitted single listen address would be a list of strings starting with the host and port
		// and following with options of listening separated by spaces, like `0.0.0.0:8000 reuseport backlog=16384`.
		// So we extract the part before the first space as the host and port.
		// It is possible that the listen port have only one part like `0.0.0.0:8000` so we do not check presence of space.
		hostPort, _, _ := strings.Cut(addr, " ")
		_, port, err := net.SplitHostPort(hostPort)
		if err != nil {
			return nil, fmt.Errorf("listening address %s cannot be parsed into host:port format: %w", hostPort, err)
		}
		portNum, err := strconv.ParseInt(port, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("listening port %s cannot be parsed to number: %w", port, err)
		}
		retPorts = append(retPorts, int32(portNum))
	}
	return retPorts, nil
}
