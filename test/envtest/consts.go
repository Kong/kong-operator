package envtest

import "time"

const (
	// konnectInfiniteSyncTime is used for tests that want to verify behavior of the reconcilers not relying on the fixed
	// sync period. It's set to 60m that is virtually infinite for the tests.
	konnectInfiniteSyncTime = time.Minute * 60

	// waitTime is a generic wait time for the tests' eventual conditions.
	waitTime = 10 * time.Second

	// tickTime is a generic tick time for the tests' eventual conditions.
	tickTime = 500 * time.Millisecond
)
