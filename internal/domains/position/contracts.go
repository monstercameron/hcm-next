// Package position owns POSITION-001: authorized reads of one position's
// current governed revision and the compatibility findings that read
// supports for Promotion and Compensation Change.
//
// Semantic owner: Position and Headcount domain. Phase: P1A.
//
// Position is a governed capacity resource, not a reference code: it has an
// effective-dated revision, a lifecycle, and a capacity policy, and none of
// those may be inferred by a caller. This package holds no persistence; the
// intent kernel wires a real PositionFacts reader to it, mirroring the split
// internal/domains/people already uses for WorkerFacts - a read model that
// could compute its own authorization or invent facts the reader never
// returned would be a read model that eventually gets both wrong.
package position

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Port and revision errors. All are matchable with errors.Is.
var (
	// ErrInvalidRequest is returned for a malformed query or request.
	ErrInvalidRequest = errors.New("position: invalid request")
	// ErrRevisionIncomplete is returned when a revision is missing its
	// effective interval, lifecycle, capacity policy, source authority or
	// provenance. An incomplete revision is refused rather than disclosed with
	// the gap left implicit.
	ErrRevisionIncomplete = errors.New("position: revision is missing effective interval, lifecycle, capacity policy, source authority or provenance")
	// ErrSubjectMismatch is returned when the reader answers about a different
	// position than the one that was asked about.
	ErrSubjectMismatch = errors.New("position: reader answered about a different position")
	// ErrReaderFailed wraps a failure from the PositionFacts port.
	ErrReaderFailed = errors.New("position: position facts reader failed")
	// ErrUnauthorized is returned when the caller-supplied Authorize hook
	// refuses the position outright. Unlike a compatibility finding, this is a
	// refusal to answer at all: no lifecycle, capacity or revision detail is
	// disclosed alongside it.
	ErrUnauthorized = errors.New("position: unauthorized scope")
)

// KindPosition is the entity kind of a position reference.
const KindPosition values.Kind = "position"

const (
	revisionSchema    = "hcmnext.domains.position.Revision"
	resultSchema      = "hcmnext.domains.position.CompatibilityResult"
	positionSchemaVer = 1
)

// Lifecycle is the position lifecycle state, per the Position and Headcount
// domain contract: DRAFT -> OPEN -> RESERVED? -> PARTIALLY_FILLED|FILLED,
// with VACANT/FROZEN/CLOSED reachable from the filled states.
type Lifecycle string

// Lifecycle states.
const (
	LifecycleUnspecified     Lifecycle = ""
	LifecycleDraft           Lifecycle = "DRAFT"
	LifecycleOpen            Lifecycle = "OPEN"
	LifecycleReserved        Lifecycle = "RESERVED"
	LifecyclePartiallyFilled Lifecycle = "PARTIALLY_FILLED"
	LifecycleFilled          Lifecycle = "FILLED"
	LifecycleVacant          Lifecycle = "VACANT"
	LifecycleFrozen          Lifecycle = "FROZEN"
	LifecycleClosed          Lifecycle = "CLOSED"
)

var lifecycleValid = map[Lifecycle]struct{}{
	LifecycleDraft: {}, LifecycleOpen: {}, LifecycleReserved: {},
	LifecyclePartiallyFilled: {}, LifecycleFilled: {}, LifecycleVacant: {},
	LifecycleFrozen: {}, LifecycleClosed: {},
}

// Valid reports whether l is a defined lifecycle state.
func (l Lifecycle) Valid() bool { _, ok := lifecycleValid[l]; return ok }

// String returns the wire token.
func (l Lifecycle) String() string { return string(l) }

// AcceptsPlacement reports whether a position in this lifecycle state may be
// the target of a new assignment at all. CLOSED and FROZEN never do; the
// remaining states may still be blocked by capacity, which this package does
// not evaluate (that is POSITION-002's job).
func (l Lifecycle) AcceptsPlacement() bool {
	switch l {
	case LifecycleOpen, LifecycleReserved, LifecyclePartiallyFilled, LifecycleVacant:
		return true
	default:
		return false
	}
}

// CapacityPolicy is the governed capacity this position revision declares.
// FTE is an exact fixed-decimal value; it is never a float.
type CapacityPolicy struct {
	CapacityFTE     values.Decimal
	CapacityHeads   int64
	OverfillAllowed bool
}

// Validate reports whether the capacity policy is well formed.
func (c CapacityPolicy) Validate() error {
	if err := c.CapacityFTE.Validate(); err != nil {
		return fmt.Errorf("%w: capacity fte: %w", ErrRevisionIncomplete, err)
	}
	if c.CapacityFTE.Sign() < 0 {
		return fmt.Errorf("%w: negative capacity fte", ErrRevisionIncomplete)
	}
	if c.CapacityHeads < 0 {
		return fmt.Errorf("%w: negative capacity heads", ErrRevisionIncomplete)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (c CapacityPolicy) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.position.CapacityPolicy", positionSchemaVer).
		Value("capacity_fte", c.CapacityFTE).
		Count("capacity_heads", int(c.CapacityHeads)).
		Bool("overfill_allowed", c.OverfillAllowed).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PositionRevision is the effective-dated, governed state of one position:
// what job, organization and legal entity it belongs to, its lifecycle,
// capacity policy, source revision, authority and provenance.
type PositionRevision struct {
	Position values.EntityRef

	Revision  values.RevisionToken
	Effective values.EffectiveInterval
	Lifecycle Lifecycle

	JobCode     string
	OrgUnit     string
	LegalEntity string

	Capacity CapacityPolicy

	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

// Validate reports whether the revision is complete enough to disclose.
func (r PositionRevision) Validate() error {
	if err := r.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %w", ErrInvalidRequest, err)
	}
	if r.Position.Kind != KindPosition {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRequest, r.Position.Kind, KindPosition)
	}
	if !r.Revision.IsSpecified() {
		return fmt.Errorf("%w: no revision", ErrRevisionIncomplete)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %w", ErrRevisionIncomplete, err)
	}
	if !r.Lifecycle.Valid() {
		return fmt.Errorf("%w: lifecycle %q", ErrRevisionIncomplete, r.Lifecycle)
	}
	if r.JobCode == "" || r.OrgUnit == "" || r.LegalEntity == "" {
		return fmt.Errorf("%w: job, org unit and legal entity are required", ErrRevisionIncomplete)
	}
	if err := r.Capacity.Validate(); err != nil {
		return err
	}
	if err := r.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrRevisionIncomplete, err)
	}
	if err := r.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrRevisionIncomplete, err)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r PositionRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(revisionSchema, positionSchemaVer).
		Value("position", r.Position).
		Value("revision", r.Revision).
		Value("effective", r.Effective).
		String("lifecycle", r.Lifecycle.String()).
		String("job_code", r.JobCode).
		String("org_unit", r.OrgUnit).
		String("legal_entity", r.LegalEntity).
		Field("capacity", r.Capacity.Canonical()).
		Value("authority", r.Authority).
		Value("provenance", r.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// AsOf is the bitemporal coordinate of a position question, mirroring
// internal/domains/people.AsOf: "what is true now" and "what did we know
// then" are different questions, and a compatibility read that answers only
// the first cannot detect a stale baseline.
type AsOf struct {
	EffectiveOn values.LocalDate
	KnownAt     values.KnownAt
}

// Validate reports whether both coordinates are set.
func (a AsOf) Validate() error {
	if err := a.EffectiveOn.Validate(); err != nil {
		return fmt.Errorf("position: as-of effective date: %w", err)
	}
	if a.KnownAt.Canonical() == nil {
		return fmt.Errorf("position: as-of known-at is unset")
	}
	return nil
}

// PositionQuery is what the read port is asked for.
type PositionQuery struct {
	Tenant   values.TenantId
	Position values.EntityRef
	AsOf     AsOf
}

// Validate reports whether the query is well formed.
func (q PositionQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrInvalidRequest, err)
	}
	if err := q.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %w", ErrInvalidRequest, err)
	}
	if q.Position.Tenant != q.Tenant {
		return fmt.Errorf("%w: position %s is outside tenant %s", ErrInvalidRequest, q.Position, q.Tenant)
	}
	if q.Position.Kind != KindPosition {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRequest, q.Position.Kind, KindPosition)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return nil
}

// PositionFacts is the read port the intent kernel wires to a real
// repository. There is deliberately no variant that accepts a revision from
// the caller: a compatibility finding whose inputs the caller supplied could
// not certify what the position record actually says.
type PositionFacts interface {
	// PositionRevisionAt returns the governed revision effective and known at
	// the query's bitemporal coordinate. It returns an error only for a read
	// failure; a position that does not exist is reported via the second
	// result being false, because non-existence is an answer, not a fault.
	PositionRevisionAt(ctx context.Context, q PositionQuery) (rev PositionRevision, exists bool, err error)
}
