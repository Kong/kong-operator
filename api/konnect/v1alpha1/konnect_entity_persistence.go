package v1alpha1

// PersistsKonnectID reports whether EventGatewayBackendCluster persists a Konnect ID in status.
func (*EventGatewayBackendCluster) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether EventGatewayListener persists a Konnect ID in status.
func (*EventGatewayListener) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether EventGatewayListenerPolicy persists a Konnect ID in status.
func (*EventGatewayListenerPolicy) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether EventGatewayVirtualCluster persists a Konnect ID in status.
func (*EventGatewayVirtualCluster) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether IdentityProviderRequest persists a Konnect ID in status.
func (*IdentityProviderRequest) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether KonnectEventDataPlaneCertificate persists a Konnect ID in status.
func (*KonnectEventDataPlaneCertificate) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether KonnectEventGateway persists a Konnect ID in status.
func (*KonnectEventGateway) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether Portal persists a Konnect ID in status.
func (*Portal) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether PortalCustomDomain persists a Konnect ID in status.
func (*PortalCustomDomain) PersistsKonnectID() bool {
	return false
}

// PersistsKonnectID reports whether PortalEmailConfig persists a Konnect ID in status.
func (*PortalEmailConfig) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether PortalIPAllowList persists a Konnect ID in status.
func (*PortalIPAllowList) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether PortalPage persists a Konnect ID in status.
func (*PortalPage) PersistsKonnectID() bool {
	return true
}

// PersistsKonnectID reports whether PortalTeam persists a Konnect ID in status.
func (*PortalTeam) PersistsKonnectID() bool {
	return true
}
