package ops

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

const (
	// versionReadTimeout bounds the config version read performed around a write.
	// It is deliberately independent from the write request's timeout.
	versionReadTimeout = 2 * time.Second

	// SyncHashVersion is the version of the payload and representation hash
	// scheme persisted in a status.SyncStatus.
	//
	// Bump it whenever the canonicalization or hashing changes. Entities whose
	// persisted hashes carry a different version are treated as unverified and
	// sent to Konnect once more.
	SyncHashVersion = "v1"

	hashPrefix = "sha256:"
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
// Writes performed by the operator itself also change the config version. They are reported through BeforeWrite and
// AfterWrite, which read the config version right before the first and right after the last of overlapping writes:
// a change before the writes is attributed to an external actor, a change during the writes to the operator.
// Only an external change made while an operator write is in flight goes undetected.
type ConfigVersionTracker struct {
	versionTTL       time.Duration
	driftCheckPeriod time.Duration
	now              func() time.Time

	mu            sync.Mutex
	rebaselineWG  sync.WaitGroup
	controlPlanes map[string]*controlPlaneConfigVersion
	entities      map[types.UID]entitySyncRecord
}

// NewConfigVersionTracker returns a ConfigVersionTracker that checks the config version of a control plane
// at most once every versionTTL. When driftCheckPeriod is positive, each entity is also verified with a GET
// at least once per driftCheckPeriod even when the config version did not change.
func NewConfigVersionTracker(versionTTL, driftCheckPeriod time.Duration) *ConfigVersionTracker {
	if driftCheckPeriod < 0 {
		driftCheckPeriod = 0
	}
	return &ConfigVersionTracker{
		versionTTL:       versionTTL,
		driftCheckPeriod: driftCheckPeriod,
		now:              time.Now,
		controlPlanes:    map[string]*controlPlaneConfigVersion{},
		entities:         map[types.UID]entitySyncRecord{},
	}
}

type controlPlaneConfigVersion struct {
	fetchMu sync.Mutex

	// Fields below are guarded by ConfigVersionTracker.mu.
	serverURL      string
	sdk            sdkkonnectgo.DPNodesSDK
	fetchedAt      time.Time
	fetchErr       bool
	version        string
	changedAt      time.Time
	ownWrite       bool
	writesInFlight int
}

type hash [sha256.Size]byte

func (h hash) String() string {
	return hashPrefix + hex.EncodeToString(h[:])
}

func parseHash(s string) (hash, bool) {
	if !strings.HasPrefix(s, hashPrefix) {
		return hash{}, false
	}
	b, err := hex.DecodeString(strings.TrimPrefix(s, hashPrefix))
	if err != nil || len(b) != sha256.Size {
		return hash{}, false
	}
	var h hash
	copy(h[:], b)
	return h, true
}

type entitySyncRecord struct {
	payloadHash hash
	reprHash    hash
	hasReprHash bool
	checkedAt   time.Time
}

// BeforeWrite must be called before the operator sends a write request to the given control plane.
// When no other write is in flight and none happened since the config version was last read, it reads the
// config version first, so that a change made before the write is not attributed to the operator.
func (t *ConfigVersionTracker) BeforeWrite(ctx context.Context, controlPlaneID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	cp := t.controlPlane(controlPlaneID)
	t.observeCurrentSDK(ctx, cp)
	if !cp.hasCleanBaseline() {
		if cp.version == "" {
			// Without a baseline the version read cannot tell an operator write
			// from an external change. Treat the current time as a potential
			// external change so entities checked before it are verified once
			// the version can be read.
			cp.changedAt = t.now()
		}
		cp.ownWrite = true
		cp.writesInFlight++
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	cp.fetchMu.Lock()
	defer cp.fetchMu.Unlock()

	t.mu.Lock()
	clean, sdk := cp.hasCleanBaseline(), cp.sdk
	t.mu.Unlock()
	var ok bool
	if clean {
		versionCtx, cancel := versionReadContext(ctx)
		_, ok = t.refresh(versionCtx, cp, controlPlaneID, sdk)
		cancel()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if clean && !ok {
		// We could not establish a clean pre-write baseline. We cannot tell
		// whether a version change after this point came from the operator or
		// from an external actor, so treat the current time as a potential
		// external change. Entities checked before it will be verified.
		cp.changedAt = t.now()
	}
	cp.ownWrite = true
	cp.writesInFlight++
}

// AfterWrite must be called after the operator received the response to a write request to the given control plane.
// When no other write is in flight, it reads the config version, so that a later change is attributed to an
// external actor.
func (t *ConfigVersionTracker) AfterWrite(ctx context.Context, controlPlaneID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	cp := t.controlPlane(controlPlaneID)
	t.observeCurrentSDK(ctx, cp)
	if cp.writesInFlight > 0 {
		cp.writesInFlight--
	}
	if !cp.needsRebaseline() {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Do not block the write response on the post-write version read. The
	// rebaseline is bounded by versionReadTimeout and coalesced by
	// needsRebaseline.
	t.rebaselineWG.Go(func() {

		cp.fetchMu.Lock()
		defer cp.fetchMu.Unlock()

		t.mu.Lock()
		rebaseline, sdk := cp.needsRebaseline(), cp.sdk
		t.mu.Unlock()
		if !rebaseline {
			return
		}

		versionCtx, cancel := versionReadContext(ctx)
		defer cancel()
		_, _ = t.refresh(versionCtx, cp, controlPlaneID, sdk)
	})
}

// waitForRebaseline waits for in-flight post-write version reads. It is used by
// tests to make asynchronous rebaselines deterministic.
func (t *ConfigVersionTracker) waitForRebaseline() {
	if t == nil {
		return
	}
	t.rebaselineWG.Wait()
}

// observeCurrentSDK records the SDK used by the current write. The SDK instance
// carries the current token, and its server URL identifies the Konnect instance
// the state belongs to. Reusing a cached SDK would keep using a rotated token.
func (t *ConfigVersionTracker) observeCurrentSDK(ctx context.Context, cp *controlPlaneConfigVersion) {
	wrapper, ok := sdkops.SDKFromContext(ctx)
	if !ok {
		return
	}
	serverURL := wrapper.GetServerURL()
	if cp.serverURL != "" && cp.serverURL != serverURL {
		// The same control plane ID is being used against a different Konnect
		// instance. Do not reuse the version state from the previous instance.
		cp.version = ""
		cp.changedAt = time.Time{}
		cp.fetchedAt = time.Time{}
		cp.fetchErr = false
	}
	cp.serverURL = serverURL
	cp.sdk = wrapper.GetDPNodesSDK()
}

// versionReadContext returns a context for a config version read that is not
// canceled when the write request is canceled or times out.
func versionReadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), versionReadTimeout)
}

func (cp *controlPlaneConfigVersion) hasCleanBaseline() bool {
	return !cp.ownWrite && cp.writesInFlight == 0 && cp.version != "" && cp.sdk != nil
}

func (cp *controlPlaneConfigVersion) needsRebaseline() bool {
	return cp.ownWrite && cp.writesInFlight == 0 && cp.version != "" && cp.sdk != nil
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
	t.observeCurrentSDK(ctx, cp)
	if cp.sdk == nil {
		cp.sdk = sdk
	}
	if cp.sdk == nil {
		t.mu.Unlock()
		return time.Time{}, false
	}
	sdk = cp.sdk
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

	return t.refresh(ctx, cp, controlPlaneID, sdk)
}

// refresh reads the config version of the control plane and records an external change when it differs from
// the previous one and the operator did not write to the control plane in between.
// The caller must hold cp.fetchMu.
func (t *ConfigVersionTracker) refresh(
	ctx context.Context,
	cp *controlPlaneConfigVersion,
	controlPlaneID string,
	sdk sdkkonnectgo.DPNodesSDK,
) (time.Time, bool) {
	version, err := fetchExpectedConfigVersion(ctx, sdk, controlPlaneID)

	t.mu.Lock()
	defer t.mu.Unlock()

	// The change could have happened any time before the response, so it is
	// timestamped after it: entities checked before are considered stale.
	now := t.now()
	ownWrite := cp.ownWrite
	// A write still in flight may change the version after the response.
	cp.ownWrite = cp.writesInFlight > 0
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
	if sdk == nil {
		return "", errors.New("nil DPNodes SDK")
	}
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

// driftCheckDue reports whether the entity should be verified even when no
// control plane config version change was observed.
func (t *ConfigVersionTracker) driftCheckDue(uid types.UID, rec entitySyncRecord) bool {
	if t.driftCheckPeriod <= 0 || rec.checkedAt.IsZero() {
		return false
	}
	return !t.now().Before(rec.checkedAt.Add(t.driftCheckPeriod + t.driftJitter(uid)))
}

// driftJitter returns a deterministic per-entity offset so a large control
// plane does not verify all its entities at the same time.
func (t *ConfigVersionTracker) driftJitter(uid types.UID) time.Duration {
	maxJitter := t.driftCheckPeriod / 10
	if maxJitter <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(uid))
	return time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(maxJitter))
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
	// persisted is the sync state loaded from the entity status, when present.
	// Its hashes are unverified until a GET confirms them.
	persisted *configurationv1alpha1.SyncStatus
	// pending is the record produced by a real PUT. It is persisted in status by
	// the reconcile and committed to memory only after the status patch succeeds.
	pending *entitySyncRecord

	outcome syncOutcome
}

func (t *ConfigVersionTracker) guard(
	sdk sdkkonnectgo.DPNodesSDK,
	uid types.UID,
	controlPlaneID string,
	periodic bool,
	persisted *configurationv1alpha1.SyncStatus,
) *syncGuard {
	if t == nil || sdk == nil || controlPlaneID == "" || uid == "" {
		return nil
	}
	return &syncGuard{
		tracker:        t,
		sdk:            sdk,
		uid:            uid,
		controlPlaneID: controlPlaneID,
		periodic:       periodic,
		persisted:      persisted,
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

// recordFromStatus returns the persisted sync state as an in-memory record with
// a zero checkedAt, so the first reconcile after a restart verifies the entity
// with a GET instead of trusting it.
//
// Missing hashes and hashes from another scheme are unverified: they produce no
// record, so the entity is sent to Konnect and recorded again.
func (g *syncGuard) recordFromStatus() (entitySyncRecord, bool) {
	if g == nil || g.persisted == nil || g.persisted.HashVersion != SyncHashVersion {
		return entitySyncRecord{}, false
	}
	payloadHash, payloadOK := parseHash(g.persisted.PayloadHash)
	reprHash, reprOK := parseHash(g.persisted.RepresentationHash)
	if !payloadOK || !reprOK {
		return entitySyncRecord{}, false
	}
	return entitySyncRecord{
		payloadHash: payloadHash,
		reprHash:    reprHash,
		hasReprHash: true,
	}, true
}

// pendingStatus returns the status representation of the record produced by a
// real PUT, or nil when the update did not send the entity to Konnect.
func (g *syncGuard) pendingStatus() *configurationv1alpha1.SyncStatus {
	if g == nil || g.pending == nil {
		return nil
	}
	status := &configurationv1alpha1.SyncStatus{
		PayloadHash: g.pending.payloadHash.String(),
		HashVersion: SyncHashVersion,
	}
	if g.pending.hasReprHash {
		status.RepresentationHash = g.pending.reprHash.String()
	}
	return status
}

// pendingRecord returns the in-memory record produced by a real PUT. It must be
// committed only after the status carrying the same record is persisted.
func (g *syncGuard) pendingRecord() (entitySyncRecord, bool) {
	if g == nil || g.pending == nil {
		return entitySyncRecord{}, false
	}
	return *g.pending, true
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
	payloadHash, err := entityPayloadHash(g.controlPlaneID, konnectID, payload)
	if err != nil {
		t.Forget(g.uid)
		_, err := upsert(ctx)
		return err
	}

	// Fetching the config version before sending ensures a baseline exists before
	// the first write, so that the write is attributed to the operator.
	changedAt, versionOK := t.lastExternalChange(ctx, g.sdk, g.controlPlaneID)

	rec, ok := t.record(g.uid)
	if !ok {
		rec, ok = g.recordFromStatus()
	}
	if g.periodic && versionOK && ok && rec.payloadHash == payloadHash {
		logger := ctrllog.FromContext(ctx)
		if !t.driftCheckDue(g.uid, rec) && changedAt.Before(rec.checkedAt) {
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
				t.store(g.uid, entitySyncRecord{
					payloadHash: payloadHash,
					reprHash:    reprHash,
					hasReprHash: true,
					checkedAt:   checkedAt,
				})
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
	rec = entitySyncRecord{payloadHash: payloadHash, checkedAt: sentAt}
	if reprHash, err := representationHash(ent); err == nil {
		rec.reprHash = reprHash
		rec.hasReprHash = true
	}
	// Do not advance the in-memory record before the status carrying it is
	// persisted. The caller commits it after a successful status patch.
	g.pending = &rec
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

// entityPayloadHash hashes the desired Konnect request together with the control
// plane and entity identifiers so changes to either invalidate the cached hash.
func entityPayloadHash(controlPlaneID, konnectID string, payload any) (hash, error) {
	return hashJSON(struct {
		ControlPlaneID string `json:"control_plane_id"`
		ID             string `json:"id"`
		Payload        any    `json:"payload"`
	}{controlPlaneID, konnectID, payload})
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
	return hashJSON(canonicalizeRepresentation(m))
}

// canonicalizeRepresentation makes a Konnect representation stable across
// equivalent responses: object keys are sorted by encoding/json, and fields
// Konnect changes without the entity changing are removed recursively.
func canonicalizeRepresentation(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			if isVolatileRepresentationField(k) {
				continue
			}
			out[k] = canonicalizeRepresentation(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = canonicalizeRepresentation(child)
		}
		return out
	default:
		return value
	}
}

func isVolatileRepresentationField(key string) bool {
	switch key {
	case "created_at", "updated_at":
		return true
	default:
		return false
	}
}
