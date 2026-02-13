package gatewayclass

import (
	"context"
	"slices"
	"strings"

	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/pkg/features"

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayapipkg "github.com/kong/kong-operator/pkg/gatewayapi"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// getAcceptedCondition returns the accepted condition for the GatewayClass, with
// the proper status, reason and message.
func getAcceptedCondition(ctx context.Context, cl client.Client, gwc *gatewayv1.GatewayClass) (*metav1.Condition, error) {
	reason := gatewayv1.GatewayClassReasonAccepted
	messages := []string{}
	status := metav1.ConditionFalse

	if gwc.Spec.ParametersRef != nil {
		validRef := true
		if gwc.Spec.ParametersRef.Group != gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group) ||
			gwc.Spec.ParametersRef.Kind != "GatewayConfiguration" {
			reason = gatewayv1.GatewayClassReasonInvalidParameters
			messages = append(messages, "ParametersRef must reference a gateway-operator.konghq.com/GatewayConfiguration")
			validRef = false
		}

		if gwc.Spec.ParametersRef.Namespace == nil {
			reason = gatewayv1.GatewayClassReasonInvalidParameters
			messages = append(messages, "ParametersRef must reference a namespaced resource")
			validRef = false
		}

		if validRef {
			gatewayConfig := operatorv2beta1.GatewayConfiguration{}
			err := cl.Get(ctx, client.ObjectKey{Name: gwc.Spec.ParametersRef.Name, Namespace: string(*gwc.Spec.ParametersRef.Namespace)}, &gatewayConfig)
			if client.IgnoreNotFound(err) != nil {
				return nil, err
			}
			if apierrors.IsNotFound(err) {
				reason = gatewayv1.GatewayClassReasonInvalidParameters
				messages = append(messages, "The referenced GatewayConfiguration does not exist")
			}
		}
	}
	if reason == gatewayv1.GatewayClassReasonAccepted {
		status = metav1.ConditionTrue
		messages = []string{"GatewayClass is accepted"}
	}

	acceptedCondition := k8sutils.NewConditionWithGeneration(
		kcfgconsts.ConditionType(gatewayv1.GatewayClassConditionStatusAccepted),
		status,
		kcfgconsts.ConditionReason(reason),
		strings.Join(messages, ". "),
		gwc.GetGeneration(),
	)

	return &acceptedCondition, nil
}

// setSupportedFeatures sets the supported features in the gatewayClass status.
// The set of supported features depends on the router flavor.
func setSupportedFeatures(ctx context.Context, cl client.Client, gwc *gatewayv1.GatewayClass, gatewayConfig *operatorv2beta1.GatewayConfiguration) error {
	flavor, err := getRouterFlavor(ctx, cl, gatewayConfig)
	if err != nil {
		return err
	}
	feats, err := gatewayapipkg.GetSupportedFeatures(flavor)
	if err != nil {
		return err
	}
	supportedFeatures := feats.UnsortedList()
	slices.Sort(supportedFeatures)
	gwc.Status.SupportedFeatures = lo.Map(supportedFeatures, func(f features.FeatureName, _ int) gatewayv1.SupportedFeature {
		return gatewayv1.SupportedFeature{
			Name: gatewayv1.FeatureName(f),
		}
	})

	return nil
}

// getGatewayConfiguration returns the GatewayConfiguration referenced by the GatewayClass.
func getGatewayConfiguration(ctx context.Context, cl client.Client, gwc *gatewayv1.GatewayClass) (*operatorv2beta1.GatewayConfiguration, error) {
	gatewayConfig := operatorv2beta1.GatewayConfiguration{}

	if gwc.Spec.ParametersRef == nil {
		return nil, nil
	}

	err := cl.Get(ctx, client.ObjectKey{Name: gwc.Spec.ParametersRef.Name, Namespace: string(*gwc.Spec.ParametersRef.Namespace)}, &gatewayConfig)
	if err != nil {
		return nil, err
	}

	return &gatewayConfig, nil
}

// getRouterFlavor returns the router flavor to be used by the GatewayClass. It is inferred by
// the KONG_ROUTER_FLAVOR environment variable in the DataPlane proxy container.
func getRouterFlavor(ctx context.Context, cl client.Client, gatewayConfig *operatorv2beta1.GatewayConfiguration) (consts.RouterFlavor, error) {
	if gatewayConfig == nil ||
		gatewayConfig.Spec.DataPlaneOptions == nil ||
		gatewayConfig.Spec.DataPlaneOptions.Deployment.PodTemplateSpec == nil {
		return consts.DefaultRouterFlavor, nil
	}

	container := k8sutils.GetPodContainerByName(&gatewayConfig.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return consts.DefaultRouterFlavor, nil
	}
	value, found, err := k8sutils.GetEnvValueFromContainer(ctx, container, gatewayConfig.Namespace, consts.RouterFlavorEnvKey, cl)
	if !found {
		value = string(consts.DefaultRouterFlavor)
	}
	return consts.RouterFlavor(value), err
}
