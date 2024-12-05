package conformance

import (
	"os"
	"testing"
)

// RouterFlavor represents the flavor of the Kong router.
// ref: https://docs.konghq.com/gateway/latest/reference/configuration/#router_flavor
type RouterFlavor string

const (
	// RouterFlavorTraditionalCompatible is the traditional compatible router flavor.
	RouterFlavorTraditionalCompatible RouterFlavor = "traditional_compatible"
	// RouterFlavorExpressions is the expressions router flavor.
	RouterFlavorExpressions RouterFlavor = "expressions"
)

// KongRouterFlavor returns router mode of Kong in tests. Currently supports:
// - `traditional_compatible`
// - `expressions`
func KongRouterFlavor(t *testing.T) RouterFlavor {
	rf := os.Getenv("TEST_KONG_ROUTER_FLAVOR")
	switch {
	case rf == "":
		return RouterFlavorTraditionalCompatible
	case rf == string(RouterFlavorTraditionalCompatible):
		return RouterFlavorTraditionalCompatible
	case rf == string(RouterFlavorExpressions):
		return RouterFlavorExpressions
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
