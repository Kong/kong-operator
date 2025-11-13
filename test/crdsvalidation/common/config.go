package common

import (
	"time"
)

var SharedEventuallyConfig = EventuallyConfig{
	Timeout: 15 * time.Second,
	Period:  100 * time.Millisecond,
}
