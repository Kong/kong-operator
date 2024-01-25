//go:build integration_tests || integration_tests_bluegreen

// Package integration_test runs test suite that can be imported in other repositories.
// It bootstraps testing environment with TestMain and runs the whole suite with
// TestIntegration (each test from a test suite it a sub test of this).
package integration_test

import (
	"testing"

	integration "github.com/kong/gateway-operator/test/integration"
)

func TestMain(m *testing.M) {
	integration.TestMain(m)
}

func TestIntegration(t *testing.T) {
	// To run chosen tests, pass their names as an argument instead of
	// integration.GetDefaultTestSuite().
	integration.RunTestSuite(t, integration.GetTestSuite())
}
