package isolated

import (
	"context"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
)

// kongEnterpriseUsed reports whether DataPlanes run Kong Enterprise: either TEST_KONG_IMAGE
// points at it or it's not set and the operator's default (Enterprise) image is used.
func kongEnterpriseUsed() bool {
	return testenv.KongImage() == "" || testenv.KongEnterpriseImageUsed()
}

// SkipIfEnterpriseNotEnabled skips the test when Kong Enterprise is not enabled.
func SkipIfEnterpriseNotEnabled(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	if !kongEnterpriseUsed() {
		t.Skip("skipping, Kong enterprise is required")
	}
	return ctx
}

// SkipBrokenKongPluginsRejectedByWebhook skips tests relying on KongPlugins with a broken configuration
// being accepted by the cluster, to exercise the FallbackConfiguration feature. The operator's validating
// webhook (kong.validations.kong.konghq.com), active in this suite, rejects them at admission.
func SkipBrokenKongPluginsRejectedByWebhook(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
	t.Skip("skipping, the operator's validating webhook rejects KongPlugins with a broken configuration that this test relies on")
	return ctx
}
