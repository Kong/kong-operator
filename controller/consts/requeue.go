package consts

import "time"

const (
	// RequeueWithoutBackoff is the time after which the controller should requeue the request.
	RequeueWithoutBackoff = time.Millisecond * 200

	// RequeueWithBackoff is the time after which the controller should requeue the request with backoff.
	// This is useful to avoid requeuing the request too frequently in case of
	// e.g. external system errors.
	RequeueWithBackoff = time.Second * 3

	// KonnectConfigStoreDeletionBlockedRequeuePeriod is the fixed interval at which
	// deletions blocked by Konnect (e.g. a config store that still holds
	// secret entries) are retried. The blockage is resolved out of band on
	// human timescales and produces no watch event; polling slower than
	// RequeueWithBackoff bounds Konnect API calls, failure metrics and error
	// logs while the entity stays blocked.
	KonnectConfigStoreDeletionBlockedRequeuePeriod = time.Minute
)
