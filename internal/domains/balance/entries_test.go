package balance

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func validEntry() BalanceEntry {
	return BalanceEntry{AccountID: "worker-1", DefinitionID: "pto", DefinitionVersion: "1.0.0", Unit: "hour", Currency: "USD", Subject: "worker", Period: string(PeriodCalendarYear), Dimensions: map[string]string{"worker_id": "worker-1", "program": "pto"}, Kind: Debit, Amount: values.MustDecimal("2.50", 2, values.RoundingExactRequired), EntryType: "USAGE", SourceTransactionID: "leave-start-1", IdempotencyKey: "leave-start-1"}
}

func TestTodo_BAL_002(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()
	e := validEntry()
	r, err := s.Post(PostRequest{Entry: e, ExpectedHead: 0}, d)
	if err != nil || r.Head != 1 || r.Digest == "" || len(s.Entries(e.AccountID)) != 1 {
		t.Fatalf("post receipt=%+v err=%v", r, err)
	}
	replay, err := s.Post(PostRequest{Entry: e, ExpectedHead: 1}, d)
	if err != nil || !replay.Replay || replay.Head != 1 || len(s.Entries(e.AccountID)) != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestTodo_BAL_002_Property(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()
	e := validEntry()
	if _, err := s.Post(PostRequest{Entry: e}, d); err != nil {
		t.Fatal(err)
	}
	e.Dimensions["worker_id"] = "changed"
	if s.Entries(e.AccountID)[0].Dimensions["worker_id"] != "worker-1" {
		t.Fatal("stored entry aliases caller dimensions")
	}
}

func TestTodo_BAL_002_Race(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e := validEntry(); _, _ = s.Post(PostRequest{Entry: e, ExpectedHead: 0}, d) }()
	}
	wg.Wait()
	if len(s.Entries("worker-1")) != 1 {
		t.Fatalf("entries=%d, want one", len(s.Entries("worker-1")))
	}
}

func TestTodo_BAL_002_Recovery(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()
	e := validEntry()
	if _, err := s.Post(PostRequest{Entry: e, ExpectedHead: 1}, d); !errors.Is(err, ErrStaleHead) {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.Post(PostRequest{Entry: e, ExpectedHead: 0}, d); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_BAL_002_Mutation(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()
	base := validEntry()
	cases := []struct {
		name   string
		mutate func(*BalanceEntry)
	}{
		{"kind", func(e *BalanceEntry) { e.Kind = "OTHER" }}, {"unit", func(e *BalanceEntry) { e.Unit = "day" }}, {"period", func(e *BalanceEntry) { e.Period = "PAY_PERIOD" }}, {"dimension", func(e *BalanceEntry) { delete(e.Dimensions, "program") }}, {"amount", func(e *BalanceEntry) { e.Amount = values.Decimal{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base.copy()
			tc.mutate(&e)
			if _, err := s.Post(PostRequest{Entry: e}, d); err == nil {
				t.Fatal("invalid entry accepted")
			}
		})
	}
}
