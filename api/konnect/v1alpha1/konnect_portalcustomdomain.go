package v1alpha1

// PersistsKonnectID reports whether PortalCustomDomain persists a Konnect ID in status.
func (*PortalCustomDomain) PersistsKonnectID() bool {
	return false
}
