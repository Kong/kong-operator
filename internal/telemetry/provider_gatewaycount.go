package telemetry

import (
	"context"

	telemetryprovider "github.com/kong/kubernetes-telemetry/pkg/provider"
	telemetrytypes "github.com/kong/kubernetes-telemetry/pkg/types"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/internal/utils/gatewayclass"
	gwconfigutils "github.com/kong/kong-operator/v2/internal/utils/gatewayconfig"
)

const (
	// GatewayCountProviderName is the name of the gateway count provider.
	GatewayCountProviderName = "gateways"
	// GatewayCountProviderKind is the kind of the gateway count provider.
	GatewayCountProviderKind = telemetryprovider.Kind("gateway_count")

	// GatewayCountKey is the key for count of all gateways regardless of the controller.
	GatewayCountKey = "k8s_gateways_count"
	// ReconciledGatewayCountKey is the key for count of gateways reconciled by the controller.
	ReconciledGatewayCountKey = "k8s_gateways_reconciled_count"
	// ProgrammedGatewayCountKey is the key for count of gateways successfully configured ("Programmed") by the controller.
	ProgrammedGatewayCountKey = "k8s_gateways_programmed_count"
	// AttachedRouteCountKey is the key for the total count of attached routes to all programmed gateways.
	AttachedRouteCountKey = "k8s_gateways_attached_routes_count"
	// HybridGatewayCountKey is the key for count of Konnect hybrid gateways.
	HybridGatewayCountKey = "konnect_hybrid_gateways_count"
	// ProgrammedHybridGatewayCountKey is the key for count of programmed Konnect hybrid gateways.
	ProgrammedHybridGatewayCountKey = "konnect_hybrid_gateways_programmed_count"
	// HybridGatewayAttachedRouteKey is the total count of attached routes to the programmed hybrid gateways.
	HybridGatewayAttachedRouteKey = "konnect_hybrid_gateways_attached_routes_count"

	defaultPageSize = 1000
)

// gatewayCountProvider is the provider to provide telemetry data of count of reconciled and programmed Gateways,
// and sum of attached routes to the reconciled gateways.
// The data for the Konnect hybrid gateways are collected in the reports separately.
// Examplar reports could be:
//
//	{
//	    "k8s_gateways_count":4,
//	    "k8s_gateways_reconciled_count": 3,
//	    "k8s_gateways_programmed_count": 2,
//	    "k8s_gateways_attached_routes_count": 5,
//	    "konnect_hybrid_gateways_count": 2,
//	    "konnect_hybrid_gateways_programmed_count": 1,
//	    "konnect_hybrid_gateways_attached_routes_count": 3
//	}
//
// The Konnect hybrid gateways are a subset of reconciled gateways, so the number of reconciled/programmed gateways
// and attached routes are always smaller or equal to the metics without "konnect_hybrid" mark.
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
// - Number of total Gateways
// - Number of reconciled Gateways
// - Number of programmed Gateways
// - Sum of attached routes to all programmed gateways
// - Number of reconciled Konnect hybrid gateways
// - Number of programmed Konnect hybrid gateways
// - Sum of attached routes to all Konnect hybrid gateways.
func (p *gatewayCountProvider) Provide(ctx context.Context) (telemetrytypes.ProviderReport, error) {

	var (
		gatewayCount                    int
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
		// Sum up the count of total gateways.
		gatewayCount += len(gatewayList.Items)
		// Check each Gateway and increase the count if the Gateway satisfies the conditions of counting.
		for _, gw := range gatewayList.Items {
			// Check if the gateway is reconciled and continue checking only when it is.
			// The gatewayclass.Get returns error if the controllerName in gatewayclass.spec
			// does not match the controller name of the operator.
			// So when no error is returned, the gateways belonging to the gatewayclass are reconciled by the operator.
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
			if p.konnectEnabled && p.isGatewayHybrid(ctx, &gw, gwc.GatewayClass) {
				hybridGatewayCount++
				if programmed {
					programmedHybridGatewayCount++
					hybridGatewayAttachedRouteCount += attachedRoutesOnGateway
				}
			}
		}

		if continueToken = gatewayList.GetContinue(); continueToken == "" {
			break
		}
	}
	// Collect and return the report.
	report := telemetrytypes.ProviderReport{
		GatewayCountKey:           gatewayCount,
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

// isGatewayHybrid returns true if the gateway is Konnect hybrid gateway.
// Either the GatewayConfiguration in spec.parametersRef of its gatewayclass
// or in its own spec.infrastructure.parametersRef specifies the gateway to be managed by Konnect,
// it returns true.
func (p *gatewayCountProvider) isGatewayHybrid(
	ctx context.Context, gw *gatewayv1.Gateway, gwc *gatewayv1.GatewayClass,
) bool {
	gwConfig, err := gwconfigutils.GetFromParametersRef(ctx, p.cl, gwc.Spec.ParametersRef)
	if err != nil {
		return false
	}
	if gwconfigutils.IsGatewayHybrid(gwConfig) {
		return true
	}
	if gw.Spec.Infrastructure != nil && gw.Spec.Infrastructure.ParametersRef != nil {
		paramRef := &gatewayv1.ParametersReference{
			Group:     gw.Spec.Infrastructure.ParametersRef.Group,
			Kind:      gw.Spec.Infrastructure.ParametersRef.Kind,
			Namespace: new(gatewayv1.Namespace(gw.Namespace)),
			Name:      gw.Spec.Infrastructure.ParametersRef.Name,
		}
		localConfig, err := gwconfigutils.GetFromParametersRef(ctx, p.cl, paramRef)
		if err != nil {
			return false
		}
		if gwconfigutils.IsGatewayHybrid(localConfig) {
			return true
		}
	}
	return false
}
