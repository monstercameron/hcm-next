package reservation

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func reservationNow() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestTodo_RESERVE_001(t *testing.T) {
	now := reservationNow()
	s := NewStore()
	r := request(Digest([]byte("proposal")))
	hold, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if hold.Status != Held || hold.Fence == 0 {
		t.Fatalf("hold = %#v", hold)
	}
	if _, err = s.Consume(hold.ID, hold.Fence, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := s.Events(hold.ID); len(got) != 2 || got[0].To != Held || got[1].To != Consumed {
		t.Fatalf("lifecycle events = %#v", got)
	}
}

func TestTodo_RESERVE_001_Golden(t *testing.T) {
	now := reservationNow()
	s := NewStore()
	r := request(Digest([]byte("golden")))
	hold, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Release(hold.ID, hold.Fence, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(hold.ID)
	if !ok || got.Status != Released || got.Request.ProposalDigest != r.ProposalDigest || got.Request.AuthorityDigest != r.AuthorityDigest {
		t.Fatalf("released reservation = %#v, found=%v", got, ok)
	}
}

func TestTodo_RESERVE_001_Fault(t *testing.T) {
	now := reservationNow()
	cases := []struct {
		name   string
		mutate func(*Request)
		want   error
	}{
		{"expired", func(r *Request) { r.ExpiresAt = now }, ErrExpired},
		{"bad quantity", func(r *Request) { r.Quantity.Value = 0 }, ErrInvalidQuantity},
		{"bad interval", func(r *Request) { r.Interval.To = r.Interval.From }, ErrInvalidInterval},
		{"bad proposal digest", func(r *Request) { r.ProposalDigest = "not-a-digest" }, ErrInvalidDigest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore()
			r := request(Digest([]byte(tc.name)))
			tc.mutate(&r)
			if _, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_RESERVE_001_Mutation(t *testing.T) {
	now := reservationNow()
	s := NewStore()
	r := request(Digest([]byte("mutation")))
	hold, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Consume(hold.ID, hold.Fence, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Release(hold.ID, hold.Fence, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal mutation error = %v", err)
	}
	if len(s.Events(hold.ID)) != 2 {
		t.Fatal("invalid transition appended history")
	}
}

func TestTodo_RESERVE_001_Property(t *testing.T) {
	now := reservationNow()
	for _, delta := range []time.Duration{0, time.Nanosecond, time.Second} {
		s := NewStore()
		r := request(Digest([]byte(delta.String())))
		r.Interval = Interval{From: now, To: now.Add(time.Hour)}
		first, err := s.Acquire(r, Quantity{Value: 500, Scale: 3}, now)
		if err != nil {
			t.Fatal(err)
		}
		r.IdempotencyKey = "different"
		r.ProposalDigest = Digest([]byte("second"))
		r.Interval = Interval{From: now.Add(time.Hour).Add(-delta), To: now.Add(2 * time.Hour)}
		_, err = s.Acquire(r, Quantity{Value: 500, Scale: 3}, now)
		if delta == 0 && err != nil {
			t.Fatalf("half-open boundary should not overlap: %v", err)
		}
		if delta > 0 && !errors.Is(err, ErrCapacity) {
			t.Fatalf("overlap at %s error = %v", delta, err)
		}
		if first.ID == [16]byte{} {
			t.Fatal("acquire returned empty identity")
		}
	}
}

func TestTodo_RESERVE_001_Race(t *testing.T) {
	now := reservationNow()
	s := NewStore()
	const attempts = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	var holds []Reservation
	var firstErr error
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := request(Digest([]byte{byte(i + 1)}))
			r.Quantity.Value = 100
			r.IdempotencyKey = string(rune('a' + i))
			if hold, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now); err == nil {
				mu.Lock()
				holds = append(holds, hold)
				mu.Unlock()
			} else {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(holds) != 10 {
		t.Fatalf("successful holds = %d, want 10 (first error: %v)", len(holds), firstErr)
	}
	seen := make(map[uint64]bool)
	for _, hold := range holds {
		if seen[hold.Fence] {
			t.Fatalf("duplicate fence %d", hold.Fence)
		}
		seen[hold.Fence] = true
	}
}

func BenchmarkTodo_RESERVE_001(b *testing.B) {
	now := reservationNow()
	for i := 0; i < b.N; i++ {
		s := NewStore()
		r := request(Digest([]byte("benchmark")))
		r.IdempotencyKey = string(rune(i + 1))
		if _, err := s.Acquire(r, Quantity{Value: 1, Scale: 3}, now); err != nil {
			b.Fatal(err)
		}
	}
}
