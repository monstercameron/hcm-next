// Package effectivedate is the pure bitemporal selection engine: given a set
// of assertion coordinates, it answers "what was effective on this business
// date" and "what did we know at this moment" as two separate questions, and
// it never answers one with the other.
//
// Semantic owner: shared-engines. Phase: P1A.
//
// A coordinate here is only the temporal skeleton of an assertion - its
// identity, its effective interval, when it became known, when it was
// recorded, and which earlier assertion it corrects. The value, the field, the
// subject and the authority stay in the domain; the engine indexes by ID so
// the caller can map an answer back to its own record. That split is what
// keeps this package importable by anything without dragging a domain vocabulary
// along with it.
//
// The engine never deletes. A corrected assertion stays in the set and is
// reported as superseded, because the difference between "this was never true"
// and "we used to believe this" is the entire reason a bitemporal record
// exists.
package effectivedate

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	coordinateSchema  = "hcmnext.engines.effectivedate.Coordinate"
	effectivedateSchV = 1
)

// Selection errors. All are matchable with errors.Is.
var (
	// ErrCoordinateInvalid is returned for a coordinate missing its identity
	// or any of its temporal coordinates.
	ErrCoordinateInvalid = errors.New("effectivedate: coordinate is incomplete")
	// ErrDuplicateID is returned when two coordinates share an identity. An
	// ambiguous identity makes correction lineage unresolvable.
	ErrDuplicateID = errors.New("effectivedate: duplicate coordinate id")
	// ErrIntervalKind is returned for an effective interval that is not a
	// LOCAL_DATE interval. Effective dating is a business-calendar question;
	// an instant interval is a different contract, not a convertible one.
	ErrIntervalKind = errors.New("effectivedate: effective interval must be a LOCAL_DATE interval")
	// ErrUnknownSupersedes is returned when a coordinate corrects an assertion
	// that is not in the set. Silently dropping the link would present a
	// correction as an independent assertion.
	ErrUnknownSupersedes = errors.New("effectivedate: superseded coordinate is not in the set")
	// ErrSelfSupersedes is returned when a coordinate corrects itself.
	ErrSelfSupersedes = errors.New("effectivedate: coordinate supersedes itself")
	// ErrCutoffInvalid is returned for an unset knowledge cut-off or date.
	ErrCutoffInvalid = errors.New("effectivedate: query coordinate is unset")
)

// Version reports this engine's own contract version. It changes whenever the
// total order, the visibility rule or the in-force selection changes, because
// a stored selection must remain reproducible by the rules that produced it.
func Version() int { return effectivedateSchV }

// Coordinate is the temporal skeleton of one assertion.
//
// KnownAt and RecordedAt are both present and both mandatory. They answer
// different questions - when the authority could first have known this, and
// when the platform wrote it down - and a record that keeps only one of them
// cannot distinguish a backdated entry from a late one.
type Coordinate struct {
	// ID is the stable identity of the assertion this coordinate describes.
	ID string
	// Effective is the half-open business interval the assertion covers.
	Effective values.EffectiveInterval
	// KnownAt is when the assertion became available to its authority.
	KnownAt values.KnownAt
	// RecordedAt is when the assertion entered the record.
	RecordedAt values.RecordedAt
	// Supersedes is the ID of the assertion this one corrects, or empty when
	// this assertion is not a correction.
	Supersedes string
}

// Validate reports whether the coordinate is complete and internally
// consistent.
func (c Coordinate) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("%w: id is empty", ErrCoordinateInvalid)
	}
	if err := c.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrCoordinateInvalid, c.ID, err)
	}
	if c.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: %s is %s", ErrIntervalKind, c.ID, c.Effective.Kind())
	}
	if c.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: %s has no known-at", ErrCoordinateInvalid, c.ID)
	}
	if c.RecordedAt.Canonical() == nil {
		return fmt.Errorf("%w: %s has no recorded-at", ErrCoordinateInvalid, c.ID)
	}
	if c.Supersedes == c.ID {
		return fmt.Errorf("%w: %s", ErrSelfSupersedes, c.ID)
	}
	return values.ValidateKnowledgeOrder(c.KnownAt, c.RecordedAt, false)
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c Coordinate) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(coordinateSchema, effectivedateSchV).
		String("id", c.ID).
		Value("effective", c.Effective).
		Value("known_at", c.KnownAt).
		Value("recorded_at", c.RecordedAt).
		String("supersedes", c.Supersedes).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// startDate returns the inclusive start of the effective interval.
func (c Coordinate) startDate() values.LocalDate {
	d, _ := c.Effective.StartDate()
	return d
}

// IsCorrection reports whether this coordinate corrects an earlier assertion.
func (c Coordinate) IsCorrection() bool { return c.Supersedes != "" }

// Timeline is a validated, deterministically ordered set of coordinates for
// one subject and field. Build one with NewTimeline; the constructor is the
// only place the set is checked, so a Timeline in hand is always consistent.
type Timeline struct {
	ordered []Coordinate
	byID    map[string]Coordinate
	// supersededBy maps a corrected assertion id to the id that corrects it.
	supersededBy map[string]string
}

// NewTimeline validates the coordinates and orders them.
//
// The order is effective start, then known-at, then recorded-at, then ID. The
// trailing ID tiebreak is what makes the order total rather than merely
// mostly-defined: two assertions recorded in the same transaction would
// otherwise sort by input order, and a digest over the result would depend on
// how the caller happened to build its slice.
func NewTimeline(coords []Coordinate) (*Timeline, error) {
	byID := make(map[string]Coordinate, len(coords))
	for _, c := range coords {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if _, dup := byID[c.ID]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateID, c.ID)
		}
		byID[c.ID] = c
	}
	supersededBy := make(map[string]string, len(coords))
	for _, c := range coords {
		if !c.IsCorrection() {
			continue
		}
		if _, ok := byID[c.Supersedes]; !ok {
			return nil, fmt.Errorf("%w: %s corrects %s", ErrUnknownSupersedes, c.ID, c.Supersedes)
		}
		if prior, dup := supersededBy[c.Supersedes]; dup {
			return nil, fmt.Errorf("%w: %s is corrected by both %s and %s",
				ErrDuplicateID, c.Supersedes, prior, c.ID)
		}
		supersededBy[c.Supersedes] = c.ID
	}

	ordered := append([]Coordinate(nil), coords...)
	sort.SliceStable(ordered, func(i, j int) bool { return less(ordered[i], ordered[j]) })
	return &Timeline{ordered: ordered, byID: byID, supersededBy: supersededBy}, nil
}

// less is the total order over coordinates.
func less(a, b Coordinate) bool {
	if cmp := a.startDate().Compare(b.startDate()); cmp != 0 {
		return cmp < 0
	}
	if cmp := a.KnownAt.Instant().Compare(b.KnownAt.Instant()); cmp != 0 {
		return cmp < 0
	}
	if cmp := a.RecordedAt.Instant().Compare(b.RecordedAt.Instant()); cmp != 0 {
		return cmp < 0
	}
	return a.ID < b.ID
}

// Len returns the number of coordinates.
func (t *Timeline) Len() int { return len(t.ordered) }

// Ordered returns every coordinate in the total order. The returned slice is a
// copy; callers cannot reorder the timeline by mutating it.
func (t *Timeline) Ordered() []Coordinate {
	return append([]Coordinate(nil), t.ordered...)
}

// Lookup returns the coordinate with the given id.
func (t *Timeline) Lookup(id string) (Coordinate, bool) {
	c, ok := t.byID[id]
	return c, ok
}

// SupersededBy returns the id of the coordinate that corrects id, if any.
func (t *Timeline) SupersededBy(id string) (string, bool) {
	s, ok := t.supersededBy[id]
	return s, ok
}

// IsSuperseded reports whether some coordinate in the timeline corrects id.
func (t *Timeline) IsSuperseded(id string) bool {
	_, ok := t.supersededBy[id]
	return ok
}

// KnownAtOrBefore returns the coordinates the record could have shown at a
// knowledge cut-off, in timeline order.
//
// This is the "as known at" question on its own. A coordinate is visible when
// its known-at is at or before the cut-off; a correction recorded afterwards
// is invisible, which is exactly how a past belief is reproduced.
func (t *Timeline) KnownAtOrBefore(cut values.KnownAt) ([]Coordinate, error) {
	if cut.Canonical() == nil {
		return nil, fmt.Errorf("%w: known-at cut-off", ErrCutoffInvalid)
	}
	out := make([]Coordinate, 0, len(t.ordered))
	for _, c := range t.ordered {
		if c.KnownAt.Instant().After(cut.Instant()) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// CoveringDate returns the coordinates whose effective interval contains the
// business date, in timeline order.
//
// This is the "as of" question on its own. It says nothing about what was
// known: a future-dated assertion covering the date is returned here, and it
// is the knowledge cut-off that decides whether the caller may see it.
func (t *Timeline) CoveringDate(on values.LocalDate) ([]Coordinate, error) {
	if err := on.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCutoffInvalid, err)
	}
	out := make([]Coordinate, 0, len(t.ordered))
	for _, c := range t.ordered {
		covers, err := c.Effective.ContainsDate(on)
		if err != nil {
			return nil, fmt.Errorf("effectivedate: %s: %w", c.ID, err)
		}
		if covers {
			out = append(out, c)
		}
	}
	return out, nil
}

// InForce returns the single coordinate in force at a bitemporal position:
// effective on the business date and visible at the knowledge cut-off.
//
// When several assertions cover the same date, the one with the latest
// known-at wins, then the latest recorded-at, then the highest ID. That is a
// correction beating what it corrects, which is the whole point: the winner is
// decided by knowledge, not by which row was written first.
//
// The second result is false when nothing covers the position. That is an
// answer - the field was not asserted then - and never an error.
func (t *Timeline) InForce(on values.LocalDate, cut values.KnownAt) (Coordinate, bool, error) {
	visible, err := t.KnownAtOrBefore(cut)
	if err != nil {
		return Coordinate{}, false, err
	}
	if err := on.Validate(); err != nil {
		return Coordinate{}, false, fmt.Errorf("%w: %w", ErrCutoffInvalid, err)
	}
	var best Coordinate
	found := false
	for _, c := range visible {
		covers, err := c.Effective.ContainsDate(on)
		if err != nil {
			return Coordinate{}, false, fmt.Errorf("effectivedate: %s: %w", c.ID, err)
		}
		if !covers {
			continue
		}
		if !found || winsOver(c, best) {
			best, found = c, true
		}
	}
	return best, found, nil
}

// winsOver reports whether a beats b for the in-force position.
func winsOver(a, b Coordinate) bool {
	if cmp := a.KnownAt.Instant().Compare(b.KnownAt.Instant()); cmp != 0 {
		return cmp > 0
	}
	if cmp := a.RecordedAt.Instant().Compare(b.RecordedAt.Instant()); cmp != 0 {
		return cmp > 0
	}
	return a.ID > b.ID
}

// Explain returns a bounded, value-free description of the selection at one
// bitemporal position: how many coordinates the timeline holds, how many were
// visible at the cut-off, how many covered the date, which one won, and how
// many corrections were retained.
//
// It names identities and counts, never values: this engine has never seen a
// value, and an explanation that invented one would be describing something it
// did not compute.
func (t *Timeline) Explain(on values.LocalDate, cut values.KnownAt) ([]string, error) {
	visible, err := t.KnownAtOrBefore(cut)
	if err != nil {
		return nil, err
	}
	covering, err := t.CoveringDate(on)
	if err != nil {
		return nil, err
	}
	winner, found, err := t.InForce(on, cut)
	if err != nil {
		return nil, err
	}
	lines := []string{
		fmt.Sprintf("%d assertion(s) on the timeline", len(t.ordered)),
		fmt.Sprintf("%d known at or before %s", len(visible), cut),
		fmt.Sprintf("%d effective on %s", len(covering), on),
		fmt.Sprintf("%d correction(s) retained, none deleted", len(t.supersededBy)),
	}
	if found {
		lines = append(lines, "in force: "+winner.ID)
		if by, superseded := t.SupersededBy(winner.ID); superseded {
			lines = append(lines, "the winner is itself corrected by "+by)
		}
	} else {
		lines = append(lines, "no assertion was in force at this position")
	}
	return lines, nil
}

// Corrections returns the correction edges in timeline order, as pairs of the
// correcting id and the id it corrects.
func (t *Timeline) Corrections() [][2]string {
	out := make([][2]string, 0, len(t.supersededBy))
	for _, c := range t.ordered {
		if c.IsCorrection() {
			out = append(out, [2]string{c.ID, c.Supersedes})
		}
	}
	return out
}

// Canonical returns the canonical byte encoding of the whole ordered timeline,
// or nil when empty of encodable coordinates.
func (t *Timeline) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.engines.effectivedate.Timeline", effectivedateSchV).
		Count("coordinates", len(t.ordered))
	for _, c := range t.ordered {
		w.Value("coordinate", c)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
