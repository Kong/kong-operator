package status

import (
	internalstatus "github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object/status"
)

// Queue re-exports internalstatus.Queue so that reconcilers living outside
// the ingress-controller module tree can subscribe to status update events.
type Queue = internalstatus.Queue
