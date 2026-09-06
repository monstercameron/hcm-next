package position

// POSITION-002 calculates capacity and vacancy over PositionFacts: given the
// governed revision CheckCompatibility already knows how to read, plus the
// caller-supplied occupants (incumbents, effective-dated) and pending
// proposals (not-yet-committed placements that reserve capacity ahead of
// commit), CalculateCapacity returns exact fixed-decimal consumed, reserved
// and available heads/FTE and typed findings for over-capacity and
// vacancy-after-date.
//
// Occupants and pending proposals are caller-supplied rather than read
// through a second port for the same reason CheckCompatibility takes
// DesiredJobCode etc. directly: who is placed against a position is
// Assignment/Promotion territory (PEOPLE-003, PROMO-*), not something the
// Position domain's own port can certify. What CalculateCapacity refuses to
// trust the caller for is the position's own governed capacity policy, which
// it always re-reads fresh through PositionFacts - exactly like
// CheckCompatibility never accepts a caller-supplied revision.
//
// REFACTOR: this package never infers budget authority from position
// capacity. A position can be fully available while its promotion is refused
// for lack of COMPENSATION_POOL budget (BUDGET-002), and vice versa; the two
// are deliberately independent typed calculations.

import (
	"context"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const capacityResultSchema = "hcmnext.domains.position.CapacityResult"

// KindProposal is the entity kind used by pending capacity reservations.
const (
	KindWorker   values.Kind = "worker"
	KindProposal values.Kind = "proposal"
)

// Occupant is one worker's placement against a position over an
// effective-dated interval: POSITION-002's headcount and FTE consumption
// unit. Exclusive marks a placement that claims a whole head for its
// interval; a non-exclusive occupant consumes only FTE (a shared or partial
// placement), never a head, and can never trip the overlapping-exclusive
// finding.
type Occupant struct {
	Worker    values.EntityRef
	FTE       values.Decimal
	Effective values.EffectiveInterval
	Exclusive bool
}

// Validate reports whether the occupant is well formed on its own terms. It
// does not know the position's declared FTE scale; CalculateCapacity checks
// that once it has read the revision.
func (o Occupant) Validate() error {
	if err := o.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: occupant worker: %w", ErrInvalidRequest, err)
	}
	if err := o.FTE.Validate(); err != nil {
		return fmt.Errorf("%w: occupant fte: %w", ErrInvalidRequest, err)
	}
	if o.FTE.Sign() < 0 {
		return fmt.Errorf("%w: occupant fte is negative", ErrInvalidRequest)
	}
	if o.Effective.Validate() != nil {
		return fmt.Errorf("%w: occupant effective interval", ErrInvalidRequest)
	}
	if o.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: occupant effective interval must be LOCAL_DATE", ErrInvalidRequest)
	}
	return nil
}

// PendingProposal is a not-yet-committed placement that reserves position
// capacity ahead of commit - the position-capacity half of what POSITION-003
// will later fence as an exclusive hold. It carries no budget meaning of its
// own.
type PendingProposal struct {
	Proposal  values.EntityRef
	FTE       values.Decimal
	Effective values.EffectiveInterval
	Exclusive bool
}

// Validate reports whether the pending proposal is well formed on its own terms.
func (p PendingProposal) Validate() error {
	if err := p.Proposal.Validate(); err != nil {
		return fmt.Errorf("%w: pending proposal ref: %w", ErrInvalidRequest, err)
	}
	if err := p.FTE.Validate(); err != nil {
		return fmt.Errorf("%w: pending proposal fte: %w", ErrInvalidRequest, err)
	}
	if p.FTE.Sign() < 0 {
		return fmt.Errorf("%w: pending proposal fte is negative", ErrInvalidRequest)
	}
	if p.Effective.Validate() != nil {
		return fmt.Errorf("%w: pending proposal effective interval", ErrInvalidRequest)
	}
	if p.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: pending proposal effective interval must be LOCAL_DATE", ErrInvalidRequest)
	}
	return nil
}

// CapacityRequest is what CalculateCapacity is asked: the position and
// bitemporal coordinate to re-read fresh through PositionFacts, plus the
// occupants and pending proposals to evaluate against whatever capacity
// policy that fresh read returns.
type CapacityRequest struct {
	Tenant   values.TenantId
	Position values.EntityRef
	AsOf     AsOf

	Occupants []Occupant
	Pending   []PendingProposal

	// Authorize mirrors CompatibilityRequest.Authorize: a nil hook means no
	// additional scope restriction beyond tenant isolation; a hook that
	// returns false fails closed with ErrUnauthorized and discloses nothing.
	Authorize Authorizer
}

// Validate reports whether the request is well formed on its own terms.
func (r CapacityRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrInvalidRequest, err)
	}
	if err := r.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %w", ErrInvalidRequest, err)
	}
	if r.Position.Tenant != r.Tenant {
		return fmt.Errorf("%w: position %s is outside tenant %s", ErrInvalidRequest, r.Position, r.Tenant)
	}
	if r.Position.Kind != KindPosition {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRequest, r.Position.Kind, KindPosition)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	for i, o := range r.Occupants {
		if err := o.Validate(); err != nil {
			return fmt.Errorf("occupant %d: %w", i, err)
		}
	}
	for i, p := range r.Pending {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("pending proposal %d: %w", i, err)
		}
	}
	return nil
}

// CapacityResult is the exact fixed-decimal capacity and vacancy snapshot
// CalculateCapacity computed against one position's freshly read revision.
type CapacityResult struct {
	Position values.EntityRef
	Exists   bool

	Revision values.RevisionToken
	AsOf     AsOf

	CapacityFTE     values.Decimal
	CapacityHeads   int64
	OverfillAllowed bool

	ConsumedFTE    values.Decimal
	ConsumedHeads  int64
	ReservedFTE    values.Decimal
	ReservedHeads  int64
	AvailableFTE   values.Decimal
	AvailableHeads int64

	// VacantAfter and HasVacantAfter report the earliest date, among the
	// occupants and pending proposals covering AsOf, that nothing else is
	// known to cover - the first date the position would sit vacant with no
	// successor lined up. HasVacantAfter is false when no such date exists
	// (nothing covers AsOf, everything covering AsOf is open-ended, or every
	// determinate end is covered by a successor).
	VacantAfter    values.LocalDate
	HasVacantAfter bool

	Findings []Finding
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (res CapacityResult) Canonical() []byte {
	w := canonicalbytes.New(capacityResultSchema, positionSchemaVer).
		Value("position", res.Position).
		Bool("exists", res.Exists)
	if res.Exists {
		w.Value("revision", res.Revision).
			String("as_of.effective_on", res.AsOf.EffectiveOn.String()).
			Value("as_of.known_at", res.AsOf.KnownAt).
			Field("capacity_fte", res.CapacityFTE.Canonical()).
			Count("capacity_heads", int(res.CapacityHeads)).
			Bool("overfill_allowed", res.OverfillAllowed).
			Field("consumed_fte", res.ConsumedFTE.Canonical()).
			Count("consumed_heads", int(res.ConsumedHeads)).
			Field("reserved_fte", res.ReservedFTE.Canonical()).
			Count("reserved_heads", int(res.ReservedHeads)).
			Field("available_fte", res.AvailableFTE.Canonical()).
			Count("available_heads", int(res.AvailableHeads)).
			Bool("has_vacant_after", res.HasVacantAfter)
		if res.HasVacantAfter {
			w.Value("vacant_after", res.VacantAfter)
		}
	}
	w.Count("findings", len(res.Findings))
	for _, f := range res.Findings {
		w.Field("finding", f.Canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// coveringInterval is one occupant's or pending proposal's effective interval
// addressed by a stable key, used for pairwise overlap and vacancy-chain
// checks that do not care which kind of covering entry they are looking at.
type coveringInterval struct {
	key       string
	interval  values.EffectiveInterval
	exclusive bool
}

// CalculateCapacity is the POSITION-002 entry point: an authorized,
// as-of/known-at capacity and vacancy snapshot computed from a freshly read
// position revision plus caller-supplied occupants and pending proposals.
//
// A position the caller may not see under Authorize is refused outright
// (ErrUnauthorized), mirroring CheckCompatibility. A position that does not
// exist reports Exists=false with a POSITION_NOT_FOUND finding rather than a
// zero capacity, because zero and "unknown" are different answers.
func CalculateCapacity(ctx context.Context, reader PositionFacts, req CapacityRequest) (CapacityResult, error) {
	if reader == nil {
		return CapacityResult{}, fmt.Errorf("%w: no position facts reader", ErrInvalidRequest)
	}
	if err := req.Validate(); err != nil {
		return CapacityResult{}, err
	}

	rev, exists, err := reader.PositionRevisionAt(ctx, PositionQuery{
		Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
	})
	if err != nil {
		return CapacityResult{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
	}
	if !exists {
		return CapacityResult{
			Position: req.Position, Exists: false, AsOf: req.AsOf,
			Findings: []Finding{{Code: FindingNotFound, Detail: "no revision at the requested coordinate"}},
		}, nil
	}
	if err := rev.Validate(); err != nil {
		return CapacityResult{}, err
	}
	if rev.Position != req.Position {
		return CapacityResult{}, fmt.Errorf("%w: asked %s, answered %s", ErrSubjectMismatch, req.Position, rev.Position)
	}
	if req.Authorize != nil && !req.Authorize(rev) {
		return CapacityResult{}, ErrUnauthorized
	}

	scale := rev.Capacity.CapacityFTE.Scale()
	mode := rev.Capacity.CapacityFTE.Rounding()
	zero, err := values.NewDecimal("0", scale, mode)
	if err != nil {
		return CapacityResult{}, err
	}

	var findings []Finding
	var allCovering, activeNow, exclusiveOccupants []coveringInterval

	consumedFTE := zero
	consumedHeads := int64(0)
	for i, o := range req.Occupants {
		if o.Worker.Tenant != req.Tenant || o.Worker.Kind != KindWorker {
			return CapacityResult{}, fmt.Errorf("%w: occupant %d worker must be a worker in the requested tenant", ErrInvalidRequest, i)
		}
		if o.FTE.Scale() != scale {
			return CapacityResult{}, fmt.Errorf("%w: occupant %d fte scale %d does not match capacity scale %d",
				ErrInvalidRequest, i, o.FTE.Scale(), scale)
		}
		c := coveringInterval{key: fmt.Sprintf("occupant:%d:%s", i, o.Worker), interval: o.Effective, exclusive: o.Exclusive}
		allCovering = append(allCovering, c)
		if o.Exclusive {
			exclusiveOccupants = append(exclusiveOccupants, c)
		}
		active, cerr := o.Effective.ContainsDate(req.AsOf.EffectiveOn)
		if cerr != nil {
			return CapacityResult{}, fmt.Errorf("%w: occupant %d: %w", ErrInvalidRequest, i, cerr)
		}
		if !active {
			continue
		}
		activeNow = append(activeNow, c)
		consumedFTE, err = consumedFTE.Add(o.FTE)
		if err != nil {
			return CapacityResult{}, err
		}
		if o.Exclusive {
			consumedHeads++
		}
	}

	reservedFTE := zero
	reservedHeads := int64(0)
	for i, p := range req.Pending {
		if p.Proposal.Tenant != req.Tenant || p.Proposal.Kind != KindProposal {
			return CapacityResult{}, fmt.Errorf("%w: pending proposal %d must be a proposal in the requested tenant", ErrInvalidRequest, i)
		}
		if p.FTE.Scale() != scale {
			return CapacityResult{}, fmt.Errorf("%w: pending proposal %d fte scale %d does not match capacity scale %d",
				ErrInvalidRequest, i, p.FTE.Scale(), scale)
		}
		c := coveringInterval{key: fmt.Sprintf("pending:%d:%s", i, p.Proposal), interval: p.Effective, exclusive: p.Exclusive}
		allCovering = append(allCovering, c)
		active, cerr := p.Effective.ContainsDate(req.AsOf.EffectiveOn)
		if cerr != nil {
			return CapacityResult{}, fmt.Errorf("%w: pending proposal %d: %w", ErrInvalidRequest, i, cerr)
		}
		if !active {
			continue
		}
		activeNow = append(activeNow, c)
		reservedFTE, err = reservedFTE.Add(p.FTE)
		if err != nil {
			return CapacityResult{}, err
		}
		if p.Exclusive {
			reservedHeads++
		}
	}

	// Overlapping incumbent exclusive occupancy is a structural conflict in
	// the authoritative occupancy set. A pending proposal is deliberately not
	// part of this finding: it reserves a head and is accounted for by the
	// reserved totals, while competing holds are handled by POSITION-003.
	sort.Slice(exclusiveOccupants, func(i, j int) bool { return exclusiveOccupants[i].key < exclusiveOccupants[j].key })
	for i := 0; i < len(exclusiveOccupants); i++ {
		for j := i + 1; j < len(exclusiveOccupants); j++ {
			overlap, operr := exclusiveOccupants[i].interval.Overlaps(exclusiveOccupants[j].interval)
			if operr != nil {
				return CapacityResult{}, fmt.Errorf("%w: %w", ErrInvalidRequest, operr)
			}
			if overlap {
				findings = append(findings, Finding{
					Code:   FindingOverlappingExclusiveOccupancy,
					Detail: fmt.Sprintf("%s and %s exclusively claim a head over an overlapping interval", exclusiveOccupants[i].key, exclusiveOccupants[j].key),
				})
			}
		}
	}

	totalHeads := consumedHeads + reservedHeads
	totalFTE, err := consumedFTE.Add(reservedFTE)
	if err != nil {
		return CapacityResult{}, err
	}
	availableHeads := rev.Capacity.CapacityHeads - totalHeads
	availableFTE, err := rev.Capacity.CapacityFTE.Sub(totalFTE)
	if err != nil {
		return CapacityResult{}, err
	}

	if !rev.Capacity.OverfillAllowed {
		if totalHeads > rev.Capacity.CapacityHeads {
			findings = append(findings, Finding{
				Code:   FindingOverCapacityHeads,
				Detail: fmt.Sprintf("consumed+reserved heads %d exceed capacity %d", totalHeads, rev.Capacity.CapacityHeads),
			})
		}
		if totalFTE.Cmp(rev.Capacity.CapacityFTE) > 0 {
			findings = append(findings, Finding{
				Code:   FindingOverCapacityFTE,
				Detail: fmt.Sprintf("consumed+reserved fte %s exceeds capacity %s", totalFTE, rev.Capacity.CapacityFTE),
			})
		}
	}

	vacantAfter, hasVacantAfter, verr := findVacancyAfter(activeNow, allCovering)
	if verr != nil {
		return CapacityResult{}, verr
	}
	if hasVacantAfter {
		findings = append(findings, Finding{
			Code:   FindingVacantAfterDate,
			Detail: fmt.Sprintf("position becomes vacant after %s with no successor covering it", vacantAfter),
		})
	}

	return CapacityResult{
		Position: rev.Position, Exists: true, Revision: rev.Revision, AsOf: req.AsOf,
		CapacityFTE: rev.Capacity.CapacityFTE, CapacityHeads: rev.Capacity.CapacityHeads, OverfillAllowed: rev.Capacity.OverfillAllowed,
		ConsumedFTE: consumedFTE, ConsumedHeads: consumedHeads,
		ReservedFTE: reservedFTE, ReservedHeads: reservedHeads,
		AvailableFTE: availableFTE, AvailableHeads: availableHeads,
		VacantAfter: vacantAfter, HasVacantAfter: hasVacantAfter,
		Findings: findings,
	}, nil
}

// findVacancyAfter returns the earliest determinate end date among
// activeNow entries that no other entry in all covers, and whether one was
// found. Because every interval is half-open, an entry starting exactly at
// another's end date does cover that date - so a contiguous hand-off never
// trips this, and only a genuine gap (or nothing at all) does.
func findVacancyAfter(activeNow, all []coveringInterval) (values.LocalDate, bool, error) {
	var earliest values.LocalDate
	found := false
	for _, cur := range activeNow {
		end, hasEnd := cur.interval.EndDate()
		if !hasEnd {
			continue
		}
		covered := false
		for _, other := range all {
			if other.key == cur.key {
				continue
			}
			ok, err := other.interval.ContainsDate(end)
			if err != nil {
				return values.LocalDate{}, false, err
			}
			if ok {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if !found || end.Compare(earliest) < 0 {
			earliest = end
			found = true
		}
	}
	return earliest, found, nil
}
