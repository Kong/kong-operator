package v1alpha1

const (
	// KonnectEntityProgrammedConditionType is the condition that
	// indicates whether the entity has been programmed in Konnect.
	KonnectEntityProgrammedConditionType = "Programmed"

	// KonnectEntityProgrammedReasonProgrammed is the reason for the Programmed condition.
	// It is set when the entity has been programmed in Konnect.
	KonnectEntityProgrammedReasonProgrammed = "Programmed"
	// KonnectEntityProgrammedReasonKonnectAPIOpFailed is the reason for the Programmed condition.
	// It is set when the entity has failed to be programmed in Konnect.
	KonnectEntityProgrammedReasonKonnectAPIOpFailed = "KonnectAPIOpFailed"
	// KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs is the reason for the Programmed condition.
	// It is set when one or more KongConsumerGroup references could not be resolved.
	KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs = "FailedToResolveConsumerGroupRefs"
	// KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect is the reason for the Programmed condition.
	// It is set when one or more KongConsumerGroup references could not be reconciled with Konnect.
	KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect = "FailedToReconcileConsumerGroupsWithKonnect"
	// KonnectEntityProgrammedReasonConditionWithStatusFalseExists is the reason for the Programmed condition.
	// It is set when there's at least one status condition (not Programmed) with status false.
	KonnectEntityProgrammedReasonConditionWithStatusFalseExists = "ConditionWithStatusFalseExists"

	// KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers is the reason for the Programmed
	// condition. It is set when the control plane
	// group members could not be set.
	KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers = "FailedToSetControlPlaneGroupMembers"
)

const (
	// KonnectEntityAPIAuthConfigurationResolvedRefConditionType is the type of the
	// condition that indicates whether the APIAuth configuration reference is
	// valid and points to an existing APIAuth configuration.
	KonnectEntityAPIAuthConfigurationResolvedRefConditionType = "APIAuthResolvedRef"

	// KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef is the reason
	// used with the APIAuthResolvedRef condition type indicating that the APIAuth
	// configuration reference has been resolved.
	KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef = "ResolvedRef"
	// KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound is the reason
	// used with the APIAuthResolvedRef condition type indicating that the APIAuth
	// configuration reference could not be resolved.
	KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound = "RefNotFound"
	// KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid is the reason
	// used with the APIAuthResolvedRef condition type indicating that the APIAuth
	// configuration reference is invalid and could not be resolved.
	// Condition message can contain more information about the error.
	KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid = "RefInvalid"
)

const (
	// KonnectEntityAPIAuthConfigurationValidConditionType is the type of the
	// condition that indicates whether the referenced APIAuth configuration is
	// valid.
	KonnectEntityAPIAuthConfigurationValidConditionType = "APIAuthValid"

	// KonnectEntityAPIAuthConfigurationReasonValid is the reason used with the
	// APIAuthRefValid condition type indicating that the APIAuth configuration
	// referenced by the entity is valid.
	KonnectEntityAPIAuthConfigurationReasonValid = "Valid"
	// KonnectEntityAPIAuthConfigurationReasonInvalid is the reason used with the
	// APIAuthRefValid condition type indicating that the APIAuth configuration
	// referenced by the entity is invalid.
	KonnectEntityAPIAuthConfigurationReasonInvalid = "Invalid"
)

const (
	// ControlPlaneRefValidConditionType is the type of the condition that indicates
	// whether the ControlPlane reference is valid and points to an existing
	// ControlPlane.
	ControlPlaneRefValidConditionType = "ControlPlaneRefValid"

	// ControlPlaneRefReasonValid is the reason used with the ControlPlaneRefValid
	// condition type indicating that the ControlPlane reference is valid.
	ControlPlaneRefReasonValid = "Valid"
	// ControlPlaneRefReasonInvalid is the reason used with the ControlPlaneRefValid
	// condition type indicating that the ControlPlane reference is invalid.
	ControlPlaneRefReasonInvalid = "Invalid"
)

const (
	// KongServiceRefValidConditionType is the type of the condition that indicates
	// whether the KongService reference is valid and points to an existing
	// KongService.
	KongServiceRefValidConditionType = "KongServiceRefValid"

	// KongServiceRefReasonValid is the reason used with the KongServiceRefValid
	// condition type indicating that the KongService reference is valid.
	KongServiceRefReasonValid = "Valid"
	// KongServiceRefReasonInvalid is the reason used with the KongServiceRefValid
	// condition type indicating that the KongService reference is invalid.
	KongServiceRefReasonInvalid = "Invalid"
)

const (
	// KongConsumerRefValidConditionType is the type of the condition that indicates
	// whether the KongConsumer reference is valid and points to an existing
	// KongConsumer.
	KongConsumerRefValidConditionType = "KongConsumerRefValid"

	// KongConsumerRefReasonValid is the reason used with the KongConsumerRefValid
	// condition type indicating that the KongConsumer reference is valid.
	KongConsumerRefReasonValid = "Valid"
	// KongConsumerRefReasonInvalid is the reason used with the KongConsumerRefValid
	// condition type indicating that the KongConsumer reference is invalid.
	KongConsumerRefReasonInvalid = "Invalid"
)

const (
	// KongConsumerGroupRefsValidConditionType is the type of the condition that indicates
	// whether the KongConsumerGroups referenced by the entity are valid and all point to
	// existing KongConsumerGroups.
	KongConsumerGroupRefsValidConditionType = "KongConsumerGroupRefsValid"

	// KongConsumerGroupRefsReasonValid is the reason used with the KongConsumerGroupRefsValid
	// condition type indicating that all KongConsumerGroup references are valid.
	KongConsumerGroupRefsReasonValid = "Valid"
	// KongConsumerGroupRefsReasonInvalid is the reason used with the KongConsumerGroupRefsValid
	// condition type indicating that one or more KongConsumerGroup references are invalid.
	KongConsumerGroupRefsReasonInvalid = "Invalid"
)

const (
	// KongUpstreamRefValidConditionType is the type of the condition that indicates
	// whether the KongUpstream reference is valid and points to an existing KongUpstream.
	KongUpstreamRefValidConditionType = "KongUpstreamRefValid"

	// KongUpstreamRefReasonValid is the reason used with the KongUpstreamRefValid
	// condition type indicating that the KongUpstream reference is valid.
	KongUpstreamRefReasonValid = "Valid"
	// KongUpstreamRefReasonInvalid is the reason used with the KongUpstreamRefValid
	// condition type indicating that the KongUpstream reference is invalid.
	KongUpstreamRefReasonInvalid = "Invalid"
)

const (
	// KeySetRefValidConditionType is the type of the condition that indicates
	// whether the KeySet reference is valid and points to an existing
	// KeySet.
	KeySetRefValidConditionType = "KeySetRefValid"

	// KeySetRefReasonValid is the reason used with the KeySetRefValid
	// condition type indicating that the KeySet reference is valid.
	KeySetRefReasonValid = "Valid"
	// KeySetRefReasonInvalid is the reason used with the KeySetRefValid
	// condition type indicating that the KeySet reference is invalid.
	KeySetRefReasonInvalid = "Invalid"
)

const (
	// KongCertificateRefValidConditionType is the type of the condition that indicates
	// whether the KongCertificate reference is valid and points to an existing KongCertificate
	KongCertificateRefValidConditionType = "KongCertificateRefValid"

	// KongCertificateRefReasonValid is the reason used with the KongCertificateRefValid
	// condition type indicating that the KongCertificate reference is valid.
	KongCertificateRefReasonValid = "Valid"
	// KongCertificateRefReasonInvalid is the reason used with the KongCertificateRefValid
	// condition type indicating that the KongCertificate reference is invalid.
	KongCertificateRefReasonInvalid = "Invalid"
)

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
	KonnectExtensionReadyReasonProvisioning = "provisioning"
)

const (
	// DataPlaneCertificateProvisionedConditionType is the type of the condition that indicates
	// whether the DataPlane certificate has been properly provisioned in Konnect.
	DataPlaneCertificateProvisionedConditionType = "DataPlaneCertificateProvisioned"

	// DataPlaneCertificateProvisionedReasonProvisioned is the reason
	// used with the DataPlaneCertificateProvisioned condition type indicating that the
	// referenced DataPlane client certificate has been provisioned in Konnect.
	DataPlaneCertificateProvisionedReasonProvisioned = "Provisioned"
	// DataPlaneCertificateProvisionedReasonRefNotFound is the reason
	// used with the DataPlaneCertificateProvisioned condition type indicating that the
	// the referenced DataPlane client certificate is not found.
	DataPlaneCertificateProvisionedReasonRefNotFound = "RefNotFound"
	// DataPlaneCertificateProvisionedReasonInvalidSecret is the reason
	// used with the DataPlaneCertificateProvisioned condition type indicating that the
	// the referenced DataPlane client certificate secret is invalid.
	DataPlaneCertificateProvisionedReasonInvalidSecret = "InvalidSecret"
	// DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed is the reason
	// used with the DataPlaneCertificateProvisioned condition type indicating that the
	// the DataPlane client certificate creation in Konnect has failed.
	DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed = "KonnectAPIOpFailed"
)

const (
	// KonnectNetworkRefsValidConditionType is the type of the condition that indicates
	// whether the Konnect network reference is valid and points to an existing Konnect network.
	KonnectNetworkRefsValidConditionType = "KonnectNetworkRefsValid"

	// KonnectNetworkRefsReasonValid is the reason used with the KonnectNetworkRefsValid
	// condition type indicating that the Konnect network reference is valid.
	KonnectNetworkRefsReasonValid = "Valid"
	// KonnectNetworkRefsReasonInvalid is the reason used with the KonnectNetworkRefsValid
	// condition type indicating that the Konnect network reference is invalid.
	KonnectNetworkRefsReasonInvalid = "Invalid"
)
