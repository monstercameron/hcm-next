// Package positionpicker resolves PROMOUX-004's GREEN clause one: "an
// accessible server-filtered picker shows only authorized compatible
// positions with title, organization, manager, location, vacancy window
// and reservation state."
//
// It adds no position business rule of its own. Every candidate this
// package discloses is proven authorized, compatible and vacant by the same
// internal/domains/position ports (CheckCompatibility, CalculateCapacity)
// the promotion domain re-checks at submission time
// (internal/domains/promotion.evaluateTargetPositionSelection) -- so a
// position that would be refused on resubmission can never appear in the
// picker to begin with, and the same Authorize hook decides both, which is
// what makes the no-enumeration property hold: an unauthorized position is
// absent here for exactly the reason a guessed reference to it is refused
// there.
//
// # Directory
//
// This package has no notion of "every position in the tenant" -- there is
// no such list-all port on internal/domains/position, deliberately, for the
// same reason position.CalculateCapacity takes caller-supplied occupants
// rather than reading them itself: which positions exist and where they are
// organizationally is Position/Headcount directory territory, not something
// a per-reference compatibility port can certify. A caller supplies the
// candidate set and its display labels (title, manager, location) as
// [DirectoryEntry] values; this package's only job is to filter that set
// down to what the Position domain actually verifies and to attach the
// server-verified facts (revision reference, vacancy window, reservation
// state) a caller must never be trusted to supply itself.
package positionpicker

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrInvalidRequest is returned for a malformed request.
var ErrInvalidRequest = errors.New("positionpicker: invalid request")

// DirectoryEntry is one caller-supplied candidate and its display labels.
// The labels are trusted for presentation only; nothing here is trusted for
// authorization, compatibility, or vacancy -- every one of those is
// re-derived fresh from Position.
type DirectoryEntry struct {
	Position     values.EntityRef
	Title        string
	Organization string
	Manager      string
	Location     string
}

// Candidate is one authorized, compatible, vacant position the picker may
// disclose: REFACTOR's "browser carries only the selected position revision
// reference" starts here, since Reference is the one field a proposal binds
// to.
type Candidate struct {
	Position     values.EntityRef
	Reference    position.RevisionRef
	Title        string
	Organization string
	Manager      string
	Location     string
	// HasVacancyEnd and VacancyEnd report the earliest known date the
	// position sits vacant with no successor, when the Position domain
	// discloses one (position.CapacityResult.HasVacantAfter). A picker that
	// never surfaces this could let a proposer choose an effective date the
	// position will not actually be open for.
	HasVacancyEnd bool
	VacancyEnd    values.LocalDate
	// ReservationState is "AVAILABLE" for every candidate this package
	// returns: a position whose disclosed capacity is already fully
	// consumed (by an incumbent or another pending proposal, both supplied
	// through Occupants/Pending) is excluded rather than listed as
	// reserved, because a picker option a viewer cannot actually select is
	// not useful to disclose as a choice.
	ReservationState string
}

// ReservationAvailable is the only ReservationState a returned Candidate
// carries today. It is named as a constant, not inlined, because GREEN
// requires the state be disclosed explicitly rather than implied by a
// position merely appearing in the list.
const ReservationAvailable = "AVAILABLE"

// Request is what ResolveCandidates is asked: the proposal's desired
// placement (to filter for compatibility), the bitemporal coordinate, the
// occupancy inputs POSITION-002 needs, the authorization hook, and the
// caller-supplied directory to filter.
type Request struct {
	Tenant    values.TenantId
	AsOf      position.AsOf
	Directory []DirectoryEntry

	DesiredJobCode     string
	DesiredOrgUnit     string
	DesiredLegalEntity string

	Occupants []position.Occupant
	Pending   []position.PendingProposal

	// Authorize must be the identical hook the caller will later pass to
	// evaluateTargetPositionSelection for the same viewer and proposal: a
	// picker and a resubmission check that disagree on authorization would
	// either leak or wrongly refuse.
	Authorize position.Authorizer
}

// Validate reports whether the request is well formed on its own terms.
func (r Request) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrInvalidRequest, err)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return nil
}

// ResolveCandidates filters req.Directory down to the positions that are
// authorized, exist, are compatible with the desired placement, and have
// remaining vacancy at req.AsOf -- in that order, matching
// internal/domains/promotion.evaluateTargetPositionSelection's own ground
// order. A directory entry that fails any ground is silently absent from
// the result rather than reported as a refusal: the picker's contract is
// "here is what you may choose", not "here is why every candidate that
// didn't make it failed", which would itself be a disclosure channel for an
// unauthorized entry.
func ResolveCandidates(ctx context.Context, reader position.PositionFacts, req Request) ([]Candidate, error) {
	if reader == nil {
		return nil, fmt.Errorf("%w: no position facts reader", ErrInvalidRequest)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(req.Directory))
	for _, entry := range req.Directory {
		if err := entry.Position.Validate(); err != nil {
			return nil, fmt.Errorf("%w: directory entry position: %w", ErrInvalidRequest, err)
		}
		if entry.Position.Tenant != req.Tenant || entry.Position.Kind != position.KindPosition {
			return nil, fmt.Errorf("%w: directory entry position must be a position in the requested tenant", ErrInvalidRequest)
		}

		compat, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
			Tenant: req.Tenant, Position: entry.Position, AsOf: req.AsOf,
			DesiredJobCode: req.DesiredJobCode, DesiredOrgUnit: req.DesiredOrgUnit,
			DesiredLegalEntity: req.DesiredLegalEntity, Authorize: req.Authorize,
		})
		switch {
		case errors.Is(err, position.ErrUnauthorized):
			continue
		case err != nil:
			return nil, fmt.Errorf("positionpicker: check compatibility: %w", err)
		}
		if !compat.Exists || !compat.Compatible {
			continue
		}

		capacity, err := position.CalculateCapacity(ctx, reader, position.CapacityRequest{
			Tenant: req.Tenant, Position: entry.Position, AsOf: req.AsOf,
			Occupants: req.Occupants, Pending: req.Pending, Authorize: req.Authorize,
		})
		switch {
		case errors.Is(err, position.ErrUnauthorized):
			continue
		case err != nil:
			return nil, fmt.Errorf("positionpicker: calculate capacity: %w", err)
		}
		if !capacity.Exists {
			continue
		}
		// FindingVacantAfterDate is not a disqualifier: a position whose
		// coverage is known to end is exactly the fact HasVacancyEnd/
		// VacancyEnd below discloses, not a reason to hide the candidate.
		// Over-capacity and overlapping-exclusive-occupancy findings are:
		// the position domain has already found a structural problem with
		// the authoritative occupancy set, independent of the raw
		// available-heads/FTE arithmetic below.
		disqualified := false
		for _, f := range capacity.Findings {
			switch f.Code {
			case position.FindingOverCapacityHeads, position.FindingOverCapacityFTE, position.FindingOverlappingExclusiveOccupancy:
				disqualified = true
			}
		}
		if disqualified {
			continue
		}
		if capacity.AvailableHeads <= 0 || capacity.AvailableFTE.Sign() <= 0 {
			continue
		}

		ref, err := position.EncodeRevisionRef(entry.Position, compat.Revision)
		if err != nil {
			return nil, fmt.Errorf("positionpicker: encode revision reference: %w", err)
		}
		out = append(out, Candidate{
			Position: entry.Position, Reference: ref,
			Title: entry.Title, Organization: entry.Organization, Manager: entry.Manager, Location: entry.Location,
			HasVacancyEnd: capacity.HasVacantAfter, VacancyEnd: capacity.VacantAfter,
			ReservationState: ReservationAvailable,
		})
	}
	return out, nil
}
