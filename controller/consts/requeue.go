package consts

import "time"

const (
	// RequeueWithoutBackoff is the time after which the controller should requeue the request.
	RequeueWithoutBackoff = time.Millisecond * 200
)
