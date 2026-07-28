package consts

import "time"

const (
	// KonnectInfiniteSyncTime is used for tests that want to verify behavior of the reconcilers not relying on the fixed
	// sync period. It's set to 60m that is virtually infinite for the tests.
	KonnectInfiniteSyncTime = time.Minute * 60

	// WaitTime is a generic wait time for the tests' eventual conditions.
	WaitTime = 20 * time.Second

	// TickTime is a generic tick time for the tests' eventual conditions.
	TickTime = 250 * time.Millisecond
)
