package v1alpha1

// -----------------------------------------------------------------------------
// GWAdmin API - Conditions
// -----------------------------------------------------------------------------

const (
	// GWAdminConditionTypeReady indicates whether the certificates, the
	// Services and the Deployment of the GWAdmin are in place. It does not
	// wait for the gw-admin pods to be Ready: they only are once Kong Operator
	// has provisioned them.
	GWAdminConditionTypeReady = "Ready"

	// GWAdminConditionTypeCertificateProvisioned indicates whether the
	// certificate authority and the certificates of the GWAdmin are in place.
	GWAdminConditionTypeCertificateProvisioned = "CertificateProvisioned"
)

const (
	// GWAdminReasonReady is used when the GWAdmin is Ready.
	GWAdminReasonReady = "Ready"
	// GWAdminReasonPending is used until the controller reconciled the GWAdmin.
	GWAdminReasonPending = "Pending"
	// GWAdminReasonWaitingToBecomeReady is used while the Deployment didn't
	// observe its applied spec yet.
	GWAdminReasonWaitingToBecomeReady = "WaitingToBecomeReady"
	// GWAdminReasonDependenciesNotReady is used when another condition is not True.
	GWAdminReasonDependenciesNotReady = "DependenciesNotReady"
	// GWAdminReasonResourceConflict is used when a Deployment or Service the
	// GWAdmin needs already exists and is not controlled by it.
	GWAdminReasonResourceConflict = "ResourceConflict"
	// GWAdminReasonFailedToApply is used when a Deployment, Service or
	// HorizontalPodAutoscaler of the GWAdmin can't be applied.
	GWAdminReasonFailedToApply = "FailedToApply"

	// GWAdminReasonCertificateProvisioned is used when every certificate is in place.
	GWAdminReasonCertificateProvisioned = "CertificateProvisioned"
	// GWAdminReasonCARotationInProgress is used while the certificates move
	// to a new certificate authority.
	GWAdminReasonCARotationInProgress = "CARotationInProgress"
	// GWAdminReasonCARotationBlocked is used when certificates are due for
	// renewal but can't move to their new issuer before the gw-admin pods,
	// or the data planes, roll out, which they don't.
	GWAdminReasonCARotationBlocked = "CARotationBlocked"
	// GWAdminReasonSecretReadFailed is used when a referenced Secret can't
	// be read.
	GWAdminReasonSecretReadFailed = "SecretReadFailed" //nolint:gosec
	// GWAdminReasonSecretRefNotFound is used when a referenced Secret does
	// not exist, or is not visible to the operator.
	GWAdminReasonSecretRefNotFound = "SecretRefNotFound" //nolint:gosec
	// GWAdminReasonInvalidSecret is used when a referenced Secret does not
	// hold a usable certificate.
	GWAdminReasonInvalidSecret = "InvalidSecret"
	// GWAdminReasonSecretRefOperatorManaged is used when a referenced Secret
	// is provisioned by the operator.
	GWAdminReasonSecretRefOperatorManaged = "SecretRefOperatorManaged" //nolint:gosec
	// GWAdminReasonSecretConflict is used when a Secret the GWAdmin needs
	// already exists and is not controlled by it.
	GWAdminReasonSecretConflict = "SecretConflict" //nolint:gosec
)
