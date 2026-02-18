//go:build integration_tests

package isolated

import (
	"context"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	dpconf "github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/internal/testenv"
)

func SkipIfRouterNotExpressions(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	flavor := testenv.KongRouterFlavor()
	if flavor != dpconf.RouterFlavorExpressions {
		t.Skipf("skiping, %q router flavor specified via TEST_KONG_ROUTER_FLAVOR env but %q is required", flavor, "expressions")
	}
	return ctx
}

func SkipIfEnterpriseNotEnabled(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if !testenv.KongEnterpriseEnabled() {
		t.Skip("skipping, Kong enterprise is required")
	}
	return ctx
}

func SkipIfDBBacked(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if testenv.DBMode() != testenv.DBModeOff {
		t.Skip("skipping, DBLess mode is required")
	}
	return ctx
}
