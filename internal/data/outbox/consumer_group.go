package outbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidGroupPolicy reports a consumer-group policy that cannot
	// govern: an empty group, a non-positive attempt bound, or a poison
	// entry without owner, TTL or repair route.
	ErrInvalidGroupPolicy = errors.New("outbox: invalid consumer-group policy")

	// ErrInvalidRecord reports a record without the identity dedupe needs.
	ErrInvalidRecord = errors.New("outbox: record carries no effect identity")

	// ErrCheckpointBeforeEffect reports a checkpoint for an effect the
	// group never applied: checkpoints commit with application, never ahead.
	ErrCheckpointBeforeEffect = errors.New("outbox: checkpoint advances before its effect")
)

// ApplyOutcome is one dispatch disposition.
type ApplyOutcome string

// Dispatch dispositions.
const (
	ApplyApplied         ApplyOutcome = "APPLIED"
	ApplyDuplicateFenced ApplyOutcome = "DUPLICATE_FENCED"
	ApplyTransientFailed ApplyOutcome = "TRANSIENT_FAILED"
	ApplyPoisonIsolated  ApplyOutcome = "POISON_ISOLATED"
)

// Fencing tells one handler whether external effects may run. Replays are
// structurally fenced: duplicates never reach the handler at all.
type Fencing struct {
	AllowExternalEffects bool
}

// EffectHandler applies one record's effect.
type EffectHandler func(ctx context.Context, record Record, fencing Fencing) error

// GroupPolicy bounds one consumer group.
type GroupPolicy struct {
	Group       string
	MaxAttempts int
	PoisonOwner string
	PoisonTTL   time.Duration
	RepairRoute string
	Clock       func() time.Time
}

// PoisonEntry isolates one poison record with its owner, expiry and
// repair route.
type PoisonEntry struct {
	Group          string
	Partition      string
	EffectIdentity string
	Owner          string
	ExpiresAt      time.Time
	RepairRoute    string
	Attempts       int
}

// Checkpoint commits one partition's applied offset.
type Checkpoint struct {
	Group        string
	Partition    string
	Offset       string
	AppliedCount int
}

// CheckpointStore persists checkpoints, applied identities and poison.
// MemoryCheckpointStore is the hermetic implementation; durable adapters
// implement the same port without importing data packages into callers.
type CheckpointStore interface {
	CheckpointFor(group, partition string) (Checkpoint, bool)
	SaveCheckpoint(cp Checkpoint)
	Applied(group, partition, identity string) bool
	RecordApplied(group, partition, identity string)
	ForgetApplied(group, partition, identity string)
	SavePoison(entry PoisonEntry)
	DropPoison(group, partition, identity string)
	Poisoned(group string) []PoisonEntry
}

// MemoryCheckpointStore is a mutex-guarded in-memory CheckpointStore.
type MemoryCheckpointStore struct {
	mu          sync.Mutex
	checkpoints map[string]Checkpoint
	applied     map[string]bool
	poison      map[string]PoisonEntry
}

// NewMemoryCheckpointStore returns an empty store.
func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{
		checkpoints: make(map[string]Checkpoint),
		applied:     make(map[string]bool),
		poison:      make(map[string]PoisonEntry),
	}
}

func checkpointKey(group, partition string) string { return group + "\x00" + partition }

func appliedKey(group, partition, identity string) string {
	return group + "\x00" + partition + "\x00" + identity
}

func poisonKey(group, partition, identity string) string {
	return group + "\x00" + partition + "\x00" + identity
}

// CheckpointFor returns one stored checkpoint.
func (s *MemoryCheckpointStore) CheckpointFor(group, partition string) (Checkpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp, ok := s.checkpoints[checkpointKey(group, partition)]
	return cp, ok
}

// SaveCheckpoint stores one checkpoint.
func (s *MemoryCheckpointStore) SaveCheckpoint(cp Checkpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[checkpointKey(cp.Group, cp.Partition)] = cp
}

// Applied reports whether one identity was applied.
func (s *MemoryCheckpointStore) Applied(group, partition, identity string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied[appliedKey(group, partition, identity)]
}

// RecordApplied marks one identity applied.
func (s *MemoryCheckpointStore) RecordApplied(group, partition, identity string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied[appliedKey(group, partition, identity)] = true
}

// ForgetApplied drops one applied identity for repair redispatch.
func (s *MemoryCheckpointStore) ForgetApplied(group, partition, identity string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.applied, appliedKey(group, partition, identity))
}

// SavePoison stores one poison entry.
func (s *MemoryCheckpointStore) SavePoison(entry PoisonEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poison[poisonKey(entry.Group, entry.Partition, entry.EffectIdentity)] = entry
}

// DropPoison removes one poison entry.
func (s *MemoryCheckpointStore) DropPoison(group, partition, identity string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.poison, poisonKey(group, partition, identity))
}

// Poisoned lists one group's poison entries in stable order.
func (s *MemoryCheckpointStore) Poisoned(group string) []PoisonEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PoisonEntry
	for _, entry := range s.poison {
		if entry.Group == group {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Partition != out[j].Partition {
			return out[i].Partition < out[j].Partition
		}
		return out[i].EffectIdentity < out[j].EffectIdentity
	})
	return out
}

// ConsumerGroup dispatches records with idempotent application,
// per-partition checkpoints, and per-record poison isolation. Poison in
// one tenant never blocks another: partitions are namespaced by tenant.
type ConsumerGroup struct {
	policy   GroupPolicy
	store    CheckpointStore
	mu       sync.Mutex
	attempts map[string]int
	clock    func() time.Time
}

// NewConsumerGroup validates one group policy.
func NewConsumerGroup(policy GroupPolicy) (*ConsumerGroup, error) {
	if policy.Group == "" || policy.MaxAttempts <= 0 || policy.PoisonOwner == "" ||
		policy.PoisonTTL <= 0 || policy.RepairRoute == "" {
		return nil, fmt.Errorf("outbox: NewConsumerGroup: %w", ErrInvalidGroupPolicy)
	}
	clock := policy.Clock
	if clock == nil {
		clock = time.Now
	}
	return &ConsumerGroup{policy: policy, attempts: make(map[string]int), clock: clock}, nil
}

// Bind attaches the group's checkpoint store.
func (g *ConsumerGroup) Bind(store CheckpointStore) { g.store = store }

func partitionOf(record Record) string {
	return record.Tenant.String() + "\x00" + record.OrderingKey
}

// Dispatch applies one record: first delivery runs the handler with
// external effects allowed; redeliveries are fenced duplicates; repeated
// failures isolate poison per record without blocking the partition.
func (g *ConsumerGroup) Dispatch(ctx context.Context, record Record, handler EffectHandler) (ApplyOutcome, error) {
	if record.EffectIdentity == "" {
		return "", fmt.Errorf("outbox: Dispatch: %w", ErrInvalidRecord)
	}
	if g.store == nil {
		return "", fmt.Errorf("outbox: Dispatch: %w", ErrInvalidGroupPolicy)
	}
	partition := partitionOf(record)
	key := appliedKey(g.policy.Group, partition, record.EffectIdentity)
	g.mu.Lock()
	poisoned := g.poisonedLocked(partition, record.EffectIdentity)
	seen := g.store.Applied(g.policy.Group, partition, record.EffectIdentity)
	g.mu.Unlock()
	if poisoned {
		return ApplyPoisonIsolated, nil
	}
	if seen {
		return ApplyDuplicateFenced, nil
	}
	if err := handler(ctx, record, Fencing{AllowExternalEffects: true}); err != nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.attempts[key]++
		if g.attempts[key] >= g.policy.MaxAttempts {
			g.store.SavePoison(PoisonEntry{
				Group:          g.policy.Group,
				Partition:      partition,
				EffectIdentity: record.EffectIdentity,
				Owner:          g.policy.PoisonOwner,
				ExpiresAt:      g.clock().Add(g.policy.PoisonTTL),
				RepairRoute:    g.policy.RepairRoute,
				Attempts:       g.attempts[key],
			})
			return ApplyPoisonIsolated, nil
		}
		return ApplyTransientFailed, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.store.RecordApplied(g.policy.Group, partition, record.EffectIdentity)
	delete(g.attempts, key)
	return ApplyApplied, nil
}

func (g *ConsumerGroup) poisonedLocked(partition, identity string) bool {
	for _, entry := range g.store.Poisoned(g.policy.Group) {
		if entry.Partition == partition && entry.EffectIdentity == identity {
			return true
		}
	}
	return false
}

// CommitCheckpoint commits one partition offset after its effect applied.
func (g *ConsumerGroup) CommitCheckpoint(ctx context.Context, orderingKey string, tenant uuid.UUID, identity string) error {
	_ = ctx
	partition := tenant.String() + "\x00" + orderingKey
	if !g.store.Applied(g.policy.Group, partition, identity) {
		return fmt.Errorf("outbox: CommitCheckpoint %s: %w", identity, ErrCheckpointBeforeEffect)
	}
	previous, _ := g.store.CheckpointFor(g.policy.Group, partition)
	g.store.SaveCheckpoint(Checkpoint{
		Group:        g.policy.Group,
		Partition:    partition,
		Offset:       identity,
		AppliedCount: previous.AppliedCount + 1,
	})
	return nil
}

// Poisoned lists one group's poison entries.
func (g *ConsumerGroup) Poisoned(group string) []PoisonEntry {
	return g.store.Poisoned(group)
}

// RequeueExpired drops expired poison and its applied marker so the repair
// route can redispatch. It returns the requeued identities.
func (g *ConsumerGroup) RequeueExpired(now time.Time) []string {
	var requeued []string
	for _, entry := range g.store.Poisoned(g.policy.Group) {
		if now.After(entry.ExpiresAt) {
			g.store.DropPoison(entry.Group, entry.Partition, entry.EffectIdentity)
			g.store.ForgetApplied(entry.Group, entry.Partition, entry.EffectIdentity)
			g.mu.Lock()
			delete(g.attempts, appliedKey(entry.Group, entry.Partition, entry.EffectIdentity))
			g.mu.Unlock()
			requeued = append(requeued, entry.EffectIdentity)
		}
	}
	return requeued
}
