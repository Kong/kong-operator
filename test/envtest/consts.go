package envtest

import (
	"github.com/kong/kong-operator/v2/test/envtest/consts"
)

// This file is mostly serving one purpose:
// to allow gradual refactoring of envtest suite.
// Currently, tests living in test/envtest use these consts. Changing this to
// use consts fro test/envtest/consts/ would require changing a lot of files at once.
// We want to avoid that by using this file and moving the source of truth to test/envtest/consts/.

const (
	// konnectInfiniteSyncTime is used for tests that want to verify behavior of the reconcilers not relying on the fixed
	// sync period. It's set to 60m that is virtually infinite for the tests.
	konnectInfiniteSyncTime = consts.KonnectInfiniteSyncTime

	// waitTime is a generic wait time for the tests' eventual conditions.
	waitTime = consts.WaitTime

	// tickTime is a generic tick time for the tests' eventual conditions.
	tickTime = consts.TickTime
)
