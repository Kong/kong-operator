// Package integration_test runs test suite that can be imported in other repositories.
// It bootstraps testing environment with TestMain and runs the whole suite with
// TestIntegration (each test from a test suite it a sub test of this).
package integration_test

import (
	"testing"

	"github.com/kong/gateway-operator/modules/admission"
	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/integration"
)

var testSuiteToRun = integration.GetTestSuite()

func TestMain(m *testing.M) {
	testSuiteToRun = helpers.ParseGoTestFlags(TestIntegration, testSuiteToRun)

	integration.TestMain(
		m,
		integration.DefaultControllerConfigForTests(),
		manager.SetupControllers,
		admission.NewRequestHandler,
	)
}

func TestIntegration(t *testing.T) {
	helpers.RunTestSuite(t, testSuiteToRun)
}
