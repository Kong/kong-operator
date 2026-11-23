package isolated

import (
	"context"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
)

// kongEnterpriseUsed reports whether DataPlanes run Kong Enterprise: TEST_KONG_ENTERPRISE
// forces it when set; otherwise either TEST_KONG_IMAGE points at it or it's not set and
// the operator's default (Enterprise) image is used.
func kongEnterpriseUsed() bool {
	if enabled, ok := testenv.KongEnterpriseOverride(); ok {
		return enabled
	}
	return testenv.KongImage() == "" || testenv.KongEnterpriseEnabled()
}

// SkipIfEnterpriseNotEnabled skips the test when Kong Enterprise is not enabled.
func SkipIfEnterpriseNotEnabled(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if !kongEnterpriseUsed() {
		t.Skip("skipping, Kong enterprise is required")
	}
	return ctx
}
