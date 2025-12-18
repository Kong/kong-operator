package validatingwebhook

import (
	"testing"

	"github.com/kong/kong-operator/test/integration"
)

func TestMain(m *testing.M) {
	integration.Suite(m)
}
