package dataplane

import internal "github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane"

const (
	KongConfigurationApplySucceededEventReason            = internal.KongConfigurationApplySucceededEventReason
	KongConfigurationTranslationFailedEventReason         = internal.KongConfigurationTranslationFailedEventReason
	KongConfigurationApplyFailedEventReason               = internal.KongConfigurationApplyFailedEventReason
	FallbackKongConfigurationApplySucceededEventReason    = internal.FallbackKongConfigurationApplySucceededEventReason
	FallbackKongConfigurationTranslationFailedEventReason = internal.FallbackKongConfigurationTranslationFailedEventReason
	FallbackKongConfigurationApplyFailedEventReason       = internal.FallbackKongConfigurationApplyFailedEventReason
)
