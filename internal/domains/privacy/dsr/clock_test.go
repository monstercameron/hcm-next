package dsr

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestAssuranceFloor_UndeclaredKindIsUnspecified(t *testing.T) {
	if f := AssuranceFloor(Kind("BOGUS")); f != trust.AssuranceUnspecified {
		t.Fatalf("AssuranceFloor(bogus) = %s, want %s", f, trust.AssuranceUnspecified)
	}
}

func TestNewClockTable_RejectsBadEntries(t *testing.T) {
	cases := []ClockEntry{
		{Kind: Kind("BOGUS"), Days: 30, Basis: DayBasisCalendar},
		{Kind: KindAccess, Days: 0, Basis: DayBasisCalendar},
		{Kind: KindAccess, Days: -1, Basis: DayBasisCalendar},
		{Kind: KindAccess, Days: 30, Basis: DayBasisBusiness}, // unsupported basis
		{Kind: KindAccess, Days: 30, Basis: DayBasisUnspecified},
		{Jurisdiction: legal.Jurisdiction{Country: "us"}, Kind: KindAccess, Days: 30, Basis: DayBasisCalendar}, // lowercase country invalid
	}
	for i, c := range cases {
		if _, err := NewClockTable(c); err == nil {
			t.Errorf("case %d: NewClockTable(%+v) succeeded, want ErrClockEntryInvalid", i, c)
		}
	}
}

func TestNewClockTable_RejectsDuplicateJurisdictionKindPair(t *testing.T) {
	_, err := NewClockTable(
		ClockEntry{Kind: KindAccess, Days: 30, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindAccess, Days: 45, Basis: DayBasisCalendar},
	)
	if err == nil {
		t.Fatal("NewClockTable with two entries for the same (jurisdiction, kind) succeeded")
	}
}

func TestClockTable_DeadlineRequiresReceivedAt(t *testing.T) {
	if _, err := DefaultClockTable().Deadline(legal.Jurisdiction{}, KindAccess, values.Instant{}); err == nil {
		t.Fatal("Deadline with an unset received_at succeeded")
	}
}

func TestDefaultClockTable_IsInternallyValid(t *testing.T) {
	// DefaultClockTable panics on internal inconsistency; simply calling it
	// is the smoke test that its own literal table still validates.
	_ = DefaultClockTable()
}
