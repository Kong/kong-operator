package manager

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
)

// IsSSAProviderNeeded reports whether cfg requires the shared SSA
// TypeConverterProvider, i.e. whether any SSA-using controller (EventGateway
// DataPlane or MCPServer) is enabled.
func IsSSAProviderNeeded(cfg Config) bool {
	return cfg.KEGDataPlaneControllerEnabled || cfg.FeatureGates.Enabled(FeatureGateMCPServer)
}

// ssaCRDGroups are the CRD groups whose types are passed to ApplyIfChanged /
// ApplyStatusIfChanged by an SSA-using controller: KegDataPlane itself, the
// EventGatewayDataPlaneCertificate it creates, and the KonnectEventGateway it
// references (all owned by the EventGateway DataPlane controller). MCPServer,
// the only other SSA-using controller, needs no CRD-group schemas at all
// (only the core/apps built-ins), so no other groups belong here.
var ssaCRDGroups = map[string]struct{}{
	"eventgateway.konghq.com":  {},
	"configuration.konghq.com": {},
	"konnect.konghq.com":       {},
}

// buildSSAProvider constructs and builds the shared SSA TypeConverterProvider.
func buildSSAProvider(ctx context.Context, logger logr.Logger, mgr ctrl.Manager) (*controllerpkgssa.TypeConverterProvider, error) {
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, logger, mgr, ssaCRDGroups)
	if err != nil {
		return nil, fmt.Errorf("failed to build initial SSA TypeConverter: %w", err)
	}
	return ssaProvider, nil
}
