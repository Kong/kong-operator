package konnect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/configstoresync"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// Creation timestamps set by the API server have second precision, so an
// envtest cannot reliably create two syncs with distinct timestamps. The
// election order is covered here with explicit timestamps instead.
func TestKonnectConfigStoreSyncConflictElection(t *testing.T) {
	const (
		storeID  = "store-id"
		storeKey = "shared-key"
	)
	older := metav1.NewTime(time.Date(2026, time.October, 1, 8, 27, 37, 0, time.UTC))
	newer := metav1.NewTime(older.Add(time.Second))

	claimingSync := func(namespace, name string, created metav1.Time) *konnectv1alpha1.KonnectConfigStoreSync {
		return &konnectv1alpha1.KonnectConfigStoreSync{
			Namespace:         namespace,
			Name:              name,
			CreationTimestamp: created,
			Spec: konnectv1alpha1.KonnectConfigStoreSyncSpec{
				Mode: konnectv1alpha1.KonnectConfigStoreSyncModeCombined,
				Combined: &konnectv1alpha1.KonnectConfigStoreSyncCombined{
					StoreKey: new(storeKey),
				},
			},
			// The store key index only covers keys a sync has durably written.
			Status: konnectv1alpha1.KonnectConfigStoreSyncStatus{
				StoreID: storeID,
				Entries: []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
					{StoreKey: storeKey},
				},
			},
		}
	}

	testCases := []struct {
		name       string
		self       *konnectv1alpha1.KonnectConfigStoreSync
		other      *konnectv1alpha1.KonnectConfigStoreSync
		wantWinner types.NamespacedName
	}{
		{
			name:       "older sync wins even though its name sorts later",
			self:       claimingSync("ns", "a-second", newer),
			other:      claimingSync("ns", "z-first", older),
			wantWinner: types.NamespacedName{Namespace: "ns", Name: "z-first"},
		},
		{
			name:       "older sync keeps the key against a newer sync whose name sorts first",
			self:       claimingSync("ns", "z-first", older),
			other:      claimingSync("ns", "a-second", newer),
			wantWinner: types.NamespacedName{Namespace: "ns", Name: "z-first"},
		},
		{
			name:       "equal timestamps are broken by name",
			self:       claimingSync("ns", "z-sync", older),
			other:      claimingSync("ns", "a-sync", older),
			wantWinner: types.NamespacedName{Namespace: "ns", Name: "a-sync"},
		},
		{
			name:       "equal timestamps are broken by namespace before name",
			self:       claimingSync("ns-b", "a-sync", older),
			other:      claimingSync("ns-a", "z-sync", older),
			wantWinner: types.NamespacedName{Namespace: "ns-a", Name: "z-sync"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.self, tc.other)
			for _, opt := range index.OptionsForKonnectConfigStoreSync() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}
			r := &KonnectConfigStoreSyncReconciler{Client: builder.Build()}

			self := &konnectv1alpha1.KonnectConfigStoreSync{}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(tc.self), self))
			require.True(t, tc.self.CreationTimestamp.Equal(&self.CreationTimestamp),
				"fake client must keep the explicit creation timestamp")
			selfWins := tc.wantWinner == client.ObjectKeyFromObject(self)

			lost, err := r.checkConflicts(t.Context(), self, storeID, configstoresync.ResolveEntries(self))
			require.NoError(t, err)
			if selfWins {
				assert.Empty(t, lost)
			} else {
				assert.Equal(t, map[string]types.NamespacedName{storeKey: tc.wantWinner}, lost)
			}

			// Cleanup must follow the same election as reconciliation.
			wins, err := r.syncWinsStoreKey(t.Context(), self, storeID, storeKey)
			require.NoError(t, err)
			assert.Equal(t, selfWins, wins)
		})
	}
}
