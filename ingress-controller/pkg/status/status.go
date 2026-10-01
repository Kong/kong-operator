package status

import (
	internalobject "github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object"
	internalstatus "github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object/status"
)

// Queue re-exports internalstatus.Queue so that reconcilers living outside
// the ingress-controller module tree can subscribe to status update events.
type Queue = internalstatus.Queue

// NewQueue re-exports internalstatus.NewQueue.
var NewQueue = internalstatus.NewQueue

// ConfigurationStatus re-exports internalobject.ConfigurationStatus so that
// reconcilers living outside the ingress-controller module tree can report
// per-object configuration status.
type ConfigurationStatus = internalobject.ConfigurationStatus

const (
	// ConfigurationStatusSucceeded indicates the object's configuration was
	// successfully applied to the data plane.
	ConfigurationStatusSucceeded = internalobject.ConfigurationStatusSucceeded
	// ConfigurationStatusFailed indicates the object's configuration failed
	// to be applied to the data plane.
	ConfigurationStatusFailed = internalobject.ConfigurationStatusFailed
	// ConfigurationStatusUnknown indicates the object's configuration state
	// is unknown (e.g. it was never reported, or its generation is newer than
	// the last report).
	ConfigurationStatusUnknown = internalobject.ConfigurationStatusUnknown
)

// ConfigurationStatusSet re-exports internalobject.ConfigurationStatusSet so
// that reconcilers living outside the ingress-controller module tree can store
// per-object configuration status.
type ConfigurationStatusSet = internalobject.ConfigurationStatusSet

// NewConfigurationStatusSet re-exports internalobject.NewConfigurationStatusSet.
var NewConfigurationStatusSet = internalobject.NewConfigurationStatusSet
