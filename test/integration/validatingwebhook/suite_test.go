package validatingwebhook

import (
	"testing"

	"github.com/kong/kong-operator/v2/test/integration"
)

func TestMain(m *testing.M) {
	integration.Suite(m)
}
