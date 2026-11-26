package v1

import configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"

// GetSyncStatus returns the internal Konnect synchronization state of the entity.
func (obj *KongConsumer) GetSyncStatus() *configurationv1alpha1.SyncStatus {
	return obj.Status.Sync
}

// SetSyncStatus sets the internal Konnect synchronization state of the entity.
func (obj *KongConsumer) SetSyncStatus(s *configurationv1alpha1.SyncStatus) {
	obj.Status.Sync = s
}
