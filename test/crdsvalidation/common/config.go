package common

import (
	"time"
)

// SharedEventuallyConfig is the common Eventually configuration used across tests.
var SharedEventuallyConfig = EventuallyConfig{
	Timeout: 15 * time.Second,
	Period:  100 * time.Millisecond,
}
