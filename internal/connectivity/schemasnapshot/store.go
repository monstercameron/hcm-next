package schemasnapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// Evidence is the durable record of one snapshot's admission decision: what
// each validator found, and what the snapshot's fate became. INTG-004
// requires "an evidence record either way" -- an [Evidence] value exists for
// a rejected snapshot exactly as it does for an admitted one.
type Evidence struct {
	EvidenceID uuid.UUID
	SnapshotID uuid.UUID
	TenantID   string
	// Verdict is StateAdmitted or StateRejected; it is never StateQuarantined.
	Verdict    State
	Results    []ValidatorResult
	DecidedAt  time.Time
	RecordedAt time.Time
}

// Validate reports whether the evidence is complete enough to record.
func (e Evidence) Validate() error {
	const op = "schemasnapshot.Evidence.Validate"
	switch {
	case e.EvidenceID == uuid.Nil:
		return newError(op, ErrIncomplete, "evidence has no id")
	case e.SnapshotID == uuid.Nil:
		return newError(op, ErrIncomplete, "evidence names no snapshot")
	case strings.TrimSpace(e.TenantID) == "":
		return newError(op, ErrIncomplete, "evidence has no tenant")
	case !e.Verdict.Terminal():
		return newError(op, ErrInvalid, "evidence verdict %q is not a terminal state", string(e.Verdict))
	case len(e.Results) == 0:
		return newError(op, ErrIncomplete, "evidence carries no validator results")
	case e.DecidedAt.IsZero():
		return newError(op, ErrIncomplete, "evidence has no decision time")
	}
	return nil
}

func cloneEvidence(e Evidence) Evidence {
	out := e
	out.Results = append([]ValidatorResult(nil), e.Results...)
	return out
}

// ArtifactPutRequest is what [Ingest] asks an [ArtifactStore] to write: the
// raw schema bytes plus the identity metadata
// internal/data/artifacts.PutRequest itself requires.
type ArtifactPutRequest struct {
	TenantID            string
	Raw                 []byte
	MediaType           string
	Classification      model.ClassificationLabel
	RetentionClass      string
	CreatorPrincipalRef string
	EvidenceID          string
}

// ArtifactStore is the minimal capability [Ingest] needs from the content-
// addressed artifact store: write raw bytes once and learn the content id
// they hash to. [adapters/postgres.ArtifactStore] wraps
// internal/data/artifacts.Put over the caller's transaction; [MemoryArtifactStore]
// satisfies it for tests that need no database.
type ArtifactStore interface {
	// Put stores req.Raw and returns its content id, its byte size, and
	// whether this call was the one that first wrote it (false means
	// byte-identical content was already on file).
	Put(ctx context.Context, req ArtifactPutRequest) (contentID string, byteSize int64, created bool, err error)
}

// Querier is the database capability the PostgreSQL [Store] adapter needs,
// stated in dbport's driver-free terms -- the same technique
// internal/connectivity/observe.Querier uses. Both a [dbport.Conn] and a
// [dbport.Tx] satisfy it.
type Querier interface {
	dbport.Execer
	dbport.Querier
}

// Store persists [SchemaSnapshot] rows and their deciding [Evidence].
//
// Insert must be idempotent by [SchemaSnapshot.SnapshotID]: re-ingesting an
// identical snapshot returns the row already on file rather than a second
// one, and different identity metadata under an existing id is
// [ErrImmutable]. Decide must be idempotent too: called against an
// already-decided snapshot with the same verdict, it returns the existing
// decision unchanged; called with a different verdict, it is [ErrImmutable].
// Both properties together are what let [Ingest] be retried at any point
// (after a crash between steps, or by a concurrent caller) without ever
// producing two decisions for one snapshot.
type Store interface {
	Insert(ctx context.Context, snap SchemaSnapshot) (result SchemaSnapshot, created bool, err error)
	Decide(ctx context.Context, tenantID string, id uuid.UUID, verdict State, reason string, decidedAt time.Time, results []ValidatorResult) (SchemaSnapshot, Evidence, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (SchemaSnapshot, error)
	Evidence(ctx context.Context, tenantID string, id uuid.UUID) (Evidence, bool, error)
}

// MemoryStore is an in-memory [Store]. Like
// internal/connectivity/observe.MemoryStore, it is not a test-only
// convenience: it implements the same identity, idempotency and immutability
// rules the PostgreSQL adapter does, so a conformance-style test can run both
// through the same cases.
type MemoryStore struct {
	mu        sync.RWMutex
	snapshots map[string]map[uuid.UUID]SchemaSnapshot
	evidence  map[string]map[uuid.UUID]Evidence
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		snapshots: make(map[string]map[uuid.UUID]SchemaSnapshot),
		evidence:  make(map[string]map[uuid.UUID]Evidence),
	}
}

var _ Store = (*MemoryStore)(nil)

// Insert implements [Store].
func (m *MemoryStore) Insert(ctx context.Context, snap SchemaSnapshot) (SchemaSnapshot, bool, error) {
	const op = "schemasnapshot.MemoryStore.Insert"
	if err := ctx.Err(); err != nil {
		return SchemaSnapshot{}, false, newError(op, ErrStore, "context: %v", err)
	}
	if snap.State != StateQuarantined {
		return SchemaSnapshot{}, false, newError(op, ErrInvalid, "Insert only accepts a snapshot in StateQuarantined")
	}
	if err := snap.Validate(); err != nil {
		return SchemaSnapshot{}, false, err
	}
	if snap.Supersedes != nil {
		prior, ok := m.snapshotLocked(snap.TenantID, *snap.Supersedes)
		if !ok {
			return SchemaSnapshot{}, false, newError(op, ErrIncomplete,
				"supersedes_ref %s names no existing snapshot for this tenant", *snap.Supersedes)
		}
		if !prior.Provider.Equal(snap.Provider) {
			return SchemaSnapshot{}, false, newError(op, ErrInvalid,
				"snapshot %s cannot supersede %s of a different provider", snap.SnapshotID, *snap.Supersedes)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	byTenant, ok := m.snapshots[snap.TenantID]
	if !ok {
		byTenant = make(map[uuid.UUID]SchemaSnapshot)
		m.snapshots[snap.TenantID] = byTenant
	}
	if existing, found := byTenant[snap.SnapshotID]; found {
		if !sameIdentity(existing, snap) {
			return SchemaSnapshot{}, false, newError(op, ErrImmutable,
				"snapshot %s is already stored with different identity metadata", snap.SnapshotID)
		}
		return cloneSnapshot(existing), false, nil
	}
	snap.RecordedAt = time.Now().UTC()
	byTenant[snap.SnapshotID] = cloneSnapshot(snap)
	return cloneSnapshot(snap), true, nil
}

func (m *MemoryStore) snapshotLocked(tenantID string, id uuid.UUID) (SchemaSnapshot, bool) {
	snap, ok := m.snapshots[tenantID][id]
	return snap, ok
}

// Decide implements [Store].
func (m *MemoryStore) Decide(ctx context.Context, tenantID string, id uuid.UUID, verdict State, reason string, decidedAt time.Time, results []ValidatorResult) (SchemaSnapshot, Evidence, error) {
	const op = "schemasnapshot.MemoryStore.Decide"
	if err := ctx.Err(); err != nil {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrStore, "context: %v", err)
	}
	if !verdict.Terminal() {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrInvalid, "verdict %q is not a terminal state", string(verdict))
	}
	if strings.TrimSpace(reason) == "" {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrIncomplete, "decision has no reason")
	}
	if decidedAt.IsZero() {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrIncomplete, "decision has no time")
	}
	if len(results) == 0 {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrIncomplete, "decision carries no validator results")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	snap, ok := m.snapshots[tenantID][id]
	if !ok {
		return SchemaSnapshot{}, Evidence{}, newError(op, ErrNotFound, "tenant %s has no snapshot %s", tenantID, id)
	}
	if snap.State != StateQuarantined {
		if snap.State != verdict {
			return SchemaSnapshot{}, Evidence{}, newError(op, ErrImmutable,
				"snapshot %s is already decided %s; refusing to redecide as %s", id, snap.State, verdict)
		}
		return cloneSnapshot(snap), cloneEvidence(m.evidence[tenantID][id]), nil
	}

	snap.State = verdict
	snap.StateReason = reason
	snap.DecidedAt = decidedAt.UTC()
	m.snapshots[tenantID][id] = snap

	ev := Evidence{
		EvidenceID: uuid.New(),
		SnapshotID: id,
		TenantID:   tenantID,
		Verdict:    verdict,
		Results:    append([]ValidatorResult(nil), results...),
		DecidedAt:  decidedAt.UTC(),
		RecordedAt: time.Now().UTC(),
	}
	if err := ev.Validate(); err != nil {
		return SchemaSnapshot{}, Evidence{}, err
	}
	byTenant, ok := m.evidence[tenantID]
	if !ok {
		byTenant = make(map[uuid.UUID]Evidence)
		m.evidence[tenantID] = byTenant
	}
	byTenant[id] = cloneEvidence(ev)
	return cloneSnapshot(snap), cloneEvidence(ev), nil
}

// Get implements [Store].
func (m *MemoryStore) Get(ctx context.Context, tenantID string, id uuid.UUID) (SchemaSnapshot, error) {
	const op = "schemasnapshot.MemoryStore.Get"
	if err := ctx.Err(); err != nil {
		return SchemaSnapshot{}, newError(op, ErrStore, "context: %v", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.snapshots[tenantID][id]
	if !ok {
		return SchemaSnapshot{}, newError(op, ErrNotFound, "tenant %s has no snapshot %s", tenantID, id)
	}
	return cloneSnapshot(snap), nil
}

// Evidence implements [Store].
func (m *MemoryStore) Evidence(ctx context.Context, tenantID string, id uuid.UUID) (Evidence, bool, error) {
	const op = "schemasnapshot.MemoryStore.Evidence"
	if err := ctx.Err(); err != nil {
		return Evidence{}, false, newError(op, ErrStore, "context: %v", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ev, ok := m.evidence[tenantID][id]
	if !ok {
		return Evidence{}, false, nil
	}
	return cloneEvidence(ev), true, nil
}

// MemoryArtifactStore is an in-memory [ArtifactStore]: content-addressed by
// sha256, exactly like internal/data/artifacts.Put, but with no database and
// none of that package's retention/classification bookkeeping. It exists so
// domain-level tests of [Ingest] need no PostgreSQL instance; the real
// artifact store is always used in production via
// adapters/postgres.ArtifactStore.
type MemoryArtifactStore struct {
	mu   sync.Mutex
	byID map[string]map[string][]byte
}

// NewMemoryArtifactStore returns an empty in-memory artifact store.
func NewMemoryArtifactStore() *MemoryArtifactStore {
	return &MemoryArtifactStore{byID: make(map[string]map[string][]byte)}
}

var _ ArtifactStore = (*MemoryArtifactStore)(nil)

// Put implements [ArtifactStore].
func (m *MemoryArtifactStore) Put(ctx context.Context, req ArtifactPutRequest) (string, int64, bool, error) {
	const op = "schemasnapshot.MemoryArtifactStore.Put"
	if err := ctx.Err(); err != nil {
		return "", 0, false, newError(op, ErrStore, "context: %v", err)
	}
	if err := validateArtifactPutRequest(req); err != nil {
		return "", 0, false, err
	}
	sum := sha256.Sum256(req.Raw)
	id := hex.EncodeToString(sum[:])

	m.mu.Lock()
	defer m.mu.Unlock()
	byID, ok := m.byID[req.TenantID]
	if !ok {
		byID = make(map[string][]byte)
		m.byID[req.TenantID] = byID
	}
	if existing, found := byID[id]; found {
		return id, int64(len(existing)), false, nil
	}
	byID[id] = append([]byte(nil), req.Raw...)
	return id, int64(len(req.Raw)), true, nil
}

func validateArtifactPutRequest(req ArtifactPutRequest) error {
	const op = "schemasnapshot.ArtifactPutRequest.Validate"
	switch {
	case strings.TrimSpace(req.TenantID) == "":
		return newError(op, ErrIncomplete, "artifact write has no tenant")
	case len(req.Raw) == 0:
		return newError(op, ErrIncomplete, "artifact write carries no bytes")
	case strings.TrimSpace(req.MediaType) == "":
		return newError(op, ErrIncomplete, "artifact write has no media type")
	case !req.Classification.Valid():
		return newError(op, ErrIncomplete, "artifact write has no valid classification")
	case strings.TrimSpace(req.RetentionClass) == "":
		return newError(op, ErrIncomplete, "artifact write has no retention class")
	case strings.TrimSpace(req.CreatorPrincipalRef) == "":
		return newError(op, ErrIncomplete, "artifact write has no creator principal")
	case strings.TrimSpace(req.EvidenceID) == "":
		return newError(op, ErrIncomplete, "artifact write has no evidence id")
	}
	return nil
}
