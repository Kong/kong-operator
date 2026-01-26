package metrics

import internal "github.com/kong/kong-operator/ingress-controller/internal/metrics"

const (
	InstanceIDKey = internal.InstanceIDKey

	MetricNameConfigPushCount            = internal.MetricNameConfigPushCount
	MetricNameConfigPushBrokenResources  = internal.MetricNameConfigPushBrokenResources
	MetricNameConfigPushSize             = internal.MetricNameConfigPushSize
	MetricNameConfigPushDuration         = internal.MetricNameConfigPushDuration
	MetricNameTranslationCount           = internal.MetricNameTranslationCount
	MetricNameTranslationBrokenResources = internal.MetricNameTranslationBrokenResources
	MetricNameTranslationDuration        = internal.MetricNameTranslationDuration

	MetricNameFallbackTranslationCount           = internal.MetricNameFallbackTranslationCount
	MetricNameFallbackTranslationBrokenResources = internal.MetricNameFallbackTranslationBrokenResources
	MetricNameFallbackTranslationDuration        = internal.MetricNameFallbackTranslationDuration
	MetricNameFallbackConfigPushSize             = internal.MetricNameFallbackConfigPushSize
	MetricNameFallbackConfigPushCount            = internal.MetricNameFallbackConfigPushCount
	MetricNameFallbackConfigPushSuccessTime      = internal.MetricNameFallbackConfigPushSuccessTime
	MetricNameFallbackConfigPushDuration         = internal.MetricNameFallbackConfigPushDuration
	MetricNameFallbackConfigPushBrokenResources  = internal.MetricNameFallbackConfigPushBrokenResources

	MetricNameProcessedConfigSnapshotCacheHit  = internal.MetricNameProcessedConfigSnapshotCacheHit
	MetricNameProcessedConfigSnapshotCacheMiss = internal.MetricNameProcessedConfigSnapshotCacheMiss
)
