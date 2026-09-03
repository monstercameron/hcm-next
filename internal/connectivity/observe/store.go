package observe

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/connectivity"
)

// AppendResult reports what an append did. Existing is true when the store
// already held byte-identical evidence under the same identity, which is the
// normal outcome of a re-read and must not be reported as a failure.
type AppendResult struct {
	Observation Observation
	Existing    bool
}

// Query selects observations. Zero-valued fields are unconstrained except
// TenantID, which is always required: there is no cross-tenant read.
type Query struct {
	TenantID     string
	ConnectionID string
	Object       connectivity.ObjectKind
	SnapshotID   string
	// FromPage and ToPage bound the page sequence inclusively. Zero means
	// unbounded on that side.
	FromPage uint64
	ToPage   uint64
	// Limit caps the result. Zero means no cap.
	Limit int
}

// ObservationStore persists immutable observation evidence.
//
// Append must be idempotent by observation identity and must refuse to replace
// existing evidence with different content. Those two rules together are what
// let a run be restarted at any point without either losing a page or
// inventing a second version of one.
type ObservationStore interface {
	// Append stores an observation, or returns the identical one already
	// stored. Different content under an existing identity is [ErrImmutable].
	Append(ctx context.Context, obs Observation) (AppendResult, error)
	// Get returns one observation by tenant and id.
	Get(ctx context.Context, tenantID string, id uuid.UUID) (Observation, error)
	// List returns matching observations ordered by (object, snapshot, page).
	List(ctx context.Context, q Query) ([]Observation, error)
}

// CheckpointKey addresses one traversal's resume point.
type CheckpointKey struct {
	TenantID     string
	ConnectionID string
	Object       connectivity.ObjectKind
}

// Checkpoint is the fenced resume point of one traversal.
//
// Fence is the whole reason this is not just a stored cursor. Two workers can
// believe they own the same traversal; the one holding the older fence must
// lose, and it must lose at commit time rather than by convention.
type Checkpoint struct {
	Key CheckpointKey
	// RunID identifies the run that last committed.
	RunID string
	// SnapshotID pins the source version the cursor addresses.
	SnapshotID string
	// CursorToken is the opaque position to resume from.
	CursorToken string
	// Fence is strictly increasing per key.
	Fence uint64
	// PagesCommitted and RecordsCommitted are the totals observed so far.
	PagesCommitted   uint64
	RecordsCommitted uint64
	// Complete states the traversal finished under this snapshot.
	Complete bool
	// UpdatedAt is when the checkpoint was committed.
	UpdatedAt time.Time
}

// Cursor decodes the checkpoint's resume position.
func (c Checkpoint) Cursor() (connectivity.Cursor, error) {
	return connectivity.ParseCursor(c.CursorToken)
}

// Validate reports whether the checkpoint can be committed.
func (c Checkpoint) Validate() error {
	const op = "observe.Checkpoint.Validate"
	switch {
	case c.Key.TenantID == "":
		return newError(op, ErrIncomplete, "checkpoint has no tenant")
	case c.Key.ConnectionID == "":
		return newError(op, ErrIncomplete, "checkpoint has no connection")
	case !c.Key.Object.Valid():
		return newError(op, ErrIncomplete, "checkpoint has no object kind")
	case c.RunID == "":
		return newError(op, ErrIncomplete, "checkpoint has no run id")
	case c.SnapshotID == "":
		return newError(op, ErrIncomplete, "checkpoint has no snapshot")
	case c.Fence == 0:
		return newError(op, ErrIncomplete, "checkpoint fence is 1-based")
	case c.UpdatedAt.IsZero():
		return newError(op, ErrIncomplete, "checkpoint has no update time")
	}
	return nil
}

// CheckpointStore persists fenced resume points.
type CheckpointStore interface {
	// Load returns the stored checkpoint, if any.
	Load(ctx context.Context, key CheckpointKey) (Checkpoint, bool, error)
	// Commit stores a checkpoint whose fence strictly exceeds the stored one.
	// A commit at or behind the stored fence is [ErrFenced].
	Commit(ctx context.Context, cp Checkpoint) error
}

// Querier is the subset of pgx this package's PostgreSQL adapter needs. Both
// *pgx.Conn and pgx.Tx satisfy it, so a caller decides whether an append
// joins an existing transaction without the adapter knowing.
//
// The port lives here, beside the consumer that defines what evidence
// persistence means, rather than in the adapter package: the adapter only
// implements it (see internal/connectivity/observe/adapters/postgres).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// MemoryStore is an in-memory [ObservationStore] and [CheckpointStore].
//
// It is not a test-only convenience: the same semantics - deterministic
// identity, idempotent append, immutable content, fenced commit - are what the
// Postgres adapter implements, and a conformance test runs both through the
// same cases.
type MemoryStore struct {
	mu           sync.RWMutex
	observations map[string]map[uuid.UUID]Observation
	checkpoints  map[CheckpointKey]Checkpoint
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		observations: make(map[string]map[uuid.UUID]Observation),
		checkpoints:  make(map[CheckpointKey]Checkpoint),
	}
}

var (
	_ ObservationStore = (*MemoryStore)(nil)
	_ CheckpointStore  = (*MemoryStore)(nil)
)

// Append implements [ObservationStore].
func (m *MemoryStore) Append(ctx context.Context, obs Observation) (AppendResult, error) {
	const op = "observe.MemoryStore.Append"
	if err := ctx.Err(); err != nil {
		return AppendResult{}, newError(op, ErrStore, "context: %v", err)
	}
	if err := obs.Verify(); err != nil {
		return AppendResult{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	byTenant, ok := m.observations[obs.TenantID]
	if !ok {
		byTenant = make(map[uuid.UUID]Observation)
		m.observations[obs.TenantID] = byTenant
	}
	if existing, found := byTenant[obs.ObservationID]; found {
		if !existing.SameContent(obs) {
			return AppendResult{}, newError(op, ErrImmutable,
				"observation %s is stored with digest %s; refusing to replace it with %s",
				obs.ObservationID, existing.ContentDigest, obs.ContentDigest)
		}
		return AppendResult{Observation: existing, Existing: true}, nil
	}
	byTenant[obs.ObservationID] = cloneObservation(obs)
	return AppendResult{Observation: obs}, nil
}

// Get implements [ObservationStore].
func (m *MemoryStore) Get(ctx context.Context, tenantID string, id uuid.UUID) (Observation, error) {
	const op = "observe.MemoryStore.Get"
	if err := ctx.Err(); err != nil {
		return Observation{}, newError(op, ErrStore, "context: %v", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	obs, ok := m.observations[tenantID][id]
	if !ok {
		return Observation{}, newError(op, ErrNotFound, "tenant %s has no observation %s", tenantID, id)
	}
	return cloneObservation(obs), nil
}

// List implements [ObservationStore].
func (m *MemoryStore) List(ctx context.Context, q Query) ([]Observation, error) {
	const op = "observe.MemoryStore.List"
	if err := ctx.Err(); err != nil {
		return nil, newError(op, ErrStore, "context: %v", err)
	}
	if q.TenantID == "" {
		return nil, newError(op, ErrIncomplete, "query has no tenant")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Observation, 0, len(m.observations[q.TenantID]))
	for _, obs := range m.observations[q.TenantID] {
		if matches(obs, q) {
			out = append(out, cloneObservation(obs))
		}
	}
	sortObservations(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Load implements [CheckpointStore].
func (m *MemoryStore) Load(ctx context.Context, key CheckpointKey) (Checkpoint, bool, error) {
	const op = "observe.MemoryStore.Load"
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, false, newError(op, ErrStore, "context: %v", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp, ok := m.checkpoints[key]
	return cp, ok, nil
}

// Commit implements [CheckpointStore].
func (m *MemoryStore) Commit(ctx context.Context, cp Checkpoint) error {
	const op = "observe.MemoryStore.Commit"
	if err := ctx.Err(); err != nil {
		return newError(op, ErrStore, "context: %v", err)
	}
	if err := cp.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.checkpoints[cp.Key]; ok && cp.Fence <= existing.Fence {
		return newError(op, ErrFenced,
			"checkpoint for %s/%s is at fence %d; refusing a commit at fence %d",
			cp.Key.ConnectionID, cp.Key.Object, existing.Fence, cp.Fence)
	}
	m.checkpoints[cp.Key] = cp
	return nil
}

func matches(obs Observation, q Query) bool {
	switch {
	case q.ConnectionID != "" && obs.ConnectionID != q.ConnectionID:
		return false
	case q.Object != "" && obs.Object != q.Object:
		return false
	case q.SnapshotID != "" && obs.SnapshotID != q.SnapshotID:
		return false
	case q.FromPage != 0 && obs.PageSequence < q.FromPage:
		return false
	case q.ToPage != 0 && obs.PageSequence > q.ToPage:
		return false
	default:
		return true
	}
}

func sortObservations(obs []Observation) {
	sort.Slice(obs, func(a, b int) bool {
		x, y := obs[a], obs[b]
		if x.Object != y.Object {
			return x.Object < y.Object
		}
		if x.SnapshotID != y.SnapshotID {
			return x.SnapshotID < y.SnapshotID
		}
		return x.PageSequence < y.PageSequence
	})
}

func cloneObservation(obs Observation) Observation {
	out := obs
	out.Payload = append([]byte(nil), obs.Payload...)
	if obs.RawArtifactRef != nil {
		ref := *obs.RawArtifactRef
		out.RawArtifactRef = &ref
	}
	return out
}

// Replay reads a traversal's observations back in page order and verifies
// them: every digest must reproduce, the page sequence must be contiguous from
// one, and the schema version must not change mid-traversal.
//
// It is the answer to "can you prove what you observed", and it answers using
// only what was stored.
func Replay(ctx context.Context, store ObservationStore, q Query) ([]Observation, error) {
	const op = "observe.Replay"
	observations, err := store.List(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(observations) == 0 {
		return nil, newError(op, ErrNotFound,
			"tenant %s has no observations matching the query", q.TenantID)
	}
	schema := observations[0].SchemaVersion
	for idx, obs := range observations {
		if err := obs.Verify(); err != nil {
			return nil, err
		}
		if want := uint64(idx + 1); obs.PageSequence != want {
			return nil, newError(op, ErrSequenceGap,
				"expected page %d of snapshot %s, found page %d", want, obs.SnapshotID, obs.PageSequence)
		}
		if obs.SchemaVersion != schema {
			return nil, newError(op, ErrSequenceGap,
				"page %d was observed under schema %s but page 1 under %s",
				obs.PageSequence, obs.SchemaVersion, schema)
		}
	}
	return observations, nil
}
