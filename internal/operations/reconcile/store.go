package reconcile

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store persists reconciliation-job rows.
//
// Create must be idempotent on (tenant, effect, policy): a second create for
// the same triple returns the row already stored rather than a second one.
// Advance must be a compare-and-swap on the stored version: a write presented
// with a version the store no longer holds is [ErrVersionConflict] and
// changes nothing. Those two rules together are what let a caller restart at
// any point without losing a job or inventing a duplicate.
type Store interface {
	// Create inserts a new job, or returns the job already stored for
	// (tenant, effect_ref, policy_ref) with existing=true.
	Create(ctx context.Context, ex Executor, job Job) (stored Job, existing bool, err error)
	// Load returns one job by id.
	Load(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error)
	// LoadForUpdate is Load taking a row lock, so a concurrent advance of the
	// same job serializes on it rather than racing past a stale read.
	LoadForUpdate(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error)
	// Due returns the tenant's still-open jobs (PENDING, OBSERVING or
	// UNKNOWN) whose NextCheckAt is at or before asOf, soonest first, bounded
	// by limit (a limit below one reads one page of 100).
	Due(ctx context.Context, ex Executor, tenantID uuid.UUID, asOf time.Time, limit int) ([]Job, error)
	// Advance writes every mutable field of next under compare-and-swap on
	// expectedVersion. A mismatch is [ErrVersionConflict].
	Advance(ctx context.Context, ex Executor, next Job, expectedVersion uint64) error
}

// MemoryStore is an in-memory [Store].
//
// It is not a test-only convenience: it implements the same semantics --
// deterministic identity, idempotent create, fenced-by-version advance -- that
// [PostgresStore] implements, which is what lets [Coordinator]'s pure-logic
// tests (GOLDEN, MUTATION) exercise the full trigger/observe/compare/settle
// state machine without a database, while the PostgreSQL-backed tests prove
// the same rules survive a real restart.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[uuid.UUID]map[uuid.UUID]Job    // tenant -> job id -> job
	byNK map[uuid.UUID]map[string]uuid.UUID // tenant -> "effect\x1fpolicy" -> job id
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID: make(map[uuid.UUID]map[uuid.UUID]Job),
		byNK: make(map[uuid.UUID]map[string]uuid.UUID),
	}
}

var _ Store = (*MemoryStore)(nil)

func naturalKey(effectRef, policyRef string) string { return effectRef + "\x1f" + policyRef }

// Create implements [Store].
func (m *MemoryStore) Create(ctx context.Context, ex Executor, job Job) (Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, false, wrapErr(CodeStorageFailed, ErrStorage, job.TenantID, job.JobID, err, "context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	nk := naturalKey(job.EffectRef, job.PolicyRef)
	if byNK, ok := m.byNK[job.TenantID]; ok {
		if existingID, ok := byNK[nk]; ok {
			return m.byID[job.TenantID][existingID], true, nil
		}
	} else {
		m.byNK[job.TenantID] = make(map[string]uuid.UUID)
	}
	if _, ok := m.byID[job.TenantID]; !ok {
		m.byID[job.TenantID] = make(map[uuid.UUID]Job)
	}
	m.byID[job.TenantID][job.JobID] = job
	m.byNK[job.TenantID][nk] = job.JobID
	return job, false, nil
}

// Load implements [Store].
func (m *MemoryStore) Load(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, wrapErr(CodeStorageFailed, ErrStorage, tenantID, jobID, err, "context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.byID[tenantID][jobID]
	if !ok {
		return Job{}, refuse(CodeNotFound, ErrNotFound, tenantID, jobID, "no such job")
	}
	return job, nil
}

// LoadForUpdate implements [Store]. A single mutex already serializes every
// method on this store, so it is [MemoryStore.Load] under another name.
func (m *MemoryStore) LoadForUpdate(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error) {
	return m.Load(ctx, ex, tenantID, jobID)
}

// Due implements [Store].
func (m *MemoryStore) Due(ctx context.Context, ex Executor, tenantID uuid.UUID, asOf time.Time, limit int) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, err, "context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.byID[tenantID]))
	for _, job := range m.byID[tenantID] {
		if !job.Status.Terminal() && !job.NextCheckAt.After(asOf) {
			out = append(out, job)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextCheckAt.Equal(out[j].NextCheckAt) {
			return out[i].JobID.String() < out[j].JobID.String()
		}
		return out[i].NextCheckAt.Before(out[j].NextCheckAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Advance implements [Store].
func (m *MemoryStore) Advance(ctx context.Context, ex Executor, next Job, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return wrapErr(CodeStorageFailed, ErrStorage, next.TenantID, next.JobID, err, "context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.byID[next.TenantID][next.JobID]
	if !ok {
		return refuse(CodeNotFound, ErrNotFound, next.TenantID, next.JobID, "no such job")
	}
	if current.Version != expectedVersion {
		return refuse(CodeVersionConflict, ErrVersionConflict, next.TenantID, next.JobID,
			"job is at version %d, expected %d", current.Version, expectedVersion)
	}
	next.Version = current.Version + 1
	m.byID[next.TenantID][next.JobID] = next
	return nil
}
