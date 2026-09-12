package promotion

// PROMOUX-004: "Replace free-text target positions with authorized vacancy
// selection and reservation evidence."
//
// RED: an arbitrary guessed identifier such as POS-ENG-MGR-101 is accepted
// without proving existence, vacancy, compatibility, effective-date
// capacity and reservation ownership. Before this file, checkPlacement
// (rules.go) validated TargetPlacement.JobCode and .Grade but never once
// read TargetPlacement.PositionID -- a promotion could name any string
// there and nothing in the domain would notice.
//
// GREEN, and REFACTOR's "validation and reservation remain owned by
// Position": this file adds no position business rule of its own. Every one
// of the five grounds below is a re-derived answer from
// internal/domains/position's own ports (CheckCompatibility,
// CalculateCapacity) or from the reservation-ownership port a caller
// supplies; this file only classifies which of the five grounds a Position
// finding belongs to and turns it into a promotion.Finding a reviewer can
// read.
//
// The check is not opt-in. TargetPlacement.PositionID is an existing string
// field (proto and every transport already carry it as free text), so a
// picker-issued position.RevisionRef rides it with no wire-shape change:
// only the meaning of a non-empty value tightens, from "an id someone
// typed" to "a token the server issued." evaluateTargetPositionSelection
// therefore treats ANY non-empty TargetPlacement.PositionID as a reference
// to decode -- explicit PositionSelection is for a caller that also has
// occupancy/proposal context to supply; a bare PositionID is the minimal
// shape every existing caller already has. Either way, a value that does
// not decode as a real, server-issued reference is refused on the
// existence ground: this is what turns a guessed identifier like
// POS-ENG-MGR-101 from accepted into rejected, universally, not per caller.
//
// Ground five (reservation ownership) is the one exception, and
// deliberately so: admitting a reservation is a write, and
// PreflightPromotion is also the read-only preview/simulate path several
// callers use before anything is proposed (internal/humanwork/workspace's
// /workspace/promotion page documents "writes nothing"). A caller that
// wants ground five enforced supplies a rich, explicit PositionSelection
// (with a real ProposalRef/IdempotencyKey and a wired Reservations port);
// a bare PositionID proves grounds one through four -- existence,
// compatibility, vacancy and that the disclosed revision is still current
// -- and is reported without ever attempting a write. See GREEN's
// "simulation explains advisory versus durable capacity": grounds one
// through four from a bare identifier are the advisory half, and only an
// explicit selection with a real reservation admitter reaches the durable
// half.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Finding codes for the target-position selection. Each names exactly one
// of the five grounds, so a reviewer (or a test) can tell which check
// refused a promotion without parsing prose.
const (
	// CodeTargetPositionNotFound is ground one, existence: the reference
	// does not decode, names a position outside the tenant, or the Position
	// domain reports no revision at the requested coordinate. It is also
	// what an authorization refusal reports (see [evaluateTargetPositionSelection]),
	// so a caller can never tell "does not exist" from "you may not see
	// it" apart -- the same no-enumeration property PROMOUX-001 established
	// for promotion availability.
	CodeTargetPositionNotFound = "promotion.target_position_not_found"
	// CodeTargetPositionIncompatible is ground three: the position exists
	// but its job, organization or legal entity does not match the
	// proposed placement.
	CodeTargetPositionIncompatible = "promotion.target_position_incompatible"
	// CodeTargetPositionNotEffective is ground four: the selected revision
	// is stale relative to what the position now shows, or the revision is
	// not effective at the proposed date.
	CodeTargetPositionNotEffective = "promotion.target_position_not_effective"
	// CodeTargetPositionAtCapacity is ground two: the position is closed,
	// frozen, over capacity, or otherwise has no remaining vacancy at the
	// proposed date.
	CodeTargetPositionAtCapacity = "promotion.target_position_at_capacity"
	// CodeTargetPositionReservationConflict is ground five: the position
	// and effective date passed every prior ground, but a different
	// proposal already holds the reservation for that exact slot.
	CodeTargetPositionReservationConflict = "promotion.target_position_reservation_conflict"
)

// PositionSelectionGround names one of the five independent grounds a
// target-position selection is checked against. It exists so a caller (or a
// test) can name exactly which ground refused a selection without parsing
// the finding's message.
type PositionSelectionGround string

// The five grounds RED names explicitly.
const (
	GroundExistence             PositionSelectionGround = "EXISTENCE"
	GroundVacancy               PositionSelectionGround = "VACANCY"
	GroundCompatibility         PositionSelectionGround = "COMPATIBILITY"
	GroundEffectiveDateCapacity PositionSelectionGround = "EFFECTIVE_DATE_CAPACITY"
	GroundReservationOwnership  PositionSelectionGround = "RESERVATION_OWNERSHIP"
)

// PositionSelection is the picker-disclosed input a promotion binds its
// target position to. Reference is the only thing the browser is trusted to
// carry (REFACTOR); everything else here is server-side context the caller
// (never the browser) supplies to re-derive the five grounds fresh.
type PositionSelection struct {
	// Reference is the opaque, picker-issued position+revision token. An
	// unset or malformed reference always fails at ground one (existence);
	// see [position.RevisionRef.Decode].
	Reference position.RevisionRef

	// AsOf is the bitemporal coordinate the selection is re-checked at: the
	// promotion's proposed effective date, and the caller's own known-at
	// horizon.
	AsOf position.AsOf

	// Occupants and Pending are the POSITION-002 capacity inputs: who
	// already occupies the position and which other proposals already hold
	// pending capacity against it. Precisely who is placed is Assignment
	// and Promotion territory, not something Position's own read port can
	// certify -- see internal/domains/position/capacity.go's package doc
	// for the identical argument.
	Occupants []position.Occupant
	Pending   []position.PendingProposal

	// ProposalRef and IdempotencyKey identify the proposal a reservation
	// (ground five) would protect. Both are required once a selection is
	// evaluated: a reservation with no stable owner reference could never
	// be told apart from a different proposal's on replay.
	ProposalRef    string
	IdempotencyKey string

	// Authorize mirrors position.CompatibilityRequest.Authorize: a nil hook
	// means no additional scope restriction beyond tenant isolation. It
	// must be the exact same hook a picker used to decide which candidates
	// to disclose, or an unauthorized position could pass here while never
	// having appeared in the picker (or vice versa).
	Authorize position.Authorizer
}

// PositionSlotAdmitRequest is what [PositionReservationAdmitter.AdmitPositionSlot]
// is asked to decide: does this proposal own the reservation for this
// position and effective date, right now.
type PositionSlotAdmitRequest struct {
	Tenant         values.TenantId
	Position       values.EntityRef
	EffectiveDate  values.LocalDate
	ProposalRef    string
	IdempotencyKey string
}

// Validate reports whether the admit request is well formed on its own
// terms. It does not decide admission; only [PositionReservationAdmitter]
// does that.
func (r PositionSlotAdmitRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if err := r.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %w", ErrRequestInvalid, err)
	}
	if r.Position.Tenant != r.Tenant || r.Position.Kind != position.KindPosition {
		return fmt.Errorf("%w: position must be a position in the requested tenant", ErrRequestInvalid)
	}
	if err := r.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %w", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(r.ProposalRef) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("%w: proposal ref and idempotency key are required", ErrRequestInvalid)
	}
	return nil
}

// PositionReservationAdmitter is ground five's boundary: reservation
// ownership. A promotion preflight never decides this itself -- it asks the
// port and reports whatever it says. The concrete database-backed adapter
// is internal/data/positionguard.Adapter, whose guarantee is a partial
// unique index the database evaluates as part of committing the admitting
// write, never a read the caller performs first (see
// migrations/00287_promoux004_position_slot_guard.sql).
type PositionReservationAdmitter interface {
	// AdmitPositionSlot reports whether req's proposal owns the reservation
	// for its position and effective date: true for a fresh admission or a
	// replay of the same idempotency key, false when a different proposal
	// already holds it. Only a genuine failure to decide (a database error,
	// an invalid request) is returned as an error.
	AdmitPositionSlot(ctx context.Context, req PositionSlotAdmitRequest) (bool, error)
}

// groundForPositionFinding classifies one position.Finding by which of the
// five grounds it belongs to. The mapping is exhaustive over every
// FindingCode internal/domains/position defines today: there is no default
// branch, so a position finding code this function does not explicitly name
// falls through to (\"\", false) and is refused by its caller
// ([mapPositionFindings]) rather than silently passed through unclassified.
func groundForPositionFinding(code position.FindingCode) (PositionSelectionGround, bool) {
	switch code {
	case position.FindingNotFound:
		return GroundExistence, true
	case position.FindingClosed, position.FindingFrozen,
		position.FindingOverCapacityHeads, position.FindingOverCapacityFTE,
		position.FindingOverlappingExclusiveOccupancy:
		return GroundVacancy, true
	case position.FindingJobMismatch, position.FindingOrgMismatch, position.FindingLegalEntityMismatch:
		return GroundCompatibility, true
	case position.FindingStaleRevision, position.FindingNotEffective, position.FindingVacantAfterDate:
		return GroundEffectiveDateCapacity, true
	}
	return "", false
}

// findingForGround renders one ground's refusal as a promotion.Finding. It
// is exhaustive over every declared [PositionSelectionGround]; a ground this
// switch does not name (there is no default branch) falls through to the
// trailing error rather than a silently permissive result.
func findingForGround(ground PositionSelectionGround) (Finding, error) {
	switch ground {
	case GroundExistence:
		return targetPositionNotFoundFinding(), nil
	case GroundVacancy:
		return Finding{
			Code: CodeTargetPositionAtCapacity, Severity: SeverityBlocking, Field: "target.position",
			Message: "the selected position is closed, frozen or has no remaining vacancy at the proposed date",
		}, nil
	case GroundCompatibility:
		return Finding{
			Code: CodeTargetPositionIncompatible, Severity: SeverityBlocking, Field: "target.position",
			Message: "the selected position does not match the proposed job or organization",
		}, nil
	case GroundEffectiveDateCapacity:
		return Finding{
			Code: CodeTargetPositionNotEffective, Severity: SeverityBlocking, Field: "target.position",
			Message: "the selected position revision is stale or is not effective at the proposed date; return to the picker and choose again",
		}, nil
	case GroundReservationOwnership:
		return Finding{
			Code: CodeTargetPositionReservationConflict, Severity: SeverityBlocking, Field: "target.position",
			Message: "another proposal already holds this position for the proposed effective date; choose a different position or date",
		}, nil
	}
	return Finding{}, fmt.Errorf("%w: unrecognized position selection ground %q", ErrRequestInvalid, ground)
}

// targetPositionNotFoundFinding is ground one's refusal, and also the
// refusal an authorization denial and a decode failure both collapse to: an
// unauthorized position must be indistinguishable from one that does not
// exist, or the message itself would tell a caller which guessed
// identifiers are real (PROMOUX-004's Security requirement, the same shape
// PROMOUX-001 closed for promotion availability).
func targetPositionNotFoundFinding() Finding {
	return Finding{
		Code: CodeTargetPositionNotFound, Severity: SeverityBlocking, Field: "target.position",
		Message: "the selected position could not be found",
	}
}

// mapPositionFindings classifies a Position domain finding set into
// promotion findings, one per distinct ground reached (a position result
// naming two findings for the same ground -- e.g. both over-capacity-heads
// and over-capacity-fte -- reports that ground once). It fails closed on an
// unrecognized position finding code rather than silently dropping it.
func mapPositionFindings(pf []position.Finding) ([]Finding, error) {
	var out []Finding
	seen := make(map[PositionSelectionGround]bool, len(pf))
	for _, f := range pf {
		ground, ok := groundForPositionFinding(f.Code)
		if !ok {
			return nil, fmt.Errorf("%w: unrecognized position finding code %q", ErrRequestInvalid, f.Code)
		}
		if seen[ground] {
			continue
		}
		seen[ground] = true
		finding, err := findingForGround(ground)
		if err != nil {
			return nil, err
		}
		out = append(out, finding)
	}
	return out, nil
}

// evaluateTargetPositionSelection is the whole RED-closing composition: it
// re-derives every ground it evaluates from the Position domain's own ports
// (never trusting anything the caller asserted about the position beyond
// the opaque reference) and returns the findings a refusal produces, or
// none when every ground it evaluates is satisfied.
//
// It runs for both shapes a caller can present, and the check is not
// opt-in for either: a non-empty TargetPlacement.PositionID alone is
// enough to trigger grounds one through four (see the package doc), and an
// explicit TargetPositionSelection additionally reaches ground five.
// Neither shape is trusted at face value; only what the position and
// reservation ports actually answer decides the outcome. Only a genuine
// contract failure -- an explicit selection with no reservation admitter
// wired, or a Position/reservation port that itself failed -- is returned
// as an error. Every business refusal, including a bare identifier this
// cell has no configured reader to check, is a Finding.
func evaluateTargetPositionSelection(ctx context.Context, req PreflightRequest) ([]Finding, error) {
	sel := req.TargetPositionSelection
	explicit := sel != nil
	if !explicit {
		raw := strings.TrimSpace(req.Target.PositionID)
		if raw == "" {
			return nil, nil
		}
		asOf, asOfErr := autoPositionAsOf(req)
		if asOfErr != nil {
			// The caller's own declared dates cannot even form a bitemporal
			// coordinate to ask Position at: a contract failure, not a
			// business refusal, because it means EffectiveDate or
			// EvaluationDate is itself malformed.
			return nil, fmt.Errorf("%w: target position as-of: %w", ErrRequestInvalid, asOfErr)
		}
		sel = &PositionSelection{Reference: position.RevisionRef(raw), AsOf: asOf}
	}

	pos, rev, err := sel.Reference.Decode()
	if err != nil {
		// A malformed or guessed reference -- including the zero value, and
		// including every free-text identifier no picker ever issued, such
		// as RED's own POS-ENG-MGR-101 -- fails closed at ground one,
		// exactly like a well-formed reference naming a position that does
		// not exist: neither one ever reaches a Position domain read.
		return []Finding{targetPositionNotFoundFinding()}, nil
	}
	if pos.Tenant != req.Tenant {
		return []Finding{targetPositionNotFoundFinding()}, nil
	}
	if req.PositionReader == nil {
		if explicit {
			// An explicit selection is a caller fully opting into this
			// mechanism (it also carries occupancy and proposal context a
			// bare identifier does not); a cell that wires one but not a
			// reader to check it against is misconfigured, not merely
			// unproven, and that is a contract failure to surface loudly.
			return nil, fmt.Errorf("%w: no position reader is configured for a selected target position", ErrRequestInvalid)
		}
		// A bare identifier with no reader configured on this cell can
		// never be proven, and an unprovable claim is refused exactly like
		// one this cell actively looked up and could not find -- never
		// silently passed through as if no position had been named at all.
		return []Finding{targetPositionNotFoundFinding()}, nil
	}

	compat, err := position.CheckCompatibility(ctx, req.PositionReader, position.CompatibilityRequest{
		Tenant: req.Tenant, Position: pos, AsOf: sel.AsOf,
		DesiredJobCode: req.Target.JobCode, DesiredOrgUnit: req.Target.OrgUnit,
		Authorize: sel.Authorize,
	})
	switch {
	case errors.Is(err, position.ErrUnauthorized):
		return []Finding{targetPositionNotFoundFinding()}, nil
	case err != nil:
		return nil, fmt.Errorf("promotion: check target position compatibility: %w", err)
	}
	if !compat.Exists {
		return []Finding{targetPositionNotFoundFinding()}, nil
	}
	if !compat.Compatible {
		findings, mapErr := mapPositionFindings(compat.Findings)
		if mapErr != nil {
			return nil, mapErr
		}
		if len(findings) > 0 {
			return findings, nil
		}
	}

	// Ground four, the exact-revision half: REFACTOR's "selection binds the
	// immutable position revision" means the browser's reference is not a
	// floor (position.CompatibilityRequest.MinRevision's "at least this
	// revision" semantics) but an exact binding -- the position must be
	// unchanged, in either direction, since the picker disclosed it. Equal
	// is used rather than CompareInStream deliberately: a real position
	// revision is minted as an opaque, content-addressed token
	// (values.NewOpaqueRevision), and CompareInStream refuses to order two
	// opaque tokens at all (ErrRevisionNotOrdered) -- exact equality is not
	// only what REFACTOR asks for, it is the only comparison an opaque
	// revision supports.
	if !compat.Revision.Equal(rev) {
		finding, ferr := findingForGround(GroundEffectiveDateCapacity)
		if ferr != nil {
			return nil, ferr
		}
		return []Finding{finding}, nil
	}

	capacity, err := position.CalculateCapacity(ctx, req.PositionReader, position.CapacityRequest{
		Tenant: req.Tenant, Position: pos, AsOf: sel.AsOf,
		Occupants: sel.Occupants, Pending: sel.Pending, Authorize: sel.Authorize,
	})
	switch {
	case errors.Is(err, position.ErrUnauthorized):
		return []Finding{targetPositionNotFoundFinding()}, nil
	case err != nil:
		return nil, fmt.Errorf("promotion: calculate target position capacity: %w", err)
	}
	if !capacity.Exists {
		return []Finding{targetPositionNotFoundFinding()}, nil
	}
	if len(capacity.Findings) > 0 {
		findings, mapErr := mapPositionFindings(capacity.Findings)
		if mapErr != nil {
			return nil, mapErr
		}
		if len(findings) > 0 {
			return findings, nil
		}
	}
	if capacity.AvailableHeads <= 0 || capacity.AvailableFTE.Sign() <= 0 {
		finding, ferr := findingForGround(GroundVacancy)
		if ferr != nil {
			return nil, ferr
		}
		return []Finding{finding}, nil
	}

	if !explicit {
		// Ground five is a write (an admission), and a bare identifier
		// carries no proposal identity to own a reservation under -- see
		// the package doc's "advisory versus durable capacity". Grounds
		// one through four have all passed: this is a proven, authorized,
		// compatible, vacant, current position, reported without ever
		// attempting to reserve it.
		return nil, nil
	}
	if req.Reservations == nil {
		return nil, fmt.Errorf("%w: no reservation admitter is configured for a selected target position", ErrRequestInvalid)
	}
	admitReq := PositionSlotAdmitRequest{
		Tenant: req.Tenant, Position: pos, EffectiveDate: sel.AsOf.EffectiveOn,
		ProposalRef: sel.ProposalRef, IdempotencyKey: sel.IdempotencyKey,
	}
	if err := admitReq.Validate(); err != nil {
		return nil, err
	}
	admitted, err := req.Reservations.AdmitPositionSlot(ctx, admitReq)
	if err != nil {
		return nil, fmt.Errorf("promotion: admit target position reservation: %w", err)
	}
	if !admitted {
		finding, ferr := findingForGround(GroundReservationOwnership)
		if ferr != nil {
			return nil, ferr
		}
		return []Finding{finding}, nil
	}
	return nil, nil
}

// autoPositionAsOf builds the bitemporal coordinate a bare TargetPlacement.
// PositionID is checked at, from facts PreflightRequest already declares:
// EffectiveOn is the promotion's own proposed effective date, and KnownAt is
// derived from EvaluationDate at midnight UTC. The promotion domain never
// reads a wall clock (see PreflightRequest.EvaluationDate's own doc: it is
// "today" as the caller declares it, precisely so a preflight stays a pure
// function of its declared inputs); this coordinate is built the same way.
func autoPositionAsOf(req PreflightRequest) (position.AsOf, error) {
	if err := req.EffectiveDate.Validate(); err != nil {
		return position.AsOf{}, fmt.Errorf("effective date: %w", err)
	}
	if err := req.EvaluationDate.Validate(); err != nil {
		return position.AsOf{}, fmt.Errorf("evaluation date: %w", err)
	}
	at := time.Date(int(req.EvaluationDate.Year()), req.EvaluationDate.Month(), int(req.EvaluationDate.Day()), 0, 0, 0, 0, time.UTC)
	knownAt, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		return position.AsOf{}, fmt.Errorf("known at: %w", err)
	}
	return position.AsOf{EffectiveOn: req.EffectiveDate, KnownAt: knownAt}, nil
}
