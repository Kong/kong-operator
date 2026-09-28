package konnect

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/configstoresync"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// Event reasons emitted by the KonnectConfigStoreSync controller. Most mirror
// the Synced condition reasons defined in the API package; the two below are
// controller-local because the API package defines no constant for them.
const (
	// konnectConfigStoreSyncPairValidReasonValid is the reason used with the
	// PairValid condition type (reported as True) in Combined mode when the
	// certificate/key pair validates. The API package defines only the
	// PairMismatch and NotApplicable reasons.
	konnectConfigStoreSyncPairValidReasonValid = "Valid"

	// konnectConfigStoreSyncEventReasonSplitWriteIncomplete is the distinct,
	// alertable signal emitted when a Split mode reconcile wrote at least one
	// entry but a subsequent entry write failed. The store then holds a
	// half-applied set of entries, which can wedge config delivery for the
	// whole Control Plane, so this must not look like an ordinary PushFailed.
	konnectConfigStoreSyncEventReasonSplitWriteIncomplete = "SplitWriteIncomplete"
)

// A sync can report one full desired set plus one full set awaiting cleanup.
// Further additions are deferred until pending cleanup frees status capacity.
const konnectConfigStoreSyncMaxStatusEntries = 128

// KonnectConfigStoreSyncReconciler reconciles a KonnectConfigStoreSync object:
// it keeps the entries of one Konnect Config Store in step with one Kubernetes
// Secret, one-way, so that certificate rotation is a pure data event.
//
// Credential chain (one hop further than Konnect entities, which carry their
// own controlPlaneRef): sync -> KonnectConfigStore.spec.controlPlaneRef ->
// KonnectGatewayControlPlane -> GetKonnectAPIAuthConfigurationRef() ->
// KonnectAPIAuthConfiguration. The sync itself carries no auth reference.
type KonnectConfigStoreSyncReconciler struct {
	client.Client

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SDKFactory        sdkops.SDKFactory
	// SyncPeriod is the operator-wide periodic resync interval. It doubles as
	// the drift-detection and conflict-recovery period; it is not part of the
	// propagation path (Secret/store/sync watches are).
	SyncPeriod time.Duration

	eventRecorder events.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectConfigStoreSyncReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder("konnectconfigstoresync")

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.KonnectConfigStoreSync{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.listSyncsForSecret),
		).
		Watches(
			&konnectv1alpha1.KonnectConfigStore{},
			handler.EnqueueRequestsFromMapFunc(r.listSyncsForConfigStore),
		).
		Watches(
			&configurationv1alpha1.KongReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(r.listSyncsForReferenceGrant),
		).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile syncs the referenced Secret into the referenced Config Store.
func (r *KonnectConfigStoreSyncReconciler) Reconcile(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
) (ctrl.Result, error) {
	// KonnectConfigStoreSync has no generated conditions accessors, so
	// conditions are maintained with apimeta.SetStatusCondition on a working
	// copy of the status and persisted with a single merge patch at the end.
	old := sync.DeepCopy()
	res, err := r.reconcile(ctx, sync, old)
	if !equality.Semantic.DeepEqual(old.Status, sync.Status) {
		if perr := r.Client.Status().Patch(ctx, sync, client.MergeFrom(old)); perr != nil {
			if apierrors.IsConflict(perr) {
				return ctrl.Result{Requeue: true}, nil
			}
			return res, errors.Join(err, fmt.Errorf("failed to patch status: %w", perr))
		}
	}
	return res, err
}

func (r *KonnectConfigStoreSyncReconciler) reconcile(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
) (ctrl.Result, error) {
	if !sync.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, sync, old)
	}

	if updated, res, err := patch.WithFinalizer(ctx, r.Client, sync, KonnectCleanupFinalizer); err != nil || updated || !res.IsZero() {
		return res, err
	}
	// A removed entry may remain in the store if cleanup is blocked, but its
	// reference is no longer advertised, even if validation stops this pass.
	sync.Status.References = referencesForDesiredEntries(sync.Status.References, configstoresync.ResolveEntries(sync))

	// 1. Validate the Config Store reference.
	res, store, stop, err := r.validateConfigStoreRef(ctx, sync, old)
	if err != nil || stop {
		return res, err
	}

	// 2. Validate the Secret reference.
	res, secret, stop, err := r.validateSecretRef(ctx, sync, old)
	if err != nil || stop {
		return res, err
	}

	// 3. Combined mode: the x509 pair gate. There is no bypass: a mismatched
	// pair is never written, because a half-valid pair wedges config delivery
	// for the whole Control Plane.
	var notAfter *metav1.Time
	if sync.Spec.Mode != konnectv1alpha1.KonnectConfigStoreSyncModeSplit {
		certField, keyField := combinedSourceFields(sync)
		certPEM, certOK := secret.Data[certField]
		keyPEM, keyOK := secret.Data[keyField]
		var na time.Time
		var pairErr error
		switch {
		case !certOK || !keyOK:
			missing := keyField
			if !certOK {
				missing = certField
			}
			pairErr = fmt.Errorf("secret data field %q is missing", missing)
		default:
			na, pairErr = configstoresync.ValidateX509KeyPair(certPEM, keyPEM)
		}
		if pairErr != nil {
			r.setCondition(sync, metav1.Condition{
				Type:               konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
				Status:             metav1.ConditionFalse,
				Reason:             konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonPairMismatch,
				Message:            fmt.Sprintf("certificate/key pair is invalid: %v", pairErr),
				ObservedGeneration: sync.Generation,
			})
			r.setConditionSynced(sync, metav1.ConditionFalse,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPairMismatch,
				"certificate/key pair is invalid; nothing was written")
			r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
				"PairMismatch",
				"certificate/key pair is invalid; nothing was written")
			return ctrl.Result{}, nil
		}
		notAfter = &metav1.Time{Time: na}
		r.setCondition(sync, metav1.Condition{
			Type:               konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectConfigStoreSyncPairValidReasonValid,
			Message:            "certificate/key pair is valid",
			ObservedGeneration: sync.Generation,
		})
	} else {
		r.setCondition(sync, metav1.Condition{
			Type:               konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonNotApplicable,
			Message:            "pair validation does not apply in Split mode",
			ObservedGeneration: sync.Generation,
		})
	}

	// 4. Read the store status: the sync never resolves a Control Plane
	// itself, it observes storeID/controlPlaneID from the store's status.
	storeID := store.Status.GetKonnectID()
	controlPlaneID := ""
	if store.Status.ControlPlaneID != nil {
		controlPlaneID = store.Status.ControlPlaneID.ID
	}
	if (sync.Status.StoreID != "" && sync.Status.StoreID != storeID) ||
		(sync.Status.ControlPlaneID != "" && sync.Status.ControlPlaneID != controlPlaneID) {
		// Entry observations are valid only for the remote target where they
		// were recorded. A re-created store has a new ID and starts empty.
		sync.Status.Entries = nil
		sync.Status.References = nil
		sync.Status.EntriesSynced = 0
		sync.Status.ObservedSecretResourceVersion = ""
	}
	sync.Status.StoreID = storeID
	sync.Status.ControlPlaneID = controlPlaneID

	// 5. Resolve the entries from the spec (explicit or derived keys).
	entries := configstoresync.ResolveEntries(sync)
	sync.Status.EntriesTotal = int32(len(entries)) //nolint:gosec // bounded by API MaxItems=64

	// 5a. Reject duplicate resolved store keys. CRD validation only checks
	// explicit storeKeys against each other, so a Split entry's explicit
	// storeKey can still collide with another entry's derived key.
	// Duplicates would double-write the same Config Store entry and the
	// status update would be rejected (listMapKey=storeKey). Fail closed:
	// nothing is written. No requeue: only a spec change resolves this.
	if duplicates := configstoresync.DuplicateStoreKeys(entries); len(duplicates) > 0 {
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict,
			fmt.Sprintf("resolved store key %q is claimed by multiple entries of this sync", duplicates[0]))
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"KeyConflict",
			fmt.Sprintf("resolved store key %q is claimed by multiple entries of this sync; nothing was written",
				duplicates[0]))
		return ctrl.Result{}, nil
	}

	// 6. Key-length pre-flight. This is the controller-side check CEL could
	// not express: a derived Split key can exceed the 512-byte cap even though
	// every explicit storeKey is validated.
	for _, e := range entries {
		if len(e.StoreKey) > configstoresync.MaxKeyBytes {
			r.setConditionSynced(sync, metav1.ConditionFalse,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyTooLong,
				fmt.Sprintf("resolved store key %q is %d bytes, exceeding the %d-byte Config Store cap",
					e.StoreKey, len(e.StoreKey), configstoresync.MaxKeyBytes))
			r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
				"KeyTooLong",
				fmt.Sprintf("resolved store key %q exceeds the %d-byte cap; nothing was written",
					e.StoreKey, configstoresync.MaxKeyBytes))
			return ctrl.Result{}, nil
		}
	}

	// 7. Build values and hashes; size pre-flight. Both fail closed: nothing
	// is written and the previous value keeps serving.
	type entryValue struct {
		resolved configstoresync.ResolvedEntry
		value    []byte
		hash     string
	}
	values := make([]entryValue, 0, len(entries))
	for _, e := range entries {
		var value []byte
		if sync.Spec.Mode == konnectv1alpha1.KonnectConfigStoreSyncModeSplit {
			v, ok := secret.Data[e.SourceFields[0]]
			if !ok {
				r.setConditionSynced(sync, metav1.ConditionFalse,
					konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonSecretRefInvalid,
					fmt.Sprintf("secret data field %q is missing", e.SourceFields[0]))
				return ctrl.Result{}, nil
			}
			value = v
		} else {
			certField, keyField := combinedSourceFields(sync)
			v, err := configstoresync.CombinedValue(secret.Data[certField], secret.Data[keyField])
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to build combined value: %w", err)
			}
			value = v
		}
		if len(value) > configstoresync.MaxValueBytes {
			r.setConditionSynced(sync, metav1.ConditionFalse,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge,
				fmt.Sprintf("value for store key %q is %d bytes, exceeding the %d-byte Config Store cap",
					e.StoreKey, len(value), configstoresync.MaxValueBytes))
			r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
				"ValueTooLarge",
				fmt.Sprintf("value for store key %q exceeds the %d-byte cap; nothing was written",
					e.StoreKey, configstoresync.MaxValueBytes))
			return ctrl.Result{}, nil
		}
		values = append(values, entryValue{resolved: e, value: value, hash: configstoresync.ValueHash(value)})
	}

	// 8. Resolve conflicts independently for every key after all safety gates
	// pass. Losing one key must not prevent this sync from maintaining other
	// keys it owns.
	keyConflicts, err := r.checkConflicts(ctx, sync, storeID, entries)
	if err != nil {
		return ctrl.Result{}, err
	}
	sync.Status.References = referencesWithoutKeyConflicts(sync.Status.References, entries, keyConflicts)

	// 9. Resolve the SDK lazily. An all-losing sync, or cleanup that can be
	// resolved locally, must still report its Kubernetes-side state when
	// Konnect credentials are temporarily unavailable.
	var secretsSDK sdkkonnectgo.ConfigStoreSecretsSDK
	resolveSecretsSDK := func() (sdkkonnectgo.ConfigStoreSecretsSDK, error) {
		if secretsSDK != nil {
			return secretsSDK, nil
		}
		resolved, err := r.configStoreSecretsSDK(ctx, store)
		if err != nil {
			return nil, err
		}
		secretsSDK = resolved
		return secretsSDK, nil
	}

	// 10. Prune entries removed from the spec before writing additions. This
	// frees status capacity first and prevents an old owner from deleting a
	// value after another active sync has taken over the key.
	pendingCleanup, pruneBlocked, err := r.pruneRemovedEntries(
		ctx, sync, storeID, controlPlaneID, resolveSecretsSDK, entries,
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	sync.Status.Entries = retainedEntryStatuses(sync.Status.Entries, entries, pendingCleanup)
	if len(entries)+len(pendingCleanup) > konnectConfigStoreSyncMaxStatusEntries {
		var entriesSynced int32
		var references []konnectv1alpha1.KonnectConfigStoreSyncReference
		for _, ev := range values {
			if _, lost := keyConflicts[ev.resolved.StoreKey]; lost {
				continue
			}
			if prev := findEntryStatus(sync.Status.Entries, ev.resolved.StoreKey); prev != nil && prev.Hash == ev.hash {
				entriesSynced++
				references = append(references, configstoresync.ReferenceSuffixes(ev.resolved)...)
			}
		}
		sync.Status.EntriesSynced = entriesSynced
		// Only publish desired entries last synced to the current value. Old
		// entries awaiting cleanup still serve, but no longer rotate.
		sync.Status.References = references
		message := fmt.Sprintf(
			"%d desired entries plus %d entries awaiting cleanup exceed the status capacity of %d; remove references to old entries before adding more",
			len(entries), len(pendingCleanup), konnectConfigStoreSyncMaxStatusEntries,
		)
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse, message)
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"EntryInUse", message)
		return ctrl.Result{RequeueAfter: ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod}, nil
	}

	// 11. Per entry: GET (drift detection via updated_at), then write if and
	// only if the hash changed or the entry drifted. A steady-state reconcile
	// therefore makes zero mutating Konnect calls.
	var (
		newEntries      = make([]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, 0, len(values)+len(pendingCleanup))
		entriesSynced   int32
		syncedKeys      = make(map[string]struct{}, len(values)-len(keyConflicts))
		writesCompleted int
	)
	for _, ev := range values {
		if _, lost := keyConflicts[ev.resolved.StoreKey]; lost {
			if prev := findEntryStatus(sync.Status.Entries, ev.resolved.StoreKey); prev != nil {
				newEntries = append(newEntries, *prev)
			}
			continue
		}

		secretsSDK, err := resolveSecretsSDK()
		if err != nil {
			return ctrl.Result{}, err
		}
		entryStatus := konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			StoreKey:     ev.resolved.StoreKey,
			SourceFields: ev.resolved.SourceFields,
			KeyBytes:     int64(len(ev.resolved.StoreKey)),
			ValueBytes:   int64(len(ev.value)),
			NotAfter:     notAfter,
		}
		if prev := findEntryStatus(sync.Status.Entries, ev.resolved.StoreKey); prev != nil {
			entryStatus.Hash = prev.Hash
			entryStatus.LastPushTime = prev.LastPushTime
			entryStatus.ObservedUpdatedAt = prev.ObservedUpdatedAt
		}

		updatedAt, found, err := ops.GetConfigStoreSecretMetadata(ctx, secretsSDK, controlPlaneID, storeID, ev.resolved.StoreKey)
		if err != nil {
			r.setConditionSynced(sync, metav1.ConditionFalse,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed,
				fmt.Sprintf("failed to read store entry %q: %v", ev.resolved.StoreKey, err))
			// Record the entries this reconcile already processed: an
			// unrecorded write looks like external drift on the next
			// reconcile and raises a false Drifted warning.
			mergeEntryStatuses(&sync.Status.Entries, newEntries)
			return ctrl.Result{}, err
		}

		write, eventReason := configStoreSecretWriteDecision(found, updatedAt, entryStatus.ObservedUpdatedAt, entryStatus.Hash, ev.hash)
		if write {
			created, newUpdatedAt, err := ops.UpsertConfigStoreSecret(ctx, secretsSDK, controlPlaneID, storeID, ev.resolved.StoreKey, string(ev.value))
			if err != nil {
				if writesCompleted > 0 {
					// A previous entry was written in this same reconcile: the
					// store now holds a half-applied set. Surface the distinct
					// signal, not an ordinary PushFailed.
					r.emitEvent(sync, corev1.EventTypeWarning, konnectConfigStoreSyncEventReasonSplitWriteIncomplete,
						fmt.Sprintf("wrote %d of %d entries but failed to write store key %q: the store holds a half-applied set: %v",
							writesCompleted, len(values)-len(keyConflicts), ev.resolved.StoreKey, err))
				}
				r.setConditionSynced(sync, metav1.ConditionFalse,
					konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed,
					fmt.Sprintf("failed to write store entry %q: %v", ev.resolved.StoreKey, err))
				// Record the entries this reconcile already processed: an
				// unrecorded write looks like external drift on the next
				// reconcile and raises a false Drifted warning.
				mergeEntryStatuses(&sync.Status.Entries, newEntries)
				return ctrl.Result{}, err
			}
			writesCompleted++
			now := metav1.Now()
			entryStatus.Hash = ev.hash
			entryStatus.LastPushTime = &now
			entryStatus.ObservedUpdatedAt = observedUpdatedAt(newUpdatedAt)
			switch eventReason {
			case "Drifted":
				r.emitEvent(sync, corev1.EventTypeWarning, "Drifted",
					fmt.Sprintf("store entry %q was modified outside this sync; value re-written", ev.resolved.StoreKey))
			case "EntryRecreated":
				r.emitEvent(sync, corev1.EventTypeNormal, "EntryRecreated",
					fmt.Sprintf("store entry %q was removed outside this sync; re-created", ev.resolved.StoreKey))
			default:
				if !created && entryStatusNeverRecorded(sync.Status.Entries, ev.resolved.StoreKey) {
					r.emitEvent(sync, corev1.EventTypeNormal, "AdoptedExistingEntry",
						fmt.Sprintf("store entry %q already existed and was taken over", ev.resolved.StoreKey))
				}
			}
		} else if found {
			entryStatus.ObservedUpdatedAt = observedUpdatedAt(updatedAt)
		}
		if entryStatus.Hash == ev.hash {
			entriesSynced++
			syncedKeys[ev.resolved.StoreKey] = struct{}{}
		}
		newEntries = append(newEntries, entryStatus)
	}

	// 12. Refresh status. Blocked cleanup records remain alongside the current
	// desired set so every remote key is either represented or relinquished.
	newEntries = append(newEntries, pendingCleanup...)
	sync.Status.Entries = newEntries
	sync.Status.EntriesSynced = entriesSynced
	sync.Status.ObservedSecretResourceVersion = secret.ResourceVersion
	var references []konnectv1alpha1.KonnectConfigStoreSyncReference
	for _, e := range entries {
		if _, synced := syncedKeys[e.StoreKey]; synced {
			references = append(references, configstoresync.ReferenceSuffixes(e)...)
		}
	}
	sync.Status.References = references

	if len(pruneBlocked) > 0 {
		message := fmt.Sprintf("store entries removed from the spec are still referenced by Konnect configuration and no longer updated from the Secret: %s",
			strings.Join(pruneBlocked, ", "))
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse, message)
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"EntryInUse", message)
		return ctrl.Result{RequeueAfter: ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod}, nil
	}

	if len(keyConflicts) > 0 {
		r.reportKeyConflicts(sync, old, keyConflicts)
		// There is no sync-on-sync watch, so periodic requeue is required for
		// each losing key to recover after its winner is deleted.
		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	}

	wasSynced := apimeta.IsStatusConditionTrue(sync.Status.Conditions, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType)
	r.setConditionSynced(sync, metav1.ConditionTrue,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate,
		"all entries are in sync with the referenced Secret")
	if !wasSynced {
		r.emitEvent(sync, corev1.EventTypeNormal, "Synced", "all entries are in sync with the referenced Secret")
	}

	return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
}

// removedEntryKeys returns the store keys recorded in status that the current
// spec no longer resolves to.
func removedEntryKeys(status []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, entries []configstoresync.ResolvedEntry) []string {
	current := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		current[e.StoreKey] = struct{}{}
	}
	var removed []string
	for _, s := range status {
		if _, ok := current[s.StoreKey]; !ok {
			removed = append(removed, s.StoreKey)
		}
	}
	return removed
}

// retainedEntryStatuses keeps status for the desired entries and appends
// removed entries whose deletion is blocked. Successfully deleted and
// relinquished entries are dropped.
func retainedEntryStatuses(
	status []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus,
	entries []configstoresync.ResolvedEntry,
	pendingCleanup []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus,
) []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus {
	current := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		current[entry.StoreKey] = struct{}{}
	}
	retained := make([]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, 0, len(entries)+len(pendingCleanup))
	for _, entryStatus := range status {
		if _, ok := current[entryStatus.StoreKey]; ok {
			retained = append(retained, entryStatus)
		}
	}
	return append(retained, pendingCleanup...)
}

func referencesForDesiredEntries(
	references []konnectv1alpha1.KonnectConfigStoreSyncReference,
	entries []configstoresync.ResolvedEntry,
) []konnectv1alpha1.KonnectConfigStoreSyncReference {
	if len(references) == 0 {
		return references
	}
	desiredSuffixes := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		for _, reference := range configstoresync.ReferenceSuffixes(entry) {
			desiredSuffixes[reference.Suffix] = struct{}{}
		}
	}
	retained := make([]konnectv1alpha1.KonnectConfigStoreSyncReference, 0, len(references))
	for _, reference := range references {
		if _, desired := desiredSuffixes[reference.Suffix]; desired {
			retained = append(retained, reference)
		}
	}
	return retained
}

func referencesWithoutKeyConflicts(
	references []konnectv1alpha1.KonnectConfigStoreSyncReference,
	entries []configstoresync.ResolvedEntry,
	conflicts map[string]types.NamespacedName,
) []konnectv1alpha1.KonnectConfigStoreSyncReference {
	if len(conflicts) == 0 {
		return references
	}
	conflictingSuffixes := make(map[string]struct{}, len(conflicts))
	for _, entry := range entries {
		if _, conflict := conflicts[entry.StoreKey]; !conflict {
			continue
		}
		for _, reference := range configstoresync.ReferenceSuffixes(entry) {
			conflictingSuffixes[reference.Suffix] = struct{}{}
		}
	}
	retained := make([]konnectv1alpha1.KonnectConfigStoreSyncReference, 0, len(references))
	for _, reference := range references {
		if _, conflict := conflictingSuffixes[reference.Suffix]; !conflict {
			retained = append(retained, reference)
		}
	}
	return retained
}

// pruneRemovedEntries deletes spec-removed entries independently. An entry
// claimed by another active sync is relinquished without a DELETE; an entry
// still referenced by Konnect configuration remains in status for retry.
func (r *KonnectConfigStoreSyncReconciler) pruneRemovedEntries(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	storeID, controlPlaneID string,
	resolveSecretsSDK func() (sdkkonnectgo.ConfigStoreSecretsSDK, error),
	entries []configstoresync.ResolvedEntry,
) ([]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, []string, error) {
	removed := removedEntryKeys(sync.Status.Entries, entries)
	if len(removed) == 0 {
		return nil, nil, nil
	}

	candidates := make(map[string]struct{}, len(removed))
	for _, key := range removed {
		claimed, err := r.otherActiveSyncClaimsStoreKey(ctx, sync, storeID, key)
		if err != nil {
			return nil, nil, err
		}
		if claimed {
			continue
		}
		wins, err := r.syncWinsStoreKey(ctx, sync, storeID, key)
		if err != nil {
			return nil, nil, err
		}
		if wins {
			candidates[key] = struct{}{}
		}
	}

	inUse, err := r.entriesInUse(ctx, candidates)
	if err != nil {
		return nil, nil, err
	}
	inUseSet := make(map[string]struct{}, len(inUse))
	for _, key := range inUse {
		inUseSet[key] = struct{}{}
	}

	pendingCleanup := make([]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, 0, len(inUse))
	for _, key := range removed {
		if _, candidate := candidates[key]; !candidate {
			continue
		}
		if _, blocked := inUseSet[key]; blocked {
			if prev := findEntryStatus(sync.Status.Entries, key); prev != nil {
				pendingCleanup = append(pendingCleanup, *prev)
			}
			continue
		}
		secretsSDK, err := resolveSecretsSDK()
		if err != nil {
			return nil, nil, err
		}
		if err := ops.DeleteConfigStoreSecret(ctx, secretsSDK, controlPlaneID, storeID, key); err != nil {
			r.setConditionSynced(sync, metav1.ConditionFalse,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed,
				fmt.Sprintf("failed to delete store entry %q removed from the spec: %v", key, err))
			return nil, nil, err
		}
		r.emitEvent(sync, corev1.EventTypeNormal, "EntryPruned",
			fmt.Sprintf("store entry %q removed from the spec was deleted", key))
	}
	return pendingCleanup, inUse, nil
}

// validateConfigStoreRef resolves spec.configStoreRef, enforces the
// cross-namespace grant, and verifies the referenced store is programmed. It
// reports stop=true when reconciliation must not proceed to any Konnect call.
func (r *KonnectConfigStoreSyncReconciler) validateConfigStoreRef(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
) (_ ctrl.Result, store *konnectv1alpha1.KonnectConfigStore, stop bool, _ error) {
	nn := resolveNamespacedRef(sync.Namespace, sync.Spec.ConfigStoreRef)

	fail := func(reason, message string, syncedReason string) (ctrl.Result, *konnectv1alpha1.KonnectConfigStore, bool, error) {
		r.setCondition(sync, metav1.Condition{
			Type:               konnectv1alpha1.ConfigStoreRefValidConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sync.Generation,
		})
		r.setConditionSynced(sync, metav1.ConditionFalse, syncedReason, message)
		// No requeue: a change to the referenced store triggers reconciliation,
		// and an invalid reference needs a spec change to be fixed.
		return ctrl.Result{}, nil, true, nil
	}

	if nn.Namespace != sync.Namespace {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx, r.Client,
			sync.Namespace, nn.Namespace, nn.Name,
			metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind("KonnectConfigStoreSync")),
			metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind(configurationv1alpha1.KonnectConfigStoreKind)),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				return fail(konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted,
					fmt.Sprintf("cross-namespace reference to KonnectConfigStore %s is not permitted by a KongReferenceGrant", nn),
					konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonWaitingForConfigStore)
			}
			return ctrl.Result{}, nil, true, fmt.Errorf("failed to verify KongReferenceGrant: %w", err)
		}
	}

	var s konnectv1alpha1.KonnectConfigStore
	if err := r.Get(ctx, nn, &s); err != nil {
		if apierrors.IsNotFound(err) {
			// If the sync had observed a programmed store before, the store
			// was removed externally; say so and wait for re-creation. The
			// store data died with the store; a re-created store gets a new
			// Konnect ID and the entries are re-created there automatically.
			hadStore := sync.Status.StoreID != ""
			res, store, stop, err := fail(konnectv1alpha1.ConfigStoreRefReasonInvalid,
				fmt.Sprintf("referenced KonnectConfigStore %s does not exist", nn),
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonWaitingForConfigStore)
			if hadStore {
				r.emitEventOnTransition(old, sync, konnectv1alpha1.ConfigStoreRefValidConditionType,
					"StoreDeleted",
					fmt.Sprintf("referenced KonnectConfigStore %s was removed; waiting for re-creation", nn))
			}
			return res, store, stop, err
		}
		return ctrl.Result{}, nil, true, fmt.Errorf("failed to get referenced KonnectConfigStore %s: %w", nn, err)
	}

	if !s.DeletionTimestamp.IsZero() {
		// The store is being deleted. A store cannot be deleted while it holds
		// entries, so the store CR will wedge until the entries are gone; the
		// sync reports and blocks rather than auto-deleting (report-and-block).
		r.setCondition(sync, metav1.Condition{
			Type:               konnectv1alpha1.ConfigStoreRefValidConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             konnectv1alpha1.ConfigStoreRefReasonInvalid,
			Message:            fmt.Sprintf("referenced KonnectConfigStore %s is being deleted", nn),
			ObservedGeneration: sync.Generation,
		})
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonConfigStoreDeletionBlocked,
			fmt.Sprintf("referenced KonnectConfigStore %s is being deleted while holding this sync's entries", nn))
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"ConfigStoreDeletionBlocked",
			fmt.Sprintf("KonnectConfigStore %s cannot be deleted while this sync's entries exist", nn))
		return ctrl.Result{RequeueAfter: ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod}, nil, true, nil
	}

	if s.Status.GetKonnectID() == "" || s.Status.ControlPlaneID == nil || s.Status.ControlPlaneID.ID == "" {
		return fail(konnectv1alpha1.ConfigStoreRefReasonNotProgrammed,
			fmt.Sprintf("referenced KonnectConfigStore %s is not programmed in Konnect yet", nn),
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonWaitingForConfigStore)
	}

	r.setCondition(sync, metav1.Condition{
		Type:               konnectv1alpha1.ConfigStoreRefValidConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             konnectv1alpha1.ConfigStoreRefReasonValid,
		Message:            fmt.Sprintf("referenced KonnectConfigStore %s is programmed", nn),
		ObservedGeneration: sync.Generation,
	})
	return ctrl.Result{}, &s, false, nil
}

// validateSecretRef resolves spec.secretRef, enforces the cross-namespace
// grant, and fetches the referenced Secret.
func (r *KonnectConfigStoreSyncReconciler) validateSecretRef(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
) (_ ctrl.Result, secret *corev1.Secret, stop bool, _ error) {
	nn := resolveNamespacedRef(sync.Namespace, sync.Spec.SecretRef)

	fail := func(reason, message string) (ctrl.Result, *corev1.Secret, bool, error) {
		r.setCondition(sync, metav1.Condition{
			Type:               konnectv1alpha1.SecretRefValidConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sync.Generation,
		})
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonSecretRefInvalid, message)
		return ctrl.Result{}, nil, true, nil
	}

	if nn.Namespace != sync.Namespace {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx, r.Client,
			sync.Namespace, nn.Namespace, nn.Name,
			metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind("KonnectConfigStoreSync")),
			metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				return fail(konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotAllowed,
					fmt.Sprintf("cross-namespace reference to Secret %s is not permitted by a KongReferenceGrant", nn))
			}
			return ctrl.Result{}, nil, true, fmt.Errorf("failed to verify KongReferenceGrant: %w", err)
		}
	}

	var s corev1.Secret
	if err := r.Get(ctx, nn, &s); err != nil {
		if apierrors.IsNotFound(err) {
			// The Secret is gone; the store data is preserved (fail closed).
			res, sec, stop, err := fail(konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotFound,
				fmt.Sprintf("referenced Secret %s does not exist", nn))
			r.emitEventOnTransition(old, sync, konnectv1alpha1.SecretRefValidConditionType,
				"SecretRefNoLongerExists",
				fmt.Sprintf("referenced Secret %s no longer exists; store data preserved", nn))
			return res, sec, stop, err
		}
		return ctrl.Result{}, nil, true, fmt.Errorf("failed to get referenced Secret %s: %w", nn, err)
	}

	r.setCondition(sync, metav1.Condition{
		Type:               konnectv1alpha1.SecretRefValidConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             konnectv1alpha1.SecretRefReasonValid,
		Message:            fmt.Sprintf("referenced Secret %s exists", nn),
		ObservedGeneration: sync.Generation,
	})
	return ctrl.Result{}, &s, false, nil
}

// checkConflicts resolves each (storeID, storeKey) independently. The sync with
// the oldest creationTimestamp wins, with ties broken by namespace then name.
// It returns only keys this sync loses; winning keys continue reconciliation.
// The guarantee is convergent, not atomic.
func (r *KonnectConfigStoreSyncReconciler) checkConflicts(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	storeID string,
	entries []configstoresync.ResolvedEntry,
) (map[string]types.NamespacedName, error) {
	self := types.NamespacedName{Namespace: sync.Namespace, Name: sync.Name}
	lost := make(map[string]types.NamespacedName)

	for _, e := range entries {
		winner := self
		winnerTS := sync.CreationTimestamp
		seen := map[types.NamespacedName]struct{}{}
		var list konnectv1alpha1.KonnectConfigStoreSyncList
		if err := r.List(ctx, &list,
			client.MatchingFields{index.IndexFieldKonnectConfigStoreSyncOnStoreKey: storeID + "/" + e.StoreKey},
		); err != nil {
			return nil, fmt.Errorf("failed to list syncs conflicting on store key %q: %w", e.StoreKey, err)
		}
		for i := range list.Items {
			other := &list.Items[i]
			nn := types.NamespacedName{Namespace: other.Namespace, Name: other.Name}
			if nn == self {
				continue
			}
			seen[nn] = struct{}{}
			if syncPrecedes(nn, other.CreationTimestamp, winner, winnerTS) {
				winner = nn
				winnerTS = other.CreationTimestamp
			}
		}
		if len(seen) == 0 {
			continue
		}
		if winner != self {
			lost[e.StoreKey] = winner
			continue
		}

		names := make([]string, 0, len(seen))
		for nn := range seen {
			names = append(names, nn.String())
		}
		slices.Sort(names)
		// A winner remains Synced, so there is no condition transition to
		// announce. Emit on every reconcile and rely on Event correlation.
		r.emitEvent(sync, corev1.EventTypeWarning, "KeyConflict",
			fmt.Sprintf("store key %q is also claimed by syncs (%s); this sync wins",
				e.StoreKey, strings.Join(names, ", ")))
	}
	return lost, nil
}

func (r *KonnectConfigStoreSyncReconciler) reportKeyConflicts(
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
	conflicts map[string]types.NamespacedName,
) {
	keys := make([]string, 0, len(conflicts))
	for key := range conflicts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	details := make([]string, 0, len(keys))
	for _, key := range keys {
		details = append(details, fmt.Sprintf("store key %q is owned by sync %s", key, conflicts[key]))
	}
	message := strings.Join(details, "; ")
	r.setConditionSynced(sync, metav1.ConditionFalse,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict, message)
	r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		"KeyConflict", message+"; conflicting entries were skipped")
}

// syncWinsStoreKey reports whether sync wins the oldest-sync election for one
// resolved store key. Cleanup paths use the same ordering as reconciliation so
// a conflict loser cannot delete the winner's entry.
func (r *KonnectConfigStoreSyncReconciler) syncWinsStoreKey(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	storeID, storeKey string,
) (bool, error) {
	self := types.NamespacedName{Namespace: sync.Namespace, Name: sync.Name}
	var contenders konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &contenders,
		client.MatchingFields{index.IndexFieldKonnectConfigStoreSyncOnStoreKey: storeID + "/" + storeKey},
	); err != nil {
		return false, fmt.Errorf("failed to list syncs claiming store key %q: %w", storeKey, err)
	}
	for i := range contenders.Items {
		other := &contenders.Items[i]
		nn := types.NamespacedName{Namespace: other.Namespace, Name: other.Name}
		if nn != self && syncPrecedes(nn, other.CreationTimestamp, self, sync.CreationTimestamp) {
			return false, nil
		}
	}
	return true, nil
}

// otherActiveSyncClaimsStoreKey reports whether another non-deleting sync
// currently declares the key for the same remote Store or referenced Store
// resource. Cleanup relinquishes such keys regardless of creation order.
func (r *KonnectConfigStoreSyncReconciler) otherActiveSyncClaimsStoreKey(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	storeID, storeKey string,
) (bool, error) {
	self := types.NamespacedName{Namespace: sync.Namespace, Name: sync.Name}
	claims := func(contenders *konnectv1alpha1.KonnectConfigStoreSyncList) bool {
		for i := range contenders.Items {
			other := &contenders.Items[i]
			if (types.NamespacedName{Namespace: other.Namespace, Name: other.Name}) == self ||
				!other.DeletionTimestamp.IsZero() {
				continue
			}
			for _, entry := range configstoresync.ResolveEntries(other) {
				if entry.StoreKey == storeKey {
					return true
				}
			}
		}
		return false
	}

	var indexedByStoreKey konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &indexedByStoreKey,
		client.MatchingFields{index.IndexFieldKonnectConfigStoreSyncOnStoreKey: storeID + "/" + storeKey},
	); err != nil {
		return false, fmt.Errorf("failed to list syncs claiming store key %q: %w", storeKey, err)
	}
	if claims(&indexedByStoreKey) {
		return true, nil
	}

	storeNN := resolveNamespacedRef(sync.Namespace, sync.Spec.ConfigStoreRef)
	var indexedByConfigStore konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &indexedByConfigStore,
		client.MatchingFields{index.IndexFieldKonnectConfigStoreSyncOnConfigStore: storeNN.String()},
	); err != nil {
		return false, fmt.Errorf("failed to list syncs for Config Store %s: %w", storeNN, err)
	}
	return claims(&indexedByConfigStore), nil
}

func syncPrecedes(
	nn types.NamespacedName,
	ts metav1.Time,
	otherNN types.NamespacedName,
	otherTS metav1.Time,
) bool {
	return ts.Before(&otherTS) ||
		(ts.Equal(&otherTS) &&
			(nn.Namespace < otherNN.Namespace || (nn.Namespace == otherNN.Namespace && nn.Name < otherNN.Name)))
}

// configStoreSecretsSDK resolves the ConfigStoreSecrets SDK for the store
// along the credential chain: sync -> KonnectConfigStore.spec.controlPlaneRef
// -> KonnectGatewayControlPlane -> GetKonnectAPIAuthConfigurationRef() ->
// KonnectAPIAuthConfiguration token + server URL.
func (r *KonnectConfigStoreSyncReconciler) configStoreSecretsSDK(
	ctx context.Context,
	store *konnectv1alpha1.KonnectConfigStore,
) (sdkkonnectgo.ConfigStoreSecretsSDK, error) {
	ref := store.Spec.ControlPlaneRef
	if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
		// A store whose controlPlaneRef points directly at a Konnect ID has no
		// in-cluster Control Plane object to read credentials from. The store
		// controller itself cannot reconcile such a store either, so its
		// status would never become programmed; this is a defensive branch.
		return nil, fmt.Errorf(
			"KonnectConfigStore %s/%s uses controlPlaneRef type %q: cannot resolve API credentials without an in-cluster KonnectGatewayControlPlane",
			store.Namespace, store.Name, ref.Type)
	}
	cpNN := resolveNamespacedRef(store.Namespace, *ref.NamespacedRef)
	var cp konnectv1alpha2.KonnectGatewayControlPlane
	if err := r.Get(ctx, cpNN, &cp); err != nil {
		return nil, fmt.Errorf("failed to get KonnectGatewayControlPlane %s for KonnectConfigStore %s/%s: %w",
			cpNN, store.Namespace, store.Name, err)
	}

	authRef := cp.GetKonnectAPIAuthConfigurationRef()
	authNN, err := getAPIAuthConfigurationRefNN(ctx, r.Client, &cp, authRef.Name, authRef.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve KonnectAPIAuthConfiguration ref for KonnectGatewayControlPlane %s: %w", cpNN, err)
	}
	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Get(ctx, authNN, &apiAuth); err != nil {
		return nil, fmt.Errorf("failed to get KonnectAPIAuthConfiguration %s: %w", authNN, err)
	}
	token, err := GetTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to get token from KonnectAPIAuthConfiguration %s: %w", authNN, err)
	}

	// NOTE: A new SDK instance is created for each reconciliation because the
	// token is retrieved at runtime through the KonnectAPIAuthConfiguration.
	srv, err := server.NewServer[konnectv1alpha2.KonnectGatewayControlPlane](apiAuth.Spec.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server URL: %w", err)
	}
	sdk := r.SDKFactory.NewKonnectSDK(srv, sdkops.SDKToken(token))
	return sdk.GetConfigStoreSecretsSDK(), nil
}

// reconcileDelete handles a sync with a deletion timestamp according to its
// deletionPolicy: Orphan (default) leaves the store entries in place; Delete
// removes them after a best-effort in-use check.
func (r *KonnectConfigStoreSyncReconciler) reconcileDelete(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sync, KonnectCleanupFinalizer) {
		return ctrl.Result{}, nil
	}

	if sync.Spec.DeletionPolicy == konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete {
		res, done, err := r.deleteStoreEntries(ctx, sync, old)
		if err != nil || !done {
			return res, err
		}
	}

	if _, res, err := patch.WithoutFinalizer(ctx, r.Client, sync, KonnectCleanupFinalizer); err != nil || !res.IsZero() {
		return res, err
	}
	return ctrl.Result{}, nil
}

// deleteStoreEntries deletes every Config Store entry the sync owns. The
// candidate key set is bounded by spec ∪ status: keys are re-derivable from
// the spec alone, so a create-then-crash before the status was persisted does
// not leak entries. It returns done=false when deletion must be retried later
// (the finalizer is held).
func (r *KonnectConfigStoreSyncReconciler) deleteStoreEntries(
	ctx context.Context,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	old *konnectv1alpha1.KonnectConfigStoreSync,
) (_ ctrl.Result, done bool, _ error) {
	nn := resolveNamespacedRef(sync.Namespace, sync.Spec.ConfigStoreRef)
	var store konnectv1alpha1.KonnectConfigStore
	if err := r.Get(ctx, nn, &store); err != nil {
		if apierrors.IsNotFound(err) {
			// The store CR is gone. Its own deletion is blocked while entries
			// exist, so a gone store means the entries are gone too.
			return ctrl.Result{}, true, nil
		}
		return ctrl.Result{}, false, fmt.Errorf("failed to get referenced KonnectConfigStore %s: %w", nn, err)
	}

	storeID := store.Status.GetKonnectID()
	controlPlaneID := ""
	if store.Status.ControlPlaneID != nil {
		controlPlaneID = store.Status.ControlPlaneID.ID
	}
	if storeID == "" || controlPlaneID == "" {
		// The store was never programmed: this sync could not have written
		// entries to it, so no ownership or in-use checks are needed.
		return ctrl.Result{}, true, nil
	}

	if !store.DeletionTimestamp.IsZero() {
		// Report and block, do not auto-delete: auto-deleting would trade a
		// stuck store CR for a possible TLS outage. The escape hatch is
		// patching deletionPolicy to Orphan, which is mutable during deletion.
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonConfigStoreDeletionBlocked,
			fmt.Sprintf("referenced KonnectConfigStore %s is being deleted while holding this sync's entries", nn))
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"ConfigStoreDeletionBlocked",
			fmt.Sprintf("KonnectConfigStore %s cannot be deleted while this sync's entries exist", nn))
		return ctrl.Result{RequeueAfter: ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod}, false, nil
	}

	// Candidate keys: spec-resolved ∪ status-recorded.
	keySet := map[string]struct{}{}
	for _, e := range configstoresync.ResolveEntries(sync) {
		keySet[e.StoreKey] = struct{}{}
	}
	for _, e := range sync.Status.Entries {
		keySet[e.StoreKey] = struct{}{}
	}

	// Relinquish keys claimed by another active sync before applying the
	// deterministic election to any remaining deleting contenders.
	for key := range keySet {
		claimed, err := r.otherActiveSyncClaimsStoreKey(ctx, sync, storeID, key)
		if err != nil {
			return ctrl.Result{}, false, err
		}
		if claimed {
			delete(keySet, key)
			continue
		}
		wins, err := r.syncWinsStoreKey(ctx, sync, store.Status.GetKonnectID(), key)
		if err != nil {
			return ctrl.Result{}, false, err
		}
		if !wins {
			delete(keySet, key)
		}
	}
	if len(keySet) == 0 {
		return ctrl.Result{}, true, nil
	}

	// Best-effort in-use check: a deleted entry that is still referenced is an
	// immediate TLS outage, so deletion is blocked while any KongCertificate
	// references one of the candidate keys via a vault reference string.
	// KongSNI carries no vault reference strings (it points at a
	// KongCertificate via certificateRef), so scanning KongCertificate covers
	// all vault references.
	inUse, err := r.entriesInUse(ctx, keySet)
	if err != nil {
		return ctrl.Result{}, false, err
	}
	if len(inUse) > 0 {
		r.setConditionSynced(sync, metav1.ConditionFalse,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse,
			fmt.Sprintf("store entries still referenced by Konnect configuration: %s", strings.Join(inUse, ", ")))
		r.emitEventOnTransition(old, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			"EntryInUse",
			fmt.Sprintf("deletion blocked: store entries still referenced: %s", strings.Join(inUse, ", ")))
		return ctrl.Result{RequeueAfter: ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod}, false, nil
	}

	secretsSDK, err := r.configStoreSecretsSDK(ctx, &store)
	if err != nil {
		return ctrl.Result{}, false, err
	}
	for key := range keySet {
		if err := ops.DeleteConfigStoreSecret(ctx, secretsSDK, controlPlaneID, storeID, key); err != nil {
			// A failed deletion holds the finalizer: the controller does not
			// give up half-way. Retried with standard backoff.
			return ctrl.Result{}, false, err
		}
	}
	return ctrl.Result{}, true, nil
}

// entriesInUse lists candidate keys still referenced by a KongCertificate
// through a vault reference string ({vault://<prefix>/<key>[/<subfield>]}).
// The match is prefix-agnostic: any KongVault may point at the store.
func (r *KonnectConfigStoreSyncReconciler) entriesInUse(
	ctx context.Context,
	keys map[string]struct{},
) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var certs configurationv1alpha1.KongCertificateList
	if err := r.List(ctx, &certs); err != nil {
		return nil, fmt.Errorf("failed to list KongCertificates for the in-use check: %w", err)
	}
	inUse := map[string]struct{}{}
	for i := range certs.Items {
		for _, ref := range []string{
			certs.Items[i].Spec.Cert,
			certs.Items[i].Spec.Key,
			certs.Items[i].Spec.CertAlt,
			certs.Items[i].Spec.KeyAlt,
		} {
			if key, ok := parseVaultReferenceKey(ref); ok {
				if _, owned := keys[key]; owned {
					inUse[key] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(inUse))
	for k := range inUse {
		out = append(out, k)
	}
	// Map iteration order is nondeterministic; an unstable list would
	// flip-flop the persisted condition message and trigger a status patch
	// on every blocked reconcile.
	slices.Sort(out)
	return out, nil
}

// parseVaultReferenceKey extracts the key segment (the path segment after the
// vault prefix) from a vault reference string of the form
// {vault://<prefix>/<key>[/<subfield>]}.
func parseVaultReferenceKey(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "{vault://") || !strings.HasSuffix(ref, "}") {
		return "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(ref, "{vault://"), "}")
	segments := strings.Split(inner, "/")
	if len(segments) < 2 || segments[1] == "" {
		return "", false
	}
	return segments[1], true
}

// configStoreSecretWriteDecision decides whether an entry must be written.
// found/updatedAt come from the store; observedUpdatedAt/prevHash from the
// sync's status; hash is the desired value's hash.
func configStoreSecretWriteDecision(
	found bool,
	updatedAt time.Time,
	observedUpdatedAt *metav1.Time,
	prevHash, hash string,
) (write bool, eventReason string) {
	if !found {
		if prevHash != "" {
			// The entry was removed out of band; re-create it.
			return true, "EntryRecreated"
		}
		return true, ""
	}
	if observedUpdatedAt != nil && updatedAt.After(observedUpdatedAt.Time) {
		// updated_at advances on every store-side write, even of an identical
		// value, so a newer timestamp means something external touched the
		// entry. Re-write to reassert our value.
		return true, "Drifted"
	}
	if prevHash != hash {
		return true, ""
	}
	return false, ""
}

func observedUpdatedAt(updatedAt time.Time) *metav1.Time {
	if updatedAt.IsZero() {
		return nil
	}
	return &metav1.Time{Time: updatedAt}
}

// findEntryStatus returns the status entry for the given store key, or nil.
func findEntryStatus(entries []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, storeKey string) *konnectv1alpha1.KonnectConfigStoreSyncEntryStatus {
	for i := range entries {
		if entries[i].StoreKey == storeKey {
			return &entries[i]
		}
	}
	return nil
}

// mergeEntryStatuses folds processed entries into the persisted status list,
// updating in place by store key and appending new keys. Used on partial
// failure so entries written before the failure are not mistaken for external
// drift on the next reconcile.
func mergeEntryStatuses(status *[]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, processed []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus) {
	for _, e := range processed {
		if existing := findEntryStatus(*status, e.StoreKey); existing != nil {
			*existing = e
		} else {
			*status = append(*status, e)
		}
	}
}

func entryStatusNeverRecorded(entries []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, storeKey string) bool {
	return findEntryStatus(entries, storeKey) == nil
}

// combinedSourceFields returns the Secret data fields holding the certificate
// and key, applying the API defaults.
func combinedSourceFields(sync *konnectv1alpha1.KonnectConfigStoreSync) (certField, keyField string) {
	certField, keyField = "tls.crt", "tls.key"
	if sync.Spec.Combined != nil {
		if sync.Spec.Combined.CertificateField != "" {
			certField = sync.Spec.Combined.CertificateField
		}
		if sync.Spec.Combined.KeyField != "" {
			keyField = sync.Spec.Combined.KeyField
		}
	}
	return certField, keyField
}

// resolveNamespacedRef defaults the reference namespace to the sync's.
func resolveNamespacedRef(defaultNamespace string, ref commonv1alpha1.NamespacedRef) types.NamespacedName {
	ns := defaultNamespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		ns = *ref.Namespace
	}
	return types.NamespacedName{Namespace: ns, Name: ref.Name}
}

func (r *KonnectConfigStoreSyncReconciler) setCondition(
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	condition metav1.Condition,
) {
	apimeta.SetStatusCondition(&sync.Status.Conditions, condition)
}

func (r *KonnectConfigStoreSyncReconciler) setConditionSynced(
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	status metav1.ConditionStatus,
	reason, message string,
) {
	r.setCondition(sync, metav1.Condition{
		Type:               konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sync.Generation,
	})
}

func (r *KonnectConfigStoreSyncReconciler) emitEvent(
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	eventType, reason, message string,
) {
	if r.eventRecorder != nil {
		r.eventRecorder.Eventf(sync, nil, eventType, reason, reason, message)
	}
}

// emitEventOnTransition emits a Warning event only when the given condition's
// (status, reason) changed in this reconcile, so steady-state reconciles do
// not spam events.
func (r *KonnectConfigStoreSyncReconciler) emitEventOnTransition(
	old, sync *konnectv1alpha1.KonnectConfigStoreSync,
	conditionType string,
	reason, message string,
) {
	cur := apimeta.FindStatusCondition(sync.Status.Conditions, conditionType)
	if cur == nil {
		return
	}
	prev := apimeta.FindStatusCondition(old.Status.Conditions, conditionType)
	if prev == nil || prev.Status != cur.Status || prev.Reason != cur.Reason {
		r.emitEvent(sync, corev1.EventTypeWarning, reason, message)
	}
}
