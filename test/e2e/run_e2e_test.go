package e2e_test

import (
	"os"
	"testing"

	"github.com/kong/gateway-operator/test/e2e"
	"github.com/kong/gateway-operator/test/helpers"
)

var testSuiteToRun = e2e.GetTestSuite()

func TestMain(m *testing.M) {
	testSuiteToRun = helpers.ParseGoTestFlags(TestE2E, testSuiteToRun)
	code := m.Run()
	if code != 0 {
		os.Exit(code)
	}
}

func TestE2E(t *testing.T) {
	helpers.RunTestSuite(t, e2e.GetTestSuite())
}
