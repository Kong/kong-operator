package isolated

import (
	"context"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/kong/kong-operator/v2/ingress-controller/test/dataplane/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
)

// SkipIfRouterNotExpressions skips the test when the router flavor is not expressions.
func SkipIfRouterNotExpressions(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	flavor := testenv.KongRouterFlavor()
	if flavor != config.RouterFlavorExpressions {
		t.Skipf("skipping, %q router flavor specified via TEST_KONG_ROUTER_FLAVOR env but %q is required", flavor, "expressions")
	}
	return ctx
}

// SkipIfEnterpriseNotEnabled skips the test when Kong Enterprise is not enabled.
func SkipIfEnterpriseNotEnabled(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if !testenv.KongEnterpriseEnabled() {
		t.Skip("skipping, Kong enterprise is required")
	}
	return ctx
}

// SkipIfDBBacked skips the test when DB-backed mode is enabled.
func SkipIfDBBacked(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if testenv.DBMode() != testenv.DBModeOff {
		t.Skip("skipping, DBLess mode is required")
	}
	return ctx
}
