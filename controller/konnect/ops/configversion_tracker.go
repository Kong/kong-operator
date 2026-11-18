package ops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

// ConfigVersionTracker avoids enforcing unchanged Kong Gateway entities in Konnect on every sync period.
//
// For every entity it remembers the hash of the last request sent to Konnect and the hash of the entity
// representation Konnect returned. For every control plane it tracks the expected config version, which
// changes whenever the control plane configuration changes.
//
// A periodic update of an entity is skipped when the request is unchanged and no external change to its
// control plane was observed since the entity was last checked. When an external change was observed, the
// entity is fetched from Konnect and only updated when its representation differs.
//
// Writes performed by the operator itself (reported through MarkWrite) also change the config version.
// A version change observed in a window in which the operator wrote to the control plane is attributed to the
// operator and does not trigger fetching entities: an external change interleaved with operator writes is only
// detected after a later external change to the control plane, a change of the entity in Kubernetes or a restart.
type ConfigVersionTracker struct {
	versionTTL time.Duration
	now        func() time.Time

	mu            sync.Mutex
	controlPlanes map[string]*controlPlaneConfigVersion
	entities      map[types.UID]entitySyncRecord
}

// NewConfigVersionTracker returns a ConfigVersionTracker that checks the config version of a control plane
// at most once every versionTTL.
func NewConfigVersionTracker(versionTTL time.Duration) *ConfigVersionTracker {
	return &ConfigVersionTracker{
		versionTTL:    versionTTL,
		now:           time.Now,
		controlPlanes: map[string]*controlPlaneConfigVersion{},
		entities:      map[types.UID]entitySyncRecord{},
	}
}

type controlPlaneConfigVersion struct {
	fetchMu sync.Mutex

	// Fields below are guarded by ConfigVersionTracker.mu.
	fetchedAt time.Time
	fetchErr  bool
	version   string
	changedAt time.Time
	ownWrite  bool
}

type hash [sha256.Size]byte

type entitySyncRecord struct {
	payloadHash hash
	reprHash    hash
	checkedAt   time.Time
}

// MarkWrite records that the operator is writing to the given control plane.
// It must be called both before sending a write request and after receiving its response, so that every
// config version check overlapping the write attributes the version change to the operator.
func (t *ConfigVersionTracker) MarkWrite(controlPlaneID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.controlPlane(controlPlaneID).ownWrite = true
}

// Forget drops what is known about the entity with the given UID.
func (t *ConfigVersionTracker) Forget(uid types.UID) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entities, uid)
}

func (t *ConfigVersionTracker) controlPlane(controlPlaneID string) *controlPlaneConfigVersion {
	cp, ok := t.controlPlanes[controlPlaneID]
	if !ok {
		cp = &controlPlaneConfigVersion{}
		t.controlPlanes[controlPlaneID] = cp
	}
	return cp
}

// lastExternalChange returns the time of the last observed external change of the control plane's
// configuration, refreshing the config version when it is older than versionTTL.
// The returned bool is false when the config version could not be retrieved.
func (t *ConfigVersionTracker) lastExternalChange(
	ctx context.Context,
	sdk sdkkonnectgo.DPNodesSDK,
	controlPlaneID string,
) (time.Time, bool) {
	t.mu.Lock()
	cp := t.controlPlane(controlPlaneID)
	t.mu.Unlock()

	cp.fetchMu.Lock()
	defer cp.fetchMu.Unlock()

	t.mu.Lock()
	if !cp.fetchedAt.IsZero() && t.now().Sub(cp.fetchedAt) < t.versionTTL {
		changedAt, ok := cp.changedAt, !cp.fetchErr
		t.mu.Unlock()
		return changedAt, ok
	}
	t.mu.Unlock()

	version, err := fetchExpectedConfigVersion(ctx, sdk, controlPlaneID)

	t.mu.Lock()
	defer t.mu.Unlock()

	// The change could have happened any time before the response, so it is
	// timestamped after it: entities checked before are considered stale.
	now := t.now()
	ownWrite := cp.ownWrite
	cp.ownWrite = false
	cp.fetchedAt = now
	if err != nil {
		log.Debug(ctrllog.FromContext(ctx), "failed to get Konnect control plane expected config version",
			"control_plane_id", controlPlaneID, "error", err.Error(),
		)
		cp.fetchErr = true
		return time.Time{}, false
	}

	if cp.version != "" && cp.version != version && !ownWrite {
		cp.changedAt = now
	}
	cp.fetchErr = false
	cp.version = version

	return cp.changedAt, true
}

func fetchExpectedConfigVersion(ctx context.Context, sdk sdkkonnectgo.DPNodesSDK, controlPlaneID string) (string, error) {
	resp, err := sdk.GetExpectedConfigVersion(ctx, controlPlaneID)
	if err != nil {
		return "", err
	}
	version := resp.GetGetExpectedConfigVersionResponse().GetExpectedConfigVersion()
	if version == "" {
		return "", errors.New("empty expected config version")
	}
	return version, nil
}

func (t *ConfigVersionTracker) record(uid types.UID) (entitySyncRecord, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.entities[uid]
	return rec, ok
}

func (t *ConfigVersionTracker) store(uid types.UID, rec entitySyncRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entities[uid] = rec
}

// syncOutcome describes what an update of a Konnect entity guarded by a syncGuard did.
type syncOutcome int

const (
	// syncOutcomeSent means the entity was sent to Konnect.
	syncOutcomeSent syncOutcome = iota
	// syncOutcomeVerified means the entity was fetched from Konnect and found in sync, so it was not sent.
	syncOutcomeVerified
	// syncOutcomeSkipped means the entity was assumed in sync with Konnect and neither fetched nor sent.
	syncOutcomeSkipped
)

// syncGuard decides, for a single update of a Konnect entity, whether the entity has to be sent to Konnect.
// A nil syncGuard always sends.
type syncGuard struct {
	tracker        *ConfigVersionTracker
	sdk            sdkkonnectgo.DPNodesSDK
	uid            types.UID
	controlPlaneID string
	// periodic is true when the update is only due to the sync period having elapsed,
	// in which case it can be skipped or verified with a GET instead of being sent.
	periodic bool

	outcome syncOutcome
}

func (t *ConfigVersionTracker) guard(
	sdk sdkkonnectgo.DPNodesSDK,
	uid types.UID,
	controlPlaneID string,
	periodic bool,
) *syncGuard {
	if t == nil || controlPlaneID == "" || uid == "" {
		return nil
	}
	return &syncGuard{
		tracker:        t,
		sdk:            sdk,
		uid:            uid,
		controlPlaneID: controlPlaneID,
		periodic:       periodic,
	}
}

// notSent reports whether the guarded update did not send the entity to Konnect.
func (g *syncGuard) notSent() bool {
	return g != nil && g.outcome != syncOutcomeSent
}

// skipped reports whether the guarded update neither fetched nor sent the entity.
func (g *syncGuard) skipped() bool {
	return g != nil && g.outcome == syncOutcomeSkipped
}

// invalidate drops what is known about the guarded entity, forcing it to be sent on the next update.
func (g *syncGuard) invalidate() {
	if g != nil {
		g.tracker.Forget(g.uid)
	}
}

// upsertUnlessInSync sends the entity to Konnect using upsert, unless the guard determines that Konnect already
// has it. konnectID and payload must contain everything that upsert sends to Konnect.
// get and upsert return the Konnect representation of the entity.
func upsertUnlessInSync[E any](
	ctx context.Context,
	g *syncGuard,
	konnectID string,
	payload any,
	get func(context.Context) (*E, error),
	upsert func(context.Context) (*E, error),
) error {
	if g == nil {
		_, err := upsert(ctx)
		return err
	}

	t := g.tracker
	payloadHash, err := hashJSON(struct {
		ControlPlaneID string `json:"control_plane_id"`
		ID             string `json:"id"`
		Payload        any    `json:"payload"`
	}{g.controlPlaneID, konnectID, payload})
	if err != nil {
		t.Forget(g.uid)
		_, err := upsert(ctx)
		return err
	}

	// Fetching the config version before sending ensures a baseline exists before
	// the first write, so that the write is attributed to the operator.
	changedAt, versionOK := t.lastExternalChange(ctx, g.sdk, g.controlPlaneID)

	if rec, ok := t.record(g.uid); g.periodic && versionOK && ok && rec.payloadHash == payloadHash {
		logger := ctrllog.FromContext(ctx)
		if changedAt.Before(rec.checkedAt) {
			log.Debug(logger, "entity unchanged and no external change in Konnect, skipping update")
			g.outcome = syncOutcomeSkipped
			return nil
		}

		checkedAt := t.now()

		ent, err := get(ctx)
		if err == nil && ent != nil {
			reprHash, err := representationHash(ent)
			if err == nil && reprHash == rec.reprHash {
				log.Debug(logger, "entity unchanged in Konnect, skipping update")
				t.store(g.uid, entitySyncRecord{payloadHash: payloadHash, reprHash: reprHash, checkedAt: checkedAt})
				g.outcome = syncOutcomeVerified
				return nil
			}
		}
		log.Debug(logger, "entity changed in Konnect or failed to get it, updating it")
	}

	sentAt := t.now()
	ent, err := upsert(ctx)
	if err != nil || ent == nil {
		t.Forget(g.uid)
		return err
	}
	rec := entitySyncRecord{payloadHash: payloadHash, checkedAt: sentAt}
	if reprHash, err := representationHash(ent); err == nil {
		rec.reprHash = reprHash
	}
	t.store(g.uid, rec)
	g.outcome = syncOutcomeSent
	return nil
}

func hashJSON(v any) (hash, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return hash{}, err
	}
	return sha256.Sum256(b), nil
}

// representationHash hashes the Konnect representation of an entity, ignoring the fields that Konnect
// changes without the entity changing.
func representationHash(v any) (hash, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return hash{}, err
	}
	var m any
	if err := json.Unmarshal(b, &m); err != nil {
		return hash{}, err
	}
	if obj, ok := m.(map[string]any); ok {
		delete(obj, "created_at")
		delete(obj, "updated_at")
	}
	return hashJSON(m)
}
