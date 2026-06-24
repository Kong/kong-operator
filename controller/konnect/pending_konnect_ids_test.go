package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPendingKonnectIDStore(t *testing.T) {
	s := newPendingKonnectIDStore()

	key := client.ObjectKey{Namespace: "ns", Name: "foo"}

	t.Run("Get on empty store returns not found", func(t *testing.T) {
		_, ok := s.Get(key)
		assert.False(t, ok)
	})

	t.Run("Store then Get returns the stored ID", func(t *testing.T) {
		s.Store(key, "konnect-id")
		got, ok := s.Get(key)
		assert.True(t, ok)
		assert.Equal(t, "konnect-id", got)
	})

	t.Run("Delete removes the entry", func(t *testing.T) {
		s.Delete(key)
		_, ok := s.Get(key)
		assert.False(t, ok)
	})

	t.Run("entries are isolated per key", func(t *testing.T) {
		k1 := client.ObjectKey{Namespace: "ns", Name: "a"}
		k2 := client.ObjectKey{Namespace: "ns", Name: "b"}
		s.Store(k1, "id-a")
		s.Store(k2, "id-b")

		got1, ok1 := s.Get(k1)
		got2, ok2 := s.Get(k2)
		assert.True(t, ok1)
		assert.True(t, ok2)
		assert.Equal(t, "id-a", got1)
		assert.Equal(t, "id-b", got2)

		s.Delete(k1)
		_, ok1 = s.Get(k1)
		_, ok2 = s.Get(k2)
		assert.False(t, ok1)
		assert.True(t, ok2)
	})
}
