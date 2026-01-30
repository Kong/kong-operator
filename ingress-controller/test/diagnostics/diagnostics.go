package diagnostics

import internal "github.com/kong/kong-operator/ingress-controller/internal/diagnostics"

type ConfigDump = internal.ConfigDump
type ConfigDumpResponse = internal.ConfigDumpResponse

type FallbackResponse = internal.FallbackResponse
type FallbackAffectedObjectMeta = internal.FallbackAffectedObjectMeta
type FallbackStatus = internal.FallbackStatus

const (
	FallbackStatusTriggered = internal.FallbackStatusTriggered
)
