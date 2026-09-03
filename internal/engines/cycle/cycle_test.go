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
