package position_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

func reservationRequest(t testing.TB, rev position.PositionRevision, proposal, digest string) position.PositionReservationRequest {
	t.Helper()
	asOf := testAsOf(t)
	return position.PositionReservationRequest{
		Tenant: rev.Position.Tenant, Position: rev.Position, AsOf: asOf,
		ProposalRevisionID: proposal, ProposalDigest: digest, EffectiveDate: asOf.EffectiveOn,
		FTE: fte(t, "1.0000"), Heads: 1, DesiredJobCode: rev.JobCode,
		DesiredOrgUnit: rev.OrgUnit, DesiredLegalEntity: rev.LegalEntity,
		AuthorityDigest: canonicalbytes.Digest([]byte("position-authority")), IdempotencyKey: proposal,
		ExpiresAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC),
	}
}

// TestTodo_POSITION_003 proves exact proposal binding, vacancy and
// compatibility refusal, natural-key idempotency, fencing, evidence, and
// one-winner competition for a position effective date.
func TestTodo_POSITION_003(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := position.NewPositionReservationStore()
	request := reservationRequest(t, rev, "proposal-revision-1", canonicalbytes.Digest([]byte("proposal-1")))

	hold, err := store.Reserve(context.Background(), reader, request, now)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if hold.State != position.PositionReservationHeld || hold.Fence == 0 || hold.Request.ProposalDigest != request.ProposalDigest {
		t.Fatalf("hold = %#v", hold)
	}
	replay, err := store.Reserve(context.Background(), reader, request, now)
	if err != nil || replay.ID != hold.ID || replay.Fence != hold.Fence {
		t.Fatalf("idempotent replay = %#v, %v", replay, err)
	}

	evidence, ok := store.Evidence(hold.ID)
	if !ok || evidence.ProposalDigest != request.ProposalDigest || evidence.EffectiveDate != request.EffectiveDate || len(evidence.Events) != 1 {
		t.Fatalf("evidence = %#v, found=%v", evidence, ok)
	}
	if got := position.Explain(hold); len(got.Inputs) < 5 || got.ProposalDigest != request.ProposalDigest {
		t.Fatalf("explanation = %#v", got)
	}

	competing := reservationRequest(t, rev, "proposal-revision-2", canonicalbytes.Digest([]byte("proposal-2")))
	if _, err := store.Reserve(context.Background(), reader, competing, now); !errors.Is(err, position.ErrCompetingReservation) {
		t.Fatalf("competing reservation error = %v", err)
	}
	if _, err := store.ReleaseOnProposalRejected(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatalf("release on rejection: %v", err)
	}
	if _, err := store.ReleaseOnProposalRejected(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatalf("idempotent release on rejection: %v", err)
	}
	if released, ok := store.Get(hold.ID); !ok || released.State != position.PositionReservationReleased {
		t.Fatalf("released hold = %#v, found=%v", released, ok)
	}
}

func TestTodo_POSITION_003_Property(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := position.NewPositionReservationStore()
	request := reservationRequest(t, rev, "proposal-revision-1", canonicalbytes.Digest([]byte("proposal-1")))
	if _, err := store.Reserve(context.Background(), reader, request, now); err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.ProposalDigest = canonicalbytes.Digest([]byte("proposal-changed"))
	if _, err := store.Reserve(context.Background(), reader, changed, now); !errors.Is(err, position.ErrReservationConflict) {
		t.Fatalf("changed exact proposal was accepted: %v", err)
	}
}

func TestTodo_POSITION_003_Golden(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	request := reservationRequest(t, rev, "proposal-revision-1", canonicalbytes.Digest([]byte("proposal-1")))
	if len(request.Canonical()) == 0 {
		t.Fatal("valid reservation request has no canonical encoding")
	}
	first := position.Explain(position.PositionReservation{ID: "reservation-1", Request: request, State: position.PositionReservationHeld, Fence: 4})
	second := position.Explain(position.PositionReservation{ID: "reservation-1", Request: request, State: position.PositionReservationHeld, Fence: 4})
	if first.Reason != second.Reason || first.ProposalDigest != second.ProposalDigest || len(first.Inputs) != len(second.Inputs) {
		t.Fatalf("explanation is not stable: %#v vs %#v", first, second)
	}
}

func TestTodo_POSITION_003_Race(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	store := position.NewPositionReservationStore()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners int
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := reservationRequest(t, rev, "proposal-revision-"+string(rune('a'+i)), canonicalbytes.Digest([]byte{byte(i)}))
			if _, err := store.Reserve(context.Background(), reader, req, now); err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly one", winners)
	}
}

func BenchmarkTodo_POSITION_003(b *testing.B) {
	rev := validRevision(b, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < b.N; i++ {
		store := position.NewPositionReservationStore()
		req := reservationRequest(b, rev, "proposal-revision-1", canonicalbytes.Digest([]byte("proposal-1")))
		if _, err := store.Reserve(context.Background(), reader, req, now); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_POSITION_003_Mutation(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := reservationRequest(t, rev, "proposal-revision-1", canonicalbytes.Digest([]byte("proposal-1")))

	full := base
	full.Occupants = []position.Occupant{{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000010"), FTE: fte(t, "2.0000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true}}
	if _, err := position.NewPositionReservationStore().Reserve(context.Background(), reader, full, now); !errors.Is(err, position.ErrNoVacancy) {
		t.Fatalf("full position error = %v", err)
	}
	badCompatibility := base
	badCompatibility.DesiredJobCode = "HR-OTHER"
	if _, err := position.NewPositionReservationStore().Reserve(context.Background(), reader, badCompatibility, now); !errors.Is(err, position.ErrReservationConflict) {
		t.Fatalf("incompatible position error = %v", err)
	}
	expired := base
	expired.ExpiresAt = now
	if _, err := position.NewPositionReservationStore().Reserve(context.Background(), reader, expired, now); !errors.Is(err, position.ErrReservationExpired) {
		t.Fatalf("expired request error = %v", err)
	}
}
