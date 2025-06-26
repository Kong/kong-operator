package conformance

import (
	"os"
	"testing"

	"github.com/kong/kong-operator/pkg/consts"
)

// KongRouterFlavor returns router mode of Kong in tests. Currently supports:
// - `traditional_compatible`
// - `expressions`
func KongRouterFlavor(t *testing.T) consts.RouterFlavor {
	rf := os.Getenv("TEST_KONG_ROUTER_FLAVOR")
	switch {
	case rf == "":
		return consts.RouterFlavorTraditionalCompatible
	case rf == string(consts.RouterFlavorTraditionalCompatible):
		return consts.RouterFlavorTraditionalCompatible
	case rf == string(consts.RouterFlavorExpressions):
		return consts.RouterFlavorExpressions
	case rf == "traditional":
		t.Logf("Kong router flavor 'traditional' is deprecated, please use 'traditional_compatible' instead")
		t.FailNow()
		return ""
	default:
		t.Errorf("unsupported Kong router flavor: %s", rf)
		t.FailNow()
		return ""
	}
}
