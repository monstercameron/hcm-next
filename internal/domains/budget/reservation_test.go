package budget

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func reservationFixture(t *testing.T) (CompensationReservationRequest, CompensationBudgetAuthority, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	amount, err := values.NewDecimal("60.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	digest := canonicalbytes.Digest([]byte("authority"))
	r := CompensationReservationRequest{TenantID: "tenant-1", BudgetID: "pool-1", ProposalDigest: canonicalbytes.Digest([]byte("proposal")), Amount: amount, Currency: "USD", AuthorityDigest: digest, IdempotencyKey: "request-1", ExpiresAt: now.Add(time.Hour)}
	available, err := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return r, CompensationBudgetAuthority{BudgetID: "pool-1", Currency: "USD", Available: available, AuthorityDigest: digest}, now
}

func TestTodo_BUDGET_002(t *testing.T) {
	r, a, now := reservationFixture(t)
	s := NewReservationStore()
	hold, err := s.Reserve(r, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if hold.State != Held || hold.Fence == 0 {
		t.Fatalf("hold = %#v", hold)
	}
	replay, err := s.Reserve(r, a, now)
	if err != nil || replay.ID != hold.ID {
		t.Fatalf("idempotent replay = %#v, %v", replay, err)
	}
	evidence, ok := s.Evidence(hold.ID)
	if !ok || evidence.ProposalDigest != r.ProposalDigest || evidence.AuthorityDigest != r.AuthorityDigest || len(evidence.Events) != 1 {
		t.Fatalf("reservation evidence = %#v, found=%v", evidence, ok)
	}
	if _, err = s.Commit(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(s.Events(hold.ID)) != 2 {
		t.Fatalf("events = %d, want 2", len(s.Events(hold.ID)))
	}
}

func TestTodo_BUDGET_002_Race(t *testing.T) {
	r, a, now := reservationFixture(t)
	s := NewReservationStore()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var holds int
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rr := r
			rr.IdempotencyKey = string(rune('a' + i))
			rr.ProposalDigest = canonicalbytes.Digest([]byte{byte(i)})
			rr.Amount, _ = values.NewDecimal("20.00", 2, values.RoundingExactRequired)
			if _, err := s.Reserve(rr, a, now); err == nil {
				mu.Lock()
				holds++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if holds != 5 {
		t.Fatalf("holds = %d, want 5", holds)
	}
}

func TestTodo_BUDGET_002_Integration(t *testing.T) {
	r, a, now := reservationFixture(t)
	s := NewReservationStore()
	hold, err := s.Reserve(r, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Release(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Get(hold.ID); !ok || got.State != Released {
		t.Fatalf("released = %#v, found=%v", got, ok)
	}
}

func TestTodo_BUDGET_002_Fault(t *testing.T) {
	r, a, now := reservationFixture(t)
	cases := []struct {
		name   string
		mutate func(*CompensationReservationRequest, *CompensationBudgetAuthority)
		want   error
	}{
		{"expired", func(r *CompensationReservationRequest, _ *CompensationBudgetAuthority) { r.ExpiresAt = now }, ErrReservationExpired},
		{"stale currency", func(r *CompensationReservationRequest, _ *CompensationBudgetAuthority) { r.Currency = "EUR" }, ErrCurrencyMismatch},
		{"over capacity", func(r *CompensationReservationRequest, _ *CompensationBudgetAuthority) {
			r.Amount, _ = values.NewDecimal("101.00", 2, values.RoundingExactRequired)
		}, ErrInsufficientBudget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr, aa := r, a
			tc.mutate(&rr, &aa)
			if _, err := NewReservationStore().Reserve(rr, aa, now); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_BUDGET_002_Security(t *testing.T) {
	r, a, now := reservationFixture(t)
	s := NewReservationStore()
	hold, err := s.Reserve(r, a, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Release(hold.ID, hold.Fence+1, now); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale fence = %v", err)
	}
	if err = s.MarkAmbiguous(hold.ID, hold.Fence, now); !errors.Is(err, ErrExternalAmbiguous) {
		t.Fatalf("ambiguous = %v", err)
	}
	if _, err = s.Release(hold.ID, hold.Fence, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ambiguous transition = %v", err)
	}
	if _, err = s.Reconcile(hold.ID, hold.Fence, Released, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}
