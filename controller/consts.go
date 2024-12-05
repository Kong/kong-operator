package controller

import "time"

const (
	// RequeueAfter is the time after which the controller should requeue the request.
	RequeueWithoutBackoff = time.Millisecond * 200
)
