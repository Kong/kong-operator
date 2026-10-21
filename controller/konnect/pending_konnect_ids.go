package konnect

import (
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pendingKonnectIDStore is a concurrency-safe, in-memory store of the Konnect ID
// of entities that have been created in Konnect but whose ID may not yet be
// persisted to their status.
//
// It bridges the window between creating an entity in Konnect and persisting its
// ID to the Kubernetes object status: if the status update fails (or the operator
// is interrupted), the cleanup logic can still recover the Konnect ID from this
// store and delete the entity, avoiding orphaned Konnect entities.
//
// The entity's ID and create-input fingerprint are stored together, so a failed
// status write cannot lose the fingerprint of the request actually sent.
// The parent references needed to delete an entity (control plane, consumer,
// service, upstream, key set, network,
// ...) are persisted to the status by their dedicated reference handlers in
// earlier reconcile passes, so they are already present on the object that the
// deletion logic operates on; the create pass only ever risks losing the entity's
// own ID.
//
// Entries are keyed by the Kubernetes object's namespace/name and are purged once
// the ID has been persisted to the status or the entity has been deleted. Keying
// by namespace/name is safe because the cleanup finalizer is added before the
// entity is created in Konnect, so an object is never removed from the API server
// (and a same-namespace/name successor can never be created) until its own
// deletion reconcile has run and purged the entry.
type pendingKonnectIDStore struct {
	mu  sync.RWMutex
	ids map[client.ObjectKey]pendingKonnectEntityStatus
}

type pendingKonnectEntityStatus struct {
	ID               string
	CreateInputsHash string
}

// newPendingKonnectIDStore returns an initialized pendingKonnectIDStore.
func newPendingKonnectIDStore() *pendingKonnectIDStore {
	return &pendingKonnectIDStore{
		ids: make(map[client.ObjectKey]pendingKonnectEntityStatus),
	}
}

// Store saves the Konnect ID and create-input fingerprint for the given object key.
func (s *pendingKonnectIDStore) Store(key client.ObjectKey, status pendingKonnectEntityStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids[key] = status
}

// Get returns the pending Konnect status for the given object key, if any.
func (s *pendingKonnectIDStore) Get(key client.ObjectKey) (pendingKonnectEntityStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.ids[key]
	return id, ok
}

// Delete removes any Konnect ID stored for the given object key.
func (s *pendingKonnectIDStore) Delete(key client.ObjectKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ids, key)
}
