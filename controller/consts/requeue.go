package consts

import "time"

const (
	// RequeueWithoutBackoff is the time after which the controller should requeue the request.
	RequeueWithoutBackoff = time.Millisecond * 200

	// RequeueWithBackoff is the time after which the controller should requeue the request with backoff.
	// This is useful to avoid requeuing the request too frequently in case of
	// e.g. external system errors.
	RequeueWithBackoff = time.Second * 3
)
