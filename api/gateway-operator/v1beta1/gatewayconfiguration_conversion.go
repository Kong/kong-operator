package v1beta1

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
)

const (
	errWrongConvertToGatewayConfiguration   = "GatewayConfiguration ConvertTo: expected *operatorv2beta1.GatewayConfiguration, got %T"
	errWrongConvertFromGatewayConfiguration = "GatewayConfiguration ConvertFrom: expected *operatorv2beta1.GatewayConfiguration, got %T"
	errControlPlaneDataPlaneNotSupported    = "GatewayConfiguration ConvertTo: ControlPlaneOptions.DataPlane is not supported"
)

// ConvertTo converts this GatewayConfiguration (v1beta1) to the Hub version.
func (g *GatewayConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*operatorv2beta1.GatewayConfiguration)
	if !ok {
		return fmt.Errorf(errWrongConvertToGatewayConfiguration, dstRaw)
	}
	dst.ObjectMeta = g.ObjectMeta
	var extensions = g.Spec.Extensions

	if g.Spec.DataPlaneOptions != nil {

		dst.Spec.DataPlaneOptions = gatewayConfigDataPlaneOptionsV1ToV2(g.Spec.DataPlaneOptions)
		extensions = append(extensions, g.Spec.DataPlaneOptions.Extensions...)
	}

	if err := g.convertControlPlaneOptions(dst, &extensions); err != nil {
		return err
	}

	// Remove all the duplicates in KonnectExtension references.
	// An example is the KonnectExtension being manually set in the ControlPlaneOptions
	// and DataPlaneOptions.
	extensions = lo.FindUniques(extensions)
	if len(extensions) == 0 {
		extensions = nil
	}

	dst.Spec.ListenersOptions = listenersOptionsV1ToV2(g.Spec.ListenersOptions)
	dst.Spec.Extensions = extensions
	dst.Status = operatorv2beta1.GatewayConfigurationStatus(g.Status)

	return nil
}

// convertControlPlaneOptions converts the ControlPlaneOptions from v1beta1 to v2beta1.
func (g *GatewayConfiguration) convertControlPlaneOptions(dst *operatorv2beta1.GatewayConfiguration, extensions *[]commonv1alpha1.ExtensionRef) error {
	if g.Spec.ControlPlaneOptions != nil {
		if g.Spec.ControlPlaneOptions.DataPlane != nil {
			return errors.New(errControlPlaneDataPlaneNotSupported)
		}
		// There is no IngressClass field in GatewayConfiguration v1beta1, so we need to extract it from the pod template spec.
		var ingressClass *string
		if g.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec != nil &&
			len(g.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec.Spec.Containers) > 0 {
			container, found := lo.Find(g.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec.Spec.Containers, func(c corev1.Container) bool {
				return c.Name == "controller"
			})
			if found {
				ingressClass = ingressClassFormatFromEnvVars(container.Env)
			}
		}
		dst.Spec.ControlPlaneOptions = &operatorv2beta1.GatewayConfigControlPlaneOptions{}
		if err := g.Spec.ControlPlaneOptions.convertTo(&dst.Spec.ControlPlaneOptions.ControlPlaneOptions, ingressClass); err != nil {
			return err
		}
		*extensions = append(*extensions, g.Spec.ControlPlaneOptions.Extensions...)
	}
	return nil
}

// ConvertFrom converts from the Hub version to this version (v1beta1).
func (g *GatewayConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*operatorv2beta1.GatewayConfiguration)
	if !ok {
		return fmt.Errorf(errWrongConvertFromGatewayConfiguration, srcRaw)
	}
	g.ObjectMeta = src.ObjectMeta
	g.Spec.Extensions = lo.Filter(src.Spec.Extensions, func(ext commonv1alpha1.ExtensionRef, _ int) bool {
		return ext.Group == "konnect.konghq.com" && ext.Kind == "KonnectExtension"
	})
	if src.Spec.DataPlaneOptions != nil {
		g.Spec.DataPlaneOptions = gatewayConfigDataPlaneOptionsV2ToV1(src.Spec.DataPlaneOptions)
	}

	if src.Spec.ControlPlaneOptions != nil {
		g.Spec.ControlPlaneOptions = &ControlPlaneOptions{}
		g.Spec.ControlPlaneOptions.convertFrom(src.Spec.ControlPlaneOptions.ControlPlaneOptions,
			func(ev *[]corev1.EnvVar) {
				*ev = append(*ev, envVarFromIngressClass(src.Spec.ControlPlaneOptions.IngressClass)...)
			},
		)
		g.Spec.ControlPlaneOptions.Extensions = lo.Filter(src.Spec.Extensions, func(ext commonv1alpha1.ExtensionRef, _ int) bool {
			return ext.Group == SchemeGroupVersion.Group && ext.Kind == "DataPlaneMetricsExtension"
		})
	}

	g.Status = GatewayConfigurationStatus(src.Status)

	return nil
}

// gatewayConfigDataPlaneOptionsV2ToV1 converts operatorv2beta1.GatewayConfigDataPlaneOptions to GatewayConfigDataPlaneOptions.
func gatewayConfigDataPlaneOptionsV2ToV1(o *operatorv2beta1.GatewayConfigDataPlaneOptions) *GatewayConfigDataPlaneOptions {
	if o == nil {
		return nil
	}

	// Convert operatorv2beta1.DataPlaneDeploymentOptions to DataPlaneDeploymentOptions
	deployment := DataPlaneDeploymentOptions{
		DeploymentOptions: DeploymentOptions{
			Replicas:        o.Deployment.Replicas,
			PodTemplateSpec: o.Deployment.PodTemplateSpec,
		},
	}
	if o.Deployment.Rollout != nil {
		deployment.Rollout = &Rollout{
			Strategy: RolloutStrategy{
				BlueGreen: &BlueGreenStrategy{},
			},
		}
		if o.Deployment.Rollout.Strategy.BlueGreen.Promotion != nil &&
			o.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy != nil {
			deployment.Rollout.Strategy.BlueGreen.Promotion = Promotion{
				Strategy: PromotionStrategy(*o.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy),
			}
		}
		if o.Deployment.Rollout.Strategy.BlueGreen.Resources != nil {
			deployment.Rollout.Strategy.BlueGreen.Resources = RolloutResources{
				Plan: RolloutResourcePlan{
					Deployment: RolloutResourcePlanDeployment(o.Deployment.Rollout.Strategy.BlueGreen.Resources.Plan.Deployment),
				},
			}
		}
	}

	// Convert operatorv2beta1.GatewayConfigDataPlaneNetworkOptions to GatewayConfigDataPlaneNetworkOptions
	network := GatewayConfigDataPlaneNetworkOptions{}
	if o.Network.Services != nil && o.Network.Services.Ingress != nil {
		network.Services = &GatewayConfigDataPlaneServices{
			Ingress: &GatewayConfigServiceOptions{
				ServiceOptions: ServiceOptions{
					Type:                  o.Network.Services.Ingress.Type,
					Name:                  o.Network.Services.Ingress.Name,
					Annotations:           o.Network.Services.Ingress.Annotations,
					ExternalTrafficPolicy: o.Network.Services.Ingress.ExternalTrafficPolicy,
				},
			},
		}
	}

	// Convert operatorv2beta1.GatewayConfigDataPlaneResources to GatewayConfigDataPlaneResources
	var resources *GatewayConfigDataPlaneResources
	if o.Resources != nil && o.Resources.PodDisruptionBudget != nil {
		resources = &GatewayConfigDataPlaneResources{
			PodDisruptionBudget: &PodDisruptionBudget{
				Spec: PodDisruptionBudgetSpec{
					MinAvailable:               o.Resources.PodDisruptionBudget.Spec.MinAvailable,
					MaxUnavailable:             o.Resources.PodDisruptionBudget.Spec.MaxUnavailable,
					UnhealthyPodEvictionPolicy: o.Resources.PodDisruptionBudget.Spec.UnhealthyPodEvictionPolicy,
				},
			},
		}
	}

	// Convert []operatorv2beta1.NamespacedName to []NamespacedName
	var pluginsToInstall []NamespacedName
	if len(o.PluginsToInstall) > 0 {
		pluginsToInstall = make([]NamespacedName, 0, len(o.PluginsToInstall))
	}
	for _, plugin := range o.PluginsToInstall {
		pluginsToInstall = append(pluginsToInstall, NamespacedName{
			Name:      plugin.Name,
			Namespace: plugin.Namespace,
		})
	}

	return &GatewayConfigDataPlaneOptions{
		Deployment:       deployment,
		Network:          network,
		Resources:        resources,
		PluginsToInstall: pluginsToInstall,
	}
}

// gatewayConfigDataPlaneOptionsV1ToV2 converts GatewayConfigDataPlaneOptions to operatorv2beta1.GatewayConfigDataPlaneOptions.
func gatewayConfigDataPlaneOptionsV1ToV2(o *GatewayConfigDataPlaneOptions) *operatorv2beta1.GatewayConfigDataPlaneOptions {
	if o == nil {
		return nil
	}

	// Convert DataPlaneDeploymentOptions to operatorv2beta1.DataPlaneDeploymentOptions
	deployment := operatorv2beta1.DataPlaneDeploymentOptions{
		DeploymentOptions: operatorv2beta1.DeploymentOptions{
			Replicas:        o.Deployment.Replicas,
			PodTemplateSpec: o.Deployment.PodTemplateSpec,
		},
	}
	if o.Deployment.Rollout != nil &&
		o.Deployment.Rollout.Strategy.BlueGreen != nil {
		deployment.Rollout = &operatorv2beta1.Rollout{
			Strategy: operatorv2beta1.RolloutStrategy{
				BlueGreen: operatorv2beta1.BlueGreenStrategy{
					Promotion: &operatorv2beta1.Promotion{
						Strategy: lo.ToPtr(operatorv2beta1.PromotionStrategy(o.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy)),
					},
					Resources: &operatorv2beta1.RolloutResources{
						Plan: operatorv2beta1.RolloutResourcePlan{
							Deployment: operatorv2beta1.RolloutResourcePlanDeployment(o.Deployment.Rollout.Strategy.BlueGreen.Resources.Plan.Deployment),
						},
					},
				},
			},
		}
	}

	// Convert GatewayConfigDataPlaneResources to operatorv2beta1.GatewayConfigDataPlaneResources
	var resources *operatorv2beta1.GatewayConfigDataPlaneResources
	if o.Resources != nil && o.Resources.PodDisruptionBudget != nil {
		resources = &operatorv2beta1.GatewayConfigDataPlaneResources{
			PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
				Spec: operatorv2beta1.PodDisruptionBudgetSpec{
					MinAvailable:               o.Resources.PodDisruptionBudget.Spec.MinAvailable,
					MaxUnavailable:             o.Resources.PodDisruptionBudget.Spec.MaxUnavailable,
					UnhealthyPodEvictionPolicy: o.Resources.PodDisruptionBudget.Spec.UnhealthyPodEvictionPolicy,
				},
			},
		}
	}

	// Convert GatewayConfigDataPlaneNetworkOptions to operatorv2beta1.GatewayConfigDataPlaneNetworkOptions
	network := operatorv2beta1.GatewayConfigDataPlaneNetworkOptions{}
	if o.Network.Services != nil && o.Network.Services.Ingress != nil {
		network.Services = &operatorv2beta1.GatewayConfigDataPlaneServices{
			Ingress: &operatorv2beta1.GatewayConfigServiceOptions{
				ServiceOptions: operatorv2beta1.ServiceOptions{
					Type:                  o.Network.Services.Ingress.Type,
					Name:                  o.Network.Services.Ingress.Name,
					Annotations:           o.Network.Services.Ingress.Annotations,
					ExternalTrafficPolicy: o.Network.Services.Ingress.ExternalTrafficPolicy,
				},
			},
		}
	}

	// Convert []NamespacedName to []operatorv2beta1.NamespacedName
	var pluginsToInstall []operatorv2beta1.NamespacedName
	if len(o.PluginsToInstall) > 0 {
		pluginsToInstall = make([]operatorv2beta1.NamespacedName, 0, len(o.PluginsToInstall))
	}
	for _, plugin := range o.PluginsToInstall {
		pluginsToInstall = append(pluginsToInstall, operatorv2beta1.NamespacedName{
			Name:      plugin.Name,
			Namespace: plugin.Namespace,
		})
	}

	return &operatorv2beta1.GatewayConfigDataPlaneOptions{
		Deployment:       deployment,
		Resources:        resources,
		Network:          network,
		PluginsToInstall: pluginsToInstall,
	}
}

// listenersOptionsV1ToV2 converts GatewayConfigurationListenerOptions to operatorv2beta1.GatewayConfigurationListenerOptions.
func listenersOptionsV1ToV2(o []GatewayConfigurationListenerOptions) []operatorv2beta1.GatewayConfigurationListenerOptions {
	if o == nil {
		return nil
	}

	options := make([]operatorv2beta1.GatewayConfigurationListenerOptions, 0, len(o))
	for _, listenerOpt := range o {
		options = append(options, operatorv2beta1.GatewayConfigurationListenerOptions{
			Name:     listenerOpt.Name,
			NodePort: listenerOpt.NodePort,
		})
	}

	return options
}
