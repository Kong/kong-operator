package manager

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
)

// IsSSAProviderNeeded reports whether cfg requires the shared SSA
// TypeConverterProvider, i.e. whether any SSA-using controller
// (EventGatewayDataPlane, AIGatewayDataPlane, MCPServer, or the hybridgateway
// Gateway API controllers) is enabled.
func IsSSAProviderNeeded(cfg Config) bool {
	return cfg.KEGDataPlaneControllerEnabled ||
		cfg.AIGatewayDataPlaneControllerEnabled ||
		cfg.FeatureGates.Enabled(FeatureGateMCPServer) ||
		cfg.KonnectControllersEnabled
}

// ssaCRDGroups are the CRD groups whose types are passed to ApplyIfChanged /
// ApplyStatusIfChanged by an SSA-using controller: KegDataPlane itself, the
// EventGatewayDataPlaneCertificate it creates, and the KonnectEventGateway it
// references (all owned by the EventGateway DataPlane controller). MCPServer,
// the only other SSA-using controller, needs no CRD-group schemas at all
// (only the core/apps built-ins), so no other groups belong here.
// AIGatewayDataPlane adds aigateway.konghq.com for its own status patches and
// configuration.konghq.com for AIGatewayDataPlaneCertificate objects.
// The hybridgateway Gateway API controllers (gated by KonnectControllersEnabled)
// also rely on configuration.konghq.com (KongRoute, KongService, KongUpstream,
// KongTarget, KongPlugin, KongPluginBinding, KongCertificate, KongSNI) and
// konnect.konghq.com for their server-side apply schema, both already present
// below.
var ssaCRDGroups = map[string]struct{}{
	"eventgateway.konghq.com":    {},
	"aigateway.konghq.com":       {},
	"configuration.konghq.com":   {},
	"aiconfiguration.konghq.com": {},
	"konnect.konghq.com":         {},
}

// buildSSAProvider constructs and builds the shared SSA TypeConverterProvider.
func buildSSAProvider(ctx context.Context, logger logr.Logger, mgr ctrl.Manager) (*controllerpkgssa.TypeConverterProvider, error) {
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, logger, mgr, ssaCRDGroups)
	if err != nil {
		return nil, fmt.Errorf("failed to build initial SSA TypeConverter: %w", err)
	}
	return ssaProvider, nil
}
