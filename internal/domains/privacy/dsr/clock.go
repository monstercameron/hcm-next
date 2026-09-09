package dsr

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// --- assurance floor -------------------------------------------------------

// assuranceFloors is the declared table [AssuranceFloor] reads: every
// [Kind] this package recognizes maps to the minimum [trust.Assurance]
// level identity evidence must reach before
// [DataSubjectRequest.Verify] will move a request of that kind to
// [VerificationVerified]. It is a lookup table, not a switch buried in
// Verify's control flow, so the PRIV-005 CONFORMANCE test can walk
// [AllKinds] and confirm every one of them has an entry.
//
// Erasure carries the highest floor this table declares
// ([trust.AssuranceHigh]): fulfilling an erasure request is irreversible,
// so it is the one kind this package never accepts at a lower assurance
// level. Every other kind requires at least [trust.AssuranceSubstantial] --
// none of these requests are ever actioned from an unauthenticated or
// low-assurance claim alone.
var assuranceFloors = map[Kind]trust.Assurance{
	KindAccess:        trust.AssuranceSubstantial,
	KindRectification: trust.AssuranceSubstantial,
	KindErasure:       trust.AssuranceHigh,
	KindRestriction:   trust.AssuranceSubstantial,
	KindPortability:   trust.AssuranceSubstantial,
	KindObjection:     trust.AssuranceSubstantial,
}

// AssuranceFloor returns the minimum identity-assurance level a Kind
// requires before a request of that kind can be verified. An undeclared
// kind returns [trust.AssuranceUnspecified], which
// [trust.Assurance.AtLeast] never satisfies -- an undeclared kind fails
// closed, it never falls through to "no floor required".
func AssuranceFloor(kind Kind) trust.Assurance {
	if floor, ok := assuranceFloors[kind]; ok {
		return floor
	}
	return trust.AssuranceUnspecified
}

// --- statutory clock --------------------------------------------------------

// ErrClockEntryInvalid is returned by [NewClockTable] when an entry is
// malformed or duplicates another entry's (jurisdiction, kind) key.
var ErrClockEntryInvalid = errors.New("dsr: clock table entry is invalid")

// ErrClockUndeclared is returned by [ClockTable.Deadline] when no entry --
// not even the zero-jurisdiction default -- covers the requested kind.
var ErrClockUndeclared = errors.New("dsr: no statutory clock entry covers this jurisdiction/kind")

// DayBasis is how a [ClockEntry]'s Days count is interpreted. Only calendar
// days are implemented today; a business-day calendar needs a holiday/work-
// week model this package does not own, so [NewClockTable] refuses
// [DayBasisBusiness] outright rather than silently computing calendar days
// under a business-day label.
type DayBasis uint8

const (
	DayBasisUnspecified DayBasis = iota
	DayBasisCalendar
	DayBasisBusiness
)

func (b DayBasis) valid() bool { return b == DayBasisCalendar }

// ClockEntry is one declared (jurisdiction, kind) -> response-deadline
// mapping. Jurisdiction's zero value ([legal.Jurisdiction]{}) is the
// fallback entry: [ClockTable.Deadline] uses it when no more specific
// jurisdiction entry matches. This package does not itself assert what any
// entry's Days value should be for a given kind of law -- that is
// configuration [DefaultClockTable] happens to seed with a working
// placeholder, not a citation this package interprets.
type ClockEntry struct {
	Jurisdiction legal.Jurisdiction
	Kind         Kind
	Days         int
	Basis        DayBasis
}

func (e ClockEntry) validate() error {
	if err := e.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrClockEntryInvalid, err)
	}
	if !e.Jurisdiction.IsZero() {
		if err := e.Jurisdiction.Validate(); err != nil {
			return fmt.Errorf("%w: jurisdiction %v", ErrClockEntryInvalid, err)
		}
	}
	if e.Days <= 0 {
		return fmt.Errorf("%w: kind %s has a non-positive day count %d", ErrClockEntryInvalid, e.Kind, e.Days)
	}
	if !e.Basis.valid() {
		return fmt.Errorf("%w: kind %s declares an unsupported day basis %d", ErrClockEntryInvalid, e.Kind, e.Basis)
	}
	return nil
}

func clockKey(j legal.Jurisdiction, k Kind) string {
	return j.String() + "|" + string(k)
}

// ClockTable is a validated, immutable set of [ClockEntry] rows.
// [ClockTable.Deadline] is the only way this package computes a statutory
// response deadline; nothing else in this package hard-codes a day count.
type ClockTable struct {
	byKey map[string]ClockEntry
}

// NewClockTable validates every entry and rejects a duplicate (jurisdiction,
// kind) key -- a table can never carry two conflicting deadlines for the
// same pair.
func NewClockTable(entries ...ClockEntry) (ClockTable, error) {
	byKey := make(map[string]ClockEntry, len(entries))
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return ClockTable{}, err
		}
		key := clockKey(e.Jurisdiction, e.Kind)
		if _, exists := byKey[key]; exists {
			return ClockTable{}, fmt.Errorf("%w: duplicate entry for jurisdiction %q kind %s", ErrClockEntryInvalid, e.Jurisdiction.String(), e.Kind)
		}
		byKey[key] = e
	}
	return ClockTable{byKey: byKey}, nil
}

// Deadline resolves the response deadline for kind in jurisdiction, given
// receivedAt. It tries, in order: the exact jurisdiction, the
// state-and-country (locality dropped), the country only, and finally the
// zero-value default -- returning the first match. [ErrClockUndeclared] is
// returned only when none of the four, including the default, declares an
// entry for kind.
func (t ClockTable) Deadline(jurisdiction legal.Jurisdiction, kind Kind, receivedAt values.Instant) (values.Instant, error) {
	if err := kind.Validate(); err != nil {
		return values.Instant{}, err
	}
	if !receivedAt.IsSet() {
		return values.Instant{}, fmt.Errorf("dsr: cannot compute a deadline without a received_at instant")
	}

	candidates := []legal.Jurisdiction{
		jurisdiction,
		{Country: jurisdiction.Country, State: jurisdiction.State},
		{Country: jurisdiction.Country},
		{},
	}
	seen := make(map[string]bool, len(candidates))
	for _, cand := range candidates {
		key := clockKey(cand, kind)
		if seen[key] {
			continue
		}
		seen[key] = true
		if entry, ok := t.byKey[key]; ok {
			sec, nsec := receivedAt.Unix()
			sec += int64(entry.Days) * 86400
			deadline, err := values.NewInstantFromUnix(sec, nsec)
			if err != nil {
				return values.Instant{}, err
			}
			return deadline, nil
		}
	}
	return values.Instant{}, fmt.Errorf("%w: jurisdiction %q kind %s", ErrClockUndeclared, jurisdiction.String(), kind)
}

// DefaultClockTable returns a validated table declaring a fallback entry
// (the zero-value jurisdiction) for every kind in [AllKinds], plus one
// worked jurisdiction-specific override (US-CA) to demonstrate that a more
// specific entry wins over the default. It panics if its own literal table
// fails validation, which would be a bug in this package, not a runtime
// condition a caller needs to handle -- see the doc_test.go conformance
// check that exercises this at test time instead of only at first call.
func DefaultClockTable() ClockTable {
	t, err := NewClockTable(
		ClockEntry{Kind: KindAccess, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindRectification, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindErasure, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindRestriction, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindPortability, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Kind: KindObjection, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"}, Kind: KindErasure, Days: 45, Basis: DayBasisCalendar},
		ClockEntry{Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"}, Kind: KindAccess, Days: 45, Basis: DayBasisCalendar},
	)
	if err != nil {
		panic(fmt.Sprintf("dsr: DefaultClockTable is internally invalid: %v", err))
	}
	return t
}
