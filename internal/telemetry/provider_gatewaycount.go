package telemetry

import (
	"context"
	"errors"
	"fmt"

	telemetryprovider "github.com/kong/kubernetes-telemetry/pkg/provider"
	telemetrytypes "github.com/kong/kubernetes-telemetry/pkg/types"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/internal/utils/gatewayclass"
)

const (
	// GatewayCountProviderName is the name of the gateway count provider.
	GatewayCountProviderName = "gateways"
	// GatewayCountProviderKind is the kind of the gateway count provider.
	GatewayCountProviderKind = telemetryprovider.Kind("gateway_count")

	// ReconciledGatewayCountKey is the key for count of gateways reconciled by the controller.
	ReconciledGatewayCountKey = "k8s_gateways_reconciled_count"
	// ProgrammedGatewayCountKey is the key for count of gateways successfully configured ("Programmed") by the controller.
	ProgrammedGatewayCountKey = "k8s_gateway_programmed_count"
	// AttachedRouteCountKey is the key for the total count of attached routes to all programmed gateways.
	AttachedRouteCountKey = "k8s_gateway_attached_route_count"
	// HybridGatewayCountKey is the key for count of Konnect hybrid gateways.
	HybridGatewayCountKey = "konnect_hybrid_gateway_count"
	// ProgrammedHybridGatewayCountKey is the key for count of programmed Konnect hybrid gateways.
	ProgrammedHybridGatewayCountKey = "konnect_hybrid_gateway_programmed_count"
	// HybridGatewayAttachedRouteKey is the total count of attached routes to the programmed hybrid gateways.
	HybridGatewayAttachedRouteKey = "konnect_hybrid_gateway_attached_route_count"

	defaultPageSize = 1000
)

// gatewayCountProvider is the provider to provide telemetry data of count of reconciled and programmed Gateways.
type gatewayCountProvider struct {
	konnectEnabled bool

	cl client.Client
}

var _ telemetryprovider.Provider = &gatewayCountProvider{}

// Name returns the name of the Gateway count provider.
func (p *gatewayCountProvider) Name() string {
	return GatewayCountProviderName
}

// Kind returns the kind of the Gateway count provider.
func (p *gatewayCountProvider) Kind() telemetryprovider.Kind {
	return GatewayCountProviderKind
}

// Provide provides the telemetry data when anonymous reports are enabled on the reconciler, including:
// - Number of reconciled Gateways
// - Number of programmed Gateways
// - Sum of attached routes to all programmed gateways
// - Number of reconciled Konnect hybrid gateways
// - Number of programmed Konnect hybrid gateways
// - Sum of attached routes to all Konnect hybrid gateways
func (p *gatewayCountProvider) Provide(ctx context.Context) (telemetrytypes.ProviderReport, error) {

	var (
		reconciledGatewayCount          int
		programmedGatewayCount          int
		attachedRouteCount              int
		hybridGatewayCount              int
		programmedHybridGatewayCount    int
		hybridGatewayAttachedRouteCount int

		continueToken string
	)

	// List all gateways and count the number of each type required for telemetry data.
	for {
		gatewayList := gatewayv1.GatewayList{}
		err := p.cl.List(ctx, &gatewayList, &client.ListOptions{
			Limit:    defaultPageSize,
			Continue: continueToken,
		})

		if err != nil {
			return nil, err
		}
		// Check each Gateway and increase the count if the Gateway satisfies the conditions of counting.
		for _, gw := range gatewayList.Items {
			// Check if the gateway is reconciled and continue checking only when it is.
			gwc, err := gatewayclass.Get(ctx, p.cl, string(gw.Spec.GatewayClassName))
			if err != nil {
				continue
			}
			reconciledGatewayCount++
			// Check if the Gateway is programmed.
			var attachedRoutesOnGateway int
			programmed := lo.ContainsBy(gw.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == "Programmed" && condition.Status == metav1.ConditionTrue
			})
			if programmed {
				programmedGatewayCount++
				attachedRoutesOnGateway = lo.SumBy(gw.Status.Listeners, func(l gatewayv1.ListenerStatus) int {
					return int(l.AttachedRoutes)
				})
				attachedRouteCount += attachedRoutesOnGateway
			}
			// Check if the gateway is Konnect hybrid gateway only when Konnect controller is enabled.
			if p.konnectEnabled {
				gwConfig, err := p.getGatewayConfigForGatewayClass(ctx, gwc.GatewayClass)
				if err != nil {
					continue
				}
				// Count the hybrid gateway if the GatewayConfiguration specifies a Konnect API auth config.
				if gwConfig.Spec.Konnect != nil && gwConfig.Spec.Konnect.APIAuthConfigurationRef != nil {
					hybridGatewayCount++
					if programmed {
						programmedHybridGatewayCount++
						hybridGatewayAttachedRouteCount += attachedRoutesOnGateway
					}
				}
			}
		}

		if continueToken = gatewayList.GetContinue(); continueToken == "" {
			break
		}
	}
	// Collect and return the report.
	report := telemetrytypes.ProviderReport{
		ReconciledGatewayCountKey: reconciledGatewayCount,
		ProgrammedGatewayCountKey: programmedGatewayCount,
		AttachedRouteCountKey:     attachedRouteCount,
	}
	if p.konnectEnabled {
		report[HybridGatewayCountKey] = hybridGatewayCount
		report[ProgrammedHybridGatewayCountKey] = programmedHybridGatewayCount
		report[HybridGatewayAttachedRouteKey] = hybridGatewayAttachedRouteCount
	}
	return report, nil
}

// getGatewayConfigForGatewayClass gets the GatewayConfiguration specified for the given GatewayClass.
// If the spec.parametersRef is empty, it returns a default GatewayConfiguration.
// If the kind in spec.parametersRef does not match, or namespace/name is not specified, it returns error.
// TODO: DRY -- extract the getGatewayConfigForParametersRef in gateway reconciler elsewhere and reuse it.
func (p *gatewayCountProvider) getGatewayConfigForGatewayClass(
	ctx context.Context, gwc *gatewayv1.GatewayClass,
) (operatorv2beta1.GatewayConfiguration, error) {
	if gwc.Spec.ParametersRef == nil {
		return operatorv2beta1.GatewayConfiguration{}, nil
	}
	parametersRef := gwc.Spec.ParametersRef
	if parametersRef.Group != gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group) ||
		parametersRef.Kind != "GatewayConfiguration" {
		return operatorv2beta1.GatewayConfiguration{},
			fmt.Errorf("controller only supports %s %s resources for GatewayClass parametersRef",
				operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration")
	}
	if parametersRef.Namespace == nil {
		return operatorv2beta1.GatewayConfiguration{}, errors.New("namespace must be specified")
	}
	if parametersRef.Name == "" {
		return operatorv2beta1.GatewayConfiguration{}, errors.New("name must be specified")
	}

	nn := client.ObjectKey{
		Namespace: string(*parametersRef.Namespace),
		Name:      parametersRef.Name,
	}
	gwConfig := operatorv2beta1.GatewayConfiguration{}
	err := p.cl.Get(ctx, nn, &gwConfig)
	return gwConfig, err
}
