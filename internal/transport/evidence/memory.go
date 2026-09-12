package evidence

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

// MemoryLineageSource is a deterministic conformance [LineageSource], the
// same role internal/transport/operations.MemoryStore plays for the
// operation resource: a composition root wires a real adapter over
// intent/proposal/approval/ledger/reconciliation stores in production; this
// is what tests and endpoint wiring use instead.
//
// It deliberately keeps two separate buckets. frozen is the only one
// Snapshot ever reads: the immutable dimension facts as they were recorded
// when an intent closed. current exists only so a test can simulate "the
// world moved after the intent closed" and prove Snapshot ignored it --
// production code has no reason to call SetCurrentMarker at all.
type MemoryLineageSource struct {
	mu      sync.RWMutex
	frozen  map[string]LineageSnapshot
	current map[string]string
}

func NewMemoryLineageSource() *MemoryLineageSource {
	return &MemoryLineageSource{frozen: make(map[string]LineageSnapshot), current: make(map[string]string)}
}

func lineageKey(tenant, intentID string) string { return tenant + "\x00" + intentID }

// Seed records the immutable snapshot for one intent, exactly as a
// production adapter would once, when the intent closed. It deep-copies the
// input so a caller mutating its own slice afterward cannot reach back into
// the store.
func (m *MemoryLineageSource) Seed(tenant, intentID string, snap LineageSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap.AuthorityLineage = append([]string(nil), snap.AuthorityLineage...)
	snap.Dimensions = append([]DimensionFact(nil), snap.Dimensions...)
	m.frozen[lineageKey(tenant, intentID)] = snap
}

// SetCurrentMarker records a value in the "current domain state" decoy
// bucket Snapshot never reads. Tests use this to prove ambient-data
// avoidance; it has no effect on Snapshot's output.
func (m *MemoryLineageSource) SetCurrentMarker(tenant, intentID, marker string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current[lineageKey(tenant, intentID)] = marker
}

// Snapshot implements [LineageSource]. It reads only the frozen bucket.
func (m *MemoryLineageSource) Snapshot(ctx context.Context, tenant, intentID string) (LineageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return LineageSnapshot{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.frozen[lineageKey(tenant, intentID)]
	if !ok {
		return LineageSnapshot{}, ErrLineageNotFound
	}
	snap.AuthorityLineage = append([]string(nil), snap.AuthorityLineage...)
	snap.Dimensions = append([]DimensionFact(nil), snap.Dimensions...)
	return snap, nil
}

// MemoryReceiptStore is a deterministic conformance [ReceiptStore].
type MemoryReceiptStore struct {
	mu      sync.RWMutex
	records map[string]Receipt
}

func NewMemoryReceiptStore() *MemoryReceiptStore {
	return &MemoryReceiptStore{records: make(map[string]Receipt)}
}

func (s *MemoryReceiptStore) Seed(r Receipt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[lineageKey(r.TenantID, r.ReceiptID)] = r
}

func (s *MemoryReceiptStore) Get(ctx context.Context, tenant, receiptID string) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[lineageKey(tenant, receiptID)]
	if !ok {
		return Receipt{}, ErrReceiptNotFound
	}
	r.Controls = append([]ControlVersion(nil), r.Controls...)
	return r, nil
}

// StaticPurposePolicy is a fixed, reviewed [PurposePolicy]: exactly the
// dimension names named for a purpose are visible; every other purpose is
// refused (ok=false), never silently permissive.
type StaticPurposePolicy map[string][]string

func (p StaticPurposePolicy) AllowedDimensions(purpose string) ([]string, bool) {
	allowed, ok := p[purpose]
	if !ok {
		return nil, false
	}
	return append([]string(nil), allowed...), true
}

// MemoryArtifactSink is a deterministic conformance [ArtifactSink].
type MemoryArtifactSink struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func NewMemoryArtifactSink() *MemoryArtifactSink {
	return &MemoryArtifactSink{objects: make(map[string][]byte)}
}

func artifactKey(tenant, id string) string { return tenant + "\x00" + id }

func (s *MemoryArtifactSink) Put(ctx context.Context, tenant, artifactID string, content []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[artifactKey(tenant, artifactID)]; exists {
		return "", fmt.Errorf("evidence: artifact %s already exists", artifactID)
	}
	s.objects[artifactKey(tenant, artifactID)] = append([]byte(nil), content...)
	s.puts++
	return "mem://evidence-artifacts/" + tenant + "/" + artifactID, nil
}

func (s *MemoryArtifactSink) Get(ctx context.Context, tenant, artifactID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.objects[artifactKey(tenant, artifactID)]
	if !ok {
		return nil, ErrArtifactNotFound
	}
	return append([]byte(nil), content...), nil
}

// Puts reports how many artifacts have been written; tests use it as one
// leg of the zero-source-mutation proof.
func (s *MemoryArtifactSink) Puts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// OperationJournal is this package's write-capable adapter over the one
// shared operations.Record concept (internal/transport/operations,
// EP-OPS-001). It implements operations.Store (Get/Cancel) so the existing
// OperationsService can serve exactly the records this package creates, and
// adds the create/complete/fail methods a long-running-work producer needs
// -- the same role internal/data/operationstore.Store.Put plays for the
// production Postgres adapter ("Put is used by the long-running work
// producer; the operation endpoint itself only exposes Get and Cancel").
//
// It never touches any table but its own: every method here reads or
// writes exactly the in-memory operation map, nothing else.
type OperationJournal struct {
	mu      sync.Mutex
	records map[string]operations.Record
	puts    int
	updates int
	now     func() time.Time
}

func NewOperationJournal(now func() time.Time) *OperationJournal {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &OperationJournal{records: make(map[string]operations.Record), now: now}
}

func opKey(tenant, id string) string { return tenant + "\x00" + id }

var errOperationExists = errors.New("evidence: operation already exists")

// Create writes the initial PENDING record for a new export. It refuses a
// duplicate operation id outright rather than silently overwriting it.
func (j *OperationJournal) Create(ctx context.Context, tenant, operationID, owner, requestType string) (operations.Record, error) {
	if err := ctx.Err(); err != nil {
		return operations.Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	key := opKey(tenant, operationID)
	if _, exists := j.records[key]; exists {
		return operations.Record{}, errOperationExists
	}
	now := j.now().UTC()
	rec := operations.Record{
		OperationID: operationID, TenantID: tenant, Owner: owner, RequestType: requestType,
		State: streaming.OperationPending, CreatedAt: now, UpdatedAt: now,
	}
	j.records[key] = rec
	j.puts++
	return rec, nil
}

// MarkRunning transitions a PENDING record to RUNNING.
func (j *OperationJournal) MarkRunning(ctx context.Context, tenant, operationID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	rec, ok := j.records[opKey(tenant, operationID)]
	if !ok {
		return operations.ErrNotFound
	}
	rec.State = streaming.OperationRunning
	rec.UpdatedAt = j.now().UTC()
	j.records[opKey(tenant, operationID)] = rec
	j.updates++
	return nil
}

// Complete transitions a record to SUCCEEDED with its typed result.
func (j *OperationJournal) Complete(ctx context.Context, tenant, operationID string, result *intentsv1.TypedPayload) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	rec, ok := j.records[opKey(tenant, operationID)]
	if !ok {
		return operations.ErrNotFound
	}
	rec.State = streaming.OperationSucceeded
	rec.Result = result
	rec.UpdatedAt = j.now().UTC()
	j.records[opKey(tenant, operationID)] = rec
	j.updates++
	return nil
}

// Fail transitions a record to FAILED with a typed error.
func (j *OperationJournal) Fail(ctx context.Context, tenant, operationID string, detail *commonv1.ErrorDetail) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	rec, ok := j.records[opKey(tenant, operationID)]
	if !ok {
		return operations.ErrNotFound
	}
	rec.State = streaming.OperationFailed
	rec.Error = detail
	rec.UpdatedAt = j.now().UTC()
	j.records[opKey(tenant, operationID)] = rec
	j.updates++
	return nil
}

// Get implements operations.Store.
func (j *OperationJournal) Get(ctx context.Context, tenant, id string) (operations.Record, error) {
	if err := ctx.Err(); err != nil {
		return operations.Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	rec, ok := j.records[opKey(tenant, id)]
	if !ok {
		return operations.Record{}, operations.ErrNotFound
	}
	return rec, nil
}

// Cancel implements operations.Store with the same idempotent semantics as
// operations.MemoryStore.Cancel.
func (j *OperationJournal) Cancel(ctx context.Context, tenant, id, _key, _reason string) (operations.Record, error) {
	if err := ctx.Err(); err != nil {
		return operations.Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	rec, ok := j.records[opKey(tenant, id)]
	if !ok {
		return operations.Record{}, operations.ErrNotFound
	}
	if rec.State.Terminal() {
		return rec, nil
	}
	rec.State = streaming.OperationCancellationRequested
	rec.UpdatedAt = j.now().UTC()
	j.records[opKey(tenant, id)] = rec
	j.updates++
	return rec, nil
}

// Writes reports {creates, updates}; tests use it as the other leg of the
// zero-source-mutation proof.
func (j *OperationJournal) Writes() (creates, updates int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.puts, j.updates
}

var _ operations.Store = (*OperationJournal)(nil)
