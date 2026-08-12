package configuration_test

import (
	"os"
	"testing"

	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestMain shares a single envtest environment across every test in this
// package instead of starting a fresh etcd+API server pair (and reinstalling
// every CRD) per test. See envtest.RunWithSharedEnvironment.
func TestMain(m *testing.M) {
	os.Exit(envtest.RunWithSharedEnvironment(m))
}
