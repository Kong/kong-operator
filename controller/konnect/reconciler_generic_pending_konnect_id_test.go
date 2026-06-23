package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newKongServiceForPendingTest returns a KongService named ns/svc. When id is
// non-empty the Konnect status (ID + ControlPlaneID) is populated, simulating an
// object whose status already reflects the persisted Konnect ID.
func newKongServiceForPendingTest(id string) *configurationv1alpha1.KongService {
	svc := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "ns",
		},
	}
	if id != "" {
		svc.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: id},
			ControlPlaneID:      "cp-id",
		}
	}
	return svc
}

func newReconcilerForPendingTest(
	objs ...client.Object,
) *KonnectEntityReconciler[configurationv1alpha1.KongService, *configurationv1alpha1.KongService] {
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(objs...).
		WithStatusSubresource(&configurationv1alpha1.KongService{}).
		Build()
	return NewKonnectEntityReconciler[configurationv1alpha1.KongService](
		nil, logging.DevelopmentMode, cl,
	)
}

func TestReconcilePendingKonnectID(t *testing.T) {
	const konnectID = "service-12345"

	t.Run("restores and persists the ID when the cached status lacks it but the store has it", func(t *testing.T) {
		// The API server copy does not carry the ID yet - this models both the
		// cache-lag case and the case where the post-create status write failed.
		r := newReconcilerForPendingTest(newKongServiceForPendingTest(""))

		ent := newKongServiceForPendingTest("")
		key := client.ObjectKeyFromObject(ent)
		r.pendingKonnectIDs.Store(key, konnectID)

		res, stop, err := r.reconcilePendingKonnectID(t.Context(), ent)
		require.NoError(t, err)
		// stop=true: the ID was written to the cluster so this reconcile pass
		// ends here. The watch event from the status write triggers a fresh
		// reconcile with a consistent object, avoiding a spurious Konnect
		// update call (e.g. UpsertPlugin).
		assert.True(t, stop, "reconciliation should stop to let a fresh reconcile proceed with consistent state")
		assert.True(t, res.IsZero())

		// The ID is restored onto ent, so we no longer create (no duplicate).
		assert.Equal(t, konnectID, ent.GetKonnectStatus().GetKonnectID())
		assert.False(t, shouldCreateKonnectEntity(ent), "must not create a duplicate Konnect entity")

		// The ID is written through to the API server.
		var fetched configurationv1alpha1.KongService
		require.NoError(t, r.Client.Get(t.Context(), key, &fetched))
		assert.Equal(t, konnectID, fetched.GetKonnectStatus().GetKonnectID())

		// The bridge entry is NOT yet purged on this pass. It is cleaned up in
		// the subsequent reconcile (triggered by the status watch event) once
		// the cached status reflects the ID.
		_, ok := r.pendingKonnectIDs.Get(key)
		assert.True(t, ok, "entry must remain in store until the next reconcile")

		// Simulate the next reconcile: the cache now reflects the persisted ID.
		ent2 := newKongServiceForPendingTest(konnectID)
		_, stop2, err2 := r.reconcilePendingKonnectID(t.Context(), ent2)
		require.NoError(t, err2)
		assert.False(t, stop2, "second pass should continue normally")
		_, ok2 := r.pendingKonnectIDs.Get(key)
		assert.False(t, ok2, "entry must be purged once the cached status reflects the ID")
	})

	t.Run("does nothing and allows create when there is no store entry", func(t *testing.T) {
		r := newReconcilerForPendingTest(newKongServiceForPendingTest(""))
		ent := newKongServiceForPendingTest("")

		res, stop, err := r.reconcilePendingKonnectID(t.Context(), ent)
		require.NoError(t, err)
		assert.False(t, stop)
		assert.True(t, res.IsZero())
		assert.Empty(t, ent.GetKonnectStatus().GetKonnectID())
		assert.True(t, shouldCreateKonnectEntity(ent), "a first-time create must proceed")
	})

	t.Run("purges the store entry once the cached status already carries the ID", func(t *testing.T) {
		r := newReconcilerForPendingTest(newKongServiceForPendingTest(konnectID))
		ent := newKongServiceForPendingTest(konnectID)
		key := client.ObjectKeyFromObject(ent)
		r.pendingKonnectIDs.Store(key, konnectID)

		_, stop, err := r.reconcilePendingKonnectID(t.Context(), ent)
		require.NoError(t, err)
		assert.False(t, stop)

		_, ok := r.pendingKonnectIDs.Get(key)
		assert.False(t, ok, "entry must be purged once the status reflects the ID")
	})
}

func TestRestorePendingKonnectIDForDeletion(t *testing.T) {
	const konnectID = "service-12345"

	t.Run("restores the ID from the store when the status has none", func(t *testing.T) {
		r := newReconcilerForPendingTest()
		ent := newKongServiceForPendingTest("")
		key := client.ObjectKeyFromObject(ent)
		r.pendingKonnectIDs.Store(key, konnectID)

		r.restorePendingKonnectIDForDeletion(ent)
		assert.Equal(t, konnectID, ent.GetKonnectStatus().GetKonnectID())
	})

	t.Run("keeps the existing status ID and ignores the store", func(t *testing.T) {
		r := newReconcilerForPendingTest()
		ent := newKongServiceForPendingTest(konnectID)
		key := client.ObjectKeyFromObject(ent)
		r.pendingKonnectIDs.Store(key, "a-different-id")

		r.restorePendingKonnectIDForDeletion(ent)
		assert.Equal(t, konnectID, ent.GetKonnectStatus().GetKonnectID(), "the persisted status ID must win")
	})

	t.Run("no-op when neither the status nor the store has an ID", func(t *testing.T) {
		r := newReconcilerForPendingTest()
		ent := newKongServiceForPendingTest("")

		r.restorePendingKonnectIDForDeletion(ent)
		assert.Empty(t, ent.GetKonnectStatus().GetKonnectID())
	})
}
