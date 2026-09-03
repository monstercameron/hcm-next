package coordinator

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/transaction"
)

var (
	ErrInvalidRequest      = errors.New("transaction coordinator: invalid commit request")
	ErrParticipantMismatch = errors.New("transaction coordinator: participant write mismatch")
	ErrCommitAmbiguous     = errors.New("transaction coordinator: commit outcome is ambiguous")
)

// TxFactory is the only capability the coordinator needs from a database
// pool. The coordinator never opens a second transaction or commits a caller's
// transaction on its behalf.
type TxFactory interface {
	Begin(context.Context) (dbport.Tx, error)
}

// Write is one correctness-bearing local operation. Apply must issue all its
// statements through tx and must not call an external service.
type Write struct {
	ParticipantID string
	Apply         func(context.Context, dbport.Tx) error
}

// Publish is an after-commit notification. It is never called if the local
// transaction rolls back or fails to commit.
type Publish func(context.Context, Receipt) error

type CommitRequest struct {
	Plan    transaction.Resolution
	Writes  []Write
	Publish Publish
}

type Receipt struct {
	PlanID           string
	ResolutionDigest string
	Participants     []string
}

// Commit atomically executes each admitted participant write. The write order
// is the resolution's canonical lock order (with participant order preserved
// within a stream), preventing two coordinators from deadlocking. Effects are
// not executed here; Publish runs only after a successful Commit.
func (c *Coordinator) Commit(ctx context.Context, req CommitRequest) (Receipt, error) {
	if c == nil || c.db == nil {
		return Receipt{}, fmt.Errorf("%w: no database", ErrInvalidRequest)
	}
	if err := req.Plan.VerifyDigest(); err != nil {
		return Receipt{}, err
	}
	if req.Plan.PlanID == "" || len(req.Plan.Admitted) == 0 {
		return Receipt{}, fmt.Errorf("%w: resolution has no admitted participants", ErrInvalidRequest)
	}
	byID := make(map[string]Write, len(req.Writes))
	for _, w := range req.Writes {
		if w.ParticipantID == "" || w.Apply == nil || byID[w.ParticipantID].Apply != nil {
			return Receipt{}, fmt.Errorf("%w: invalid or duplicate write %q", ErrParticipantMismatch, w.ParticipantID)
		}
		byID[w.ParticipantID] = w
	}
	if len(byID) != len(req.Plan.Admitted) {
		return Receipt{}, fmt.Errorf("%w: got %d writes for %d admitted participants", ErrParticipantMismatch, len(byID), len(req.Plan.Admitted))
	}
	ordered := orderedParticipants(req.Plan.Admitted, req.Plan.LockOrder)
	if len(ordered) != len(req.Plan.Admitted) {
		return Receipt{}, fmt.Errorf("%w: writes do not cover admitted participants", ErrParticipantMismatch)
	}
	for _, p := range ordered {
		if byID[p.ParticipantID].Apply == nil {
			return Receipt{}, fmt.Errorf("%w: missing participant %q", ErrParticipantMismatch, p.ParticipantID)
		}
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, p := range ordered {
		if err := byID[p.ParticipantID].Apply(ctx, tx); err != nil {
			return Receipt{}, fmt.Errorf("participant %s: %w", p.ParticipantID, err)
		}
	}
	receipt := Receipt{PlanID: req.Plan.PlanID, ResolutionDigest: req.Plan.Digest, Participants: participantIDs(ordered)}
	if err := tx.Commit(ctx); err != nil {
		return receipt, fmt.Errorf("%w: %v", ErrCommitAmbiguous, err)
	}
	if req.Publish != nil {
		if err := req.Publish(ctx, receipt); err != nil {
			return receipt, fmt.Errorf("publish after commit: %w", err)
		}
	}
	return receipt, nil
}

type Coordinator struct{ db TxFactory }

func New(db TxFactory) *Coordinator { return &Coordinator{db: db} }

func orderedParticipants(in []intent.PlanParticipant, lockOrder []string) []intent.PlanParticipant {
	out := make([]intent.PlanParticipant, 0, len(in))
	used := make(map[string]bool, len(in))
	for _, stream := range lockOrder {
		for _, p := range in {
			if !used[p.ParticipantID] && p.StreamID == stream {
				out = append(out, p)
				used[p.ParticipantID] = true
			}
		}
	}
	for _, p := range in {
		if !used[p.ParticipantID] {
			out = append(out, p)
		}
	}
	return out
}
func participantIDs(in []intent.PlanParticipant) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p.ParticipantID
	}
	return out
}
