package legalhold_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func instant(y int, m time.Month, d int) values.Instant {
	return values.NewInstant(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func hold() legalhold.Hold {
	return legalhold.Hold{ID: "hold-1", Tenant: "tenant-1", Compartment: "case-7", Scope: legalhold.Scope{Tenant: "tenant-1", Compartment: "case-7", RecordRefs: []string{"record-1"}}, Reason: "regulatory inquiry", Authority: "legal/authority/v1", CreatedAt: instant(2026, time.January, 2)}
}

// TestTodo_MODEL_027 proves blocked disposition is evidence-bearing and that
// scope does not accidentally freeze unrelated records.
func TestTodo_MODEL_027(t *testing.T) {
	s := legalhold.NewStore()
	if err := s.Create(hold()); err != nil {
		t.Fatal(err)
	}
	blocked := s.DecideDisposition(legalhold.Record{Tenant: "tenant-1", Compartment: "case-7", Ref: "record-1"}, "evidence-1", instant(2026, time.January, 3))
	if blocked.Allowed || blocked.Code != "HOLD_BLOCKED" || blocked.Evidence.Code != "HOLD_BLOCKED" {
		t.Fatalf("decision = %+v", blocked)
	}
	allowed := s.DecideDisposition(legalhold.Record{Tenant: "tenant-1", Compartment: "case-7", Ref: "record-2"}, "evidence-2", instant(2026, time.January, 3))
	if !allowed.Allowed || allowed.Code != "DISPOSITION_ALLOWED" {
		t.Fatalf("unrelated record = %+v", allowed)
	}
	if got := len(s.Evidence()); got != 2 {
		t.Fatalf("evidence count = %d, want create + blocked", got)
	}
}

func TestTodo_MODEL_027_Property(t *testing.T) {
	s := legalhold.NewStore()
	h := hold()
	if err := s.Create(h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Holds("tenant-1", "case-7", false); !errors.Is(err, legalhold.ErrUnauthorized) {
		t.Fatalf("unauthorized query = %v", err)
	}
	if got, err := s.Holds("tenant-2", "case-7", true); err != nil || len(got) != 0 {
		t.Fatalf("cross-tenant query leaked = %#v, %v", got, err)
	}
	if err := s.Release(h.ID, instant(2026, time.February, 1)); err != nil {
		t.Fatal(err)
	}
	if d := s.DecideDisposition(legalhold.Record{Tenant: "tenant-1", Compartment: "case-7", Ref: "record-1"}, "evidence-3", instant(2026, time.February, 2)); !d.Allowed {
		t.Fatalf("released hold still blocks: %+v", d)
	}
}

func TestTodo_MODEL_027_Golden(t *testing.T) {
	s := legalhold.NewStore()
	h := hold()
	if err := s.Create(h); err != nil {
		t.Fatal(err)
	}
	got, err := s.Holds("tenant-1", "case-7", true)
	if err != nil || len(got) != 1 {
		t.Fatalf("holds = %#v, %v", got, err)
	}
	if got[0].ID != "hold-1" || !got[0].Active() {
		t.Fatalf("hold = %+v", got[0])
	}
}

func TestTodo_MODEL_027_Security(t *testing.T) {
	s := legalhold.NewStore()
	h := hold()
	if err := s.Create(h); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Holds("tenant-1", "other-compartment", true); err != nil || len(got) != 0 {
		t.Fatalf("compartment isolation = %#v, %v", got, err)
	}
}

// TestTodo_MODEL_027_Mutation exercises lifecycle mutations that must not
// create a second hold or accidentally reactivate a released one.
func TestTodo_MODEL_027_Mutation(t *testing.T) {
	s := legalhold.NewStore()
	h := hold()
	if err := s.Create(h); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(h); !errors.Is(err, legalhold.ErrInvalidHold) {
		t.Fatalf("duplicate create = %v", err)
	}
	if err := s.Release("missing", h.CreatedAt); !errors.Is(err, legalhold.ErrUnknownHold) {
		t.Fatalf("unknown release = %v", err)
	}
	if err := s.Release(h.ID, instant(2026, time.February, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(h.ID, instant(2026, time.March, 1)); err != nil {
		t.Fatalf("release should be idempotent: %v", err)
	}
	got, _ := s.Holds(h.Tenant, h.Compartment, true)
	if len(got) != 1 || !got[0].ReleasedAt.IsSet() {
		t.Fatalf("released hold = %#v", got)
	}
}

func TestTodo_MODEL_027_AncestorArtifact(t *testing.T) {
	s := legalhold.NewStore()
	if err := s.Create(hold()); err != nil {
		t.Fatal(err)
	}
	d := s.DecideDisposition(legalhold.Record{
		Tenant: "tenant-1", Compartment: "case-7", Ref: "artifact-1",
		Ancestors: []legalhold.Record{{Tenant: "tenant-1", Compartment: "case-7", Ref: "record-1"}},
	}, "artifact-evidence", instant(2026, time.January, 3))
	if d.Allowed || d.Code != "HOLD_BLOCKED" {
		t.Fatalf("ancestor artifact = %+v", d)
	}
}

func TestTodo_MODEL_027_Race(t *testing.T) {
	s := legalhold.NewStore()
	h := hold()
	if err := s.Create(h); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := legalhold.Record{Tenant: h.Tenant, Compartment: h.Compartment, Ref: fmt.Sprintf("record-%d", i%2)}
			s.DecideDisposition(r, fmt.Sprintf("evidence-%d", i), h.CreatedAt)
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Release(h.ID, instant(2026, time.February, 1)) }()
	wg.Wait()
}

func FuzzTodo_MODEL_027(f *testing.F) {
	f.Add("tenant-1", "case-7", "record-1", "reason")
	f.Fuzz(func(t *testing.T, tenant, compartment, ref, reason string) {
		s := legalhold.NewStore()
		h := legalhold.Hold{ID: "fuzz", Tenant: tenant, Compartment: compartment,
			Scope:  legalhold.Scope{Tenant: tenant, Compartment: compartment, RecordRefs: []string{ref}},
			Reason: reason, Authority: "authority", CreatedAt: instant(2026, time.January, 2)}
		err := s.Create(h)
		if err == nil {
			d := s.DecideDisposition(legalhold.Record{Tenant: tenant, Compartment: compartment, Ref: ref}, "fuzz-evidence", h.CreatedAt)
			if !d.Allowed && d.Code != "HOLD_BLOCKED" {
				t.Fatalf("unexpected decision: %+v", d)
			}
		}
	})
}
