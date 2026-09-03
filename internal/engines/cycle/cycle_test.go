package cycle

import (
	"errors"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"testing"
	"time"
)

func validCycle() BusinessCycle {
	return BusinessCycle{Type: CycleTypeRewards, Owner: "rewards", Scope: Scope{TenantID: "t1", OrganizationID: "org1"}, Timezone: "America/New_York", TZDBVersion: "2026a", Calendar: values.CalendarRef{Ref: "us-business", Version: "1"}, Periods: []Period{{ID: "p1", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}}, Phases: []Phase{{ID: "open", Name: "OPEN", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}}, Policies: Policies{Cutoff: "CLOSE_AT_END", Late: "REVIEW", Reopen: "APPROVAL", Restatement: "NEW_REVISION"}}
}

// TestTodo_CYCLE_001_Property verifies revision ownership of mutable slices.
func TestTodo_CYCLE_001_Property(t *testing.T) {
	c := validCycle()
	r, err := NewRevision(c, "rev-1", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	c.Periods[0].ID = "changed"
	if got := r.Cycle().Periods[0].ID; got == "changed" {
		t.Fatal("revision aliases source periods")
	}
	snapshot := r.Cycle()
	snapshot.Periods[0].ID = "changed-again"
	if got := r.Cycle().Periods[0].ID; got == "changed-again" {
		t.Fatal("Cycle aliases revision periods")
	}
}

// TestTodo_CYCLE_001 is the primary acceptance case for the registry row.
func TestTodo_CYCLE_001(t *testing.T) {
	c := validCycle()
	r, err := NewRevision(c, "rev-1", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.ValidAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || r.CanonicalDigest() == "" {
		t.Fatal("valid revision was not effective or digested")
	}
}

func TestCycleRequiresCompleteContract(t *testing.T) {
	c := validCycle()
	c.Type = CycleTypeUnspecified
	if !errors.Is(c.Validate(), ErrType) {
		t.Fatal("missing type accepted")
	}
	c = validCycle()
	c.Timezone = ""
	if !errors.Is(c.Validate(), ErrTimePolicy) {
		t.Fatal("missing timezone accepted")
	}
	c = validCycle()
	c.Periods = nil
	if !errors.Is(c.Validate(), ErrPeriods) {
		t.Fatal("missing periods accepted")
	}
	c = validCycle()
	c.Policies = Policies{}
	if !errors.Is(c.Validate(), ErrPolicies) {
		t.Fatal("missing policies accepted")
	}
}

func TestRevisionIsImmutableAndEffectiveDated(t *testing.T) {
	c := validCycle()
	r, err := NewRevision(c, "rev-1", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	c.Periods[0].ID = "mutated"
	got := r.Cycle()
	got.Periods[0].ID = "also-mutated"
	if r.Cycle().Periods[0].ID != "p1" {
		t.Fatal("revision shares mutable period storage")
	}
	if !r.ValidAt(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) || r.ValidAt(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("effective interval incorrect")
	}
	if r.CanonicalDigest() == "" {
		t.Fatal("missing digest")
	}
}

func TestTodo_CYCLE_001_Conformance(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*BusinessCycle)
		want   error
	}{
		{"type", func(c *BusinessCycle) { c.Type = CycleTypeUnspecified }, ErrType},
		{"owner", func(c *BusinessCycle) { c.Owner = "  " }, ErrOwner},
		{"scope", func(c *BusinessCycle) { c.Scope = Scope{} }, ErrScope},
		{"timezone", func(c *BusinessCycle) { c.Timezone = "  " }, ErrTimePolicy},
		{"calendar", func(c *BusinessCycle) { c.Calendar = values.CalendarRef{} }, ErrTimePolicy},
		{"periods", func(c *BusinessCycle) { c.Periods = nil }, ErrPeriods},
		{"phases", func(c *BusinessCycle) { c.Phases = nil }, ErrPhases},
		{"policies", func(c *BusinessCycle) { c.Policies.Cutoff = " " }, ErrPolicies},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCycle()
			tc.mutate(&c)
			if err := c.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate()=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_CYCLE_001_Mutation(t *testing.T) {
	c := validCycle()
	if _, err := NewRevision(c, "", "v1", time.Now(), time.Time{}); !errors.Is(err, ErrRevision) {
		t.Fatalf("missing id: %v", err)
	}
	if _, err := NewRevision(c, "r", "", time.Now(), time.Time{}); !errors.Is(err, ErrRevision) {
		t.Fatalf("missing version: %v", err)
	}
	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := NewRevision(c, "r", "v1", from, from); !errors.Is(err, ErrEffective) {
		t.Fatalf("equal effective bounds: %v", err)
	}
}
