package pipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

var (
	ErrEventInvalid   = errors.New("legal pipeline: invalid event entry")
	ErrEventDuplicate = errors.New("legal pipeline: duplicate event entry")
)

// EventEntry adds tenant and append-only sequence identity to a pipeline
// event. The event's own PrevDigest remains the chain link.
type EventEntry struct {
	TenantID      string
	RowID         string
	EventSequence int64
	Event         Event
}

func (e EventEntry) Validate() error {
	if e.TenantID == "" || e.RowID == "" || e.EventSequence < 1 {
		return fmt.Errorf("%w: tenant, row and positive event sequence are required", ErrEventInvalid)
	}
	if e.Event.Stage == "" || e.Event.PrincipalID == "" || e.Event.Role == "" || e.Event.ArtifactDigest == "" || e.Event.Digest == "" {
		return fmt.Errorf("%w: stage, principal, role, artifact and digest are required", ErrEventInvalid)
	}
	if len(e.Event.Signature.Bytes) == 0 || len(e.Event.Signature.PublicKey) == 0 {
		return fmt.Errorf("%w: signature is incomplete", ErrEventInvalid)
	}
	return nil
}

// EventStore is the semantic port for the pipeline's immutable hash chain.
type EventStore interface {
	AppendEvent(context.Context, EventEntry) (EventEntry, error)
	ListEvents(context.Context, string) ([]EventEntry, error)
}

// MemoryEventStore is a concurrency-safe append-only event store. It checks
// sequence continuity and the hash-chain predecessor before accepting an
// event, matching the durable adapter's read-side verification boundary.
type MemoryEventStore struct {
	mu      sync.RWMutex
	entries map[string]EventEntry
	order   []string
}

func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{entries: make(map[string]EventEntry)}
}

func eventKey(e EventEntry) string {
	return e.TenantID + ":" + fmt.Sprint(e.EventSequence)
}

func (s *MemoryEventStore) AppendEvent(ctx context.Context, in EventEntry) (EventEntry, error) {
	if err := ctx.Err(); err != nil {
		return EventEntry{}, err
	}
	if err := in.Validate(); err != nil {
		return EventEntry{}, err
	}
	if s == nil {
		return EventEntry{}, fmt.Errorf("%w: nil store", ErrEventInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := eventKey(in)
	if _, ok := s.entries[key]; ok {
		return EventEntry{}, fmt.Errorf("%w: %s", ErrEventDuplicate, key)
	}
	if len(s.order) == 0 {
		if in.EventSequence != 1 || in.Event.PrevDigest != "" {
			return EventEntry{}, fmt.Errorf("%w: first event must be sequence 1 at genesis", ErrEventInvalid)
		}
	} else {
		last := s.entries[s.order[len(s.order)-1]]
		if in.EventSequence != last.EventSequence+1 || in.Event.PrevDigest != last.Event.Digest {
			return EventEntry{}, fmt.Errorf("%w: event chain predecessor is stale", ErrEventInvalid)
		}
	}
	in.Event.Signature.PublicKey = slices.Clone(in.Event.Signature.PublicKey)
	in.Event.Signature.Bytes = slices.Clone(in.Event.Signature.Bytes)
	s.entries[key] = in
	s.order = append(s.order, key)
	return in, nil
}

func (s *MemoryEventStore) ListEvents(ctx context.Context, tenantID string) ([]EventEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrEventInvalid)
	}
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrEventInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []EventEntry
	for _, key := range s.order {
		entry := s.entries[key]
		if entry.TenantID != tenantID {
			continue
		}
		entry.Event.Signature.PublicKey = slices.Clone(entry.Event.Signature.PublicKey)
		entry.Event.Signature.Bytes = slices.Clone(entry.Event.Signature.Bytes)
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventSequence < out[j].EventSequence })
	return out, nil
}
