package v1alpha2

const (
	// KonnectExtensionReadyConditionType is the type of the condition that indicates
	// whether the Konnect extension is ready to be used.
	KonnectExtensionReadyConditionType = "Ready"

	// KonnectExtensionReadyReasonReady is the reason used with the
	// KonnectExtensionReady condition type indicating that the Konnect extension
	// is Ready.
	KonnectExtensionReadyReasonReady = "Ready"
	// KonnectExtensionReadyReasonPending is the reason used with the
	// KonnectExtensionReady condition type indicating that the Konnect extension
	// is pending.
	KonnectExtensionReadyReasonPending = "Pending"
	// KonnectExtensionReadyReasonProvisioning is the reason used with the
	// KonnectExtensionReady condition type indicating that the Konnect extension
	// is provisioning.
	KonnectExtensionReadyReasonProvisioning = "Provisioning"
)
