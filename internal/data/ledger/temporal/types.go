package temporal

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// Querier is the minimal database capability a temporal read needs. It is
// internal/data/ledger.Querier itself, so whatever handle a caller already
// reads the ledger through satisfies this one unchanged.
type Querier = datalogger.Querier

// Decision is the authorization outcome this package reads under. It is
// internal/data/bitemporal.Decision itself rather than a parallel copy:
// the five delegated modes pass it straight through to that adapter, and
// RECONSTRUCT compiles the same allow/deny lists into its own statement, so
// one type describes both. See that type's own documentation for why
// "field" means schema_ref at this layer.
type Decision = bitemporal.Decision

// CorrectionKind labels why an assertion appears where it does in the
// timeline. It is internal/data/bitemporal.CorrectionKind, computed by that
// adapter's SQL from a self-join against the assertion a CORRECTION names.
type CorrectionKind = bitemporal.CorrectionKind

// Mode selects one of the six query shapes LEDGER-006 exposes.
type Mode string

const (
	// ModeCurrent resolves what is effective now, using everything known now.
	ModeCurrent Mode = "CURRENT"
	// ModeEffectiveAsOf resolves what was effective at Request.EffectiveAt.
	ModeEffectiveAsOf Mode = "EFFECTIVE_AS_OF"
	// ModeKnownAsOf resolves what the ledger had recorded by Request.KnownAt.
	ModeKnownAsOf Mode = "KNOWN_AS_OF"
	// ModeBetween returns every assertion effective in the half-open window
	// [EffectiveFrom, EffectiveTo).
	ModeBetween Mode = "BETWEEN"
	// ModeHistory returns every visible assertion with no effective bound.
	ModeHistory Mode = "HISTORY"
	// ModeReconstruct rebuilds one subject's whole state at one bitemporal
	// coordinate. It is this package's own mode; the other five delegate to
	// internal/data/bitemporal.
	ModeReconstruct Mode = "RECONSTRUCT"
)

// Valid reports whether m is one of the six declared modes.
func (m Mode) Valid() bool {
	switch m {
	case ModeCurrent, ModeEffectiveAsOf, ModeKnownAsOf, ModeBetween, ModeHistory, ModeReconstruct:
		return true
	default:
		return false
	}
}

// delegated reports whether m is one of DATA-005's five modes, which this
// package answers through internal/data/bitemporal rather than its own SQL.
func (m Mode) delegated() bool {
	return m.Valid() && m != ModeReconstruct
}

// TruthClass is what an assertion is permitted to be presented as. It is
// derived from the assertion class the ledger recorded, with a CORRECTION
// inheriting the class of the assertion it ultimately corrects. Resolution
// never crosses a truth class: see the package doc for why.
type TruthClass string

const (
	// TruthDomain is a DOMAIN_FACT: a fact this platform is the configured
	// authority for, and the only class that may be presented as domain truth.
	TruthDomain TruthClass = "DOMAIN_TRUTH"
	// TruthTransaction is a TRANSACTION_FACT: this platform's own record of
	// what it proposed, decided, attempted or executed. It is authoritative
	// about the transaction, never about the employee domain.
	TruthTransaction TruthClass = "TRANSACTION_RECORD"
	// TruthObserved is an EXTERNAL_OBSERVATION: what another configured
	// authority reported. Evidence, never domain truth.
	TruthObserved TruthClass = "EXTERNAL_OBSERVATION"
	// TruthClaimed is a CLAIM: an assertion not yet promoted to domain truth.
	TruthClaimed TruthClass = "CLAIM"
	// TruthUnresolved is a CORRECTION whose origin could not be proven inside
	// the visible, authorized set. It is never promoted; see the package doc.
	TruthUnresolved TruthClass = "UNRESOLVED"
)

// Promotable reports whether an assertion of this truth class may be
// resolved into [State] as a current value. Observations, claims and
// unresolved corrections are evidence: they are returned, but separately.
func (c TruthClass) Promotable() bool {
	return c == TruthDomain || c == TruthTransaction
}

// truthClassOf maps an assertion class that is not a CORRECTION onto its
// truth class. A CORRECTION has no truth class of its own and is resolved by
// [resolveTruthClass] instead.
func truthClassOf(class datalogger.AssertionClass) TruthClass {
	switch class {
	case datalogger.DomainFact:
		return TruthDomain
	case datalogger.TransactionFact:
		return TruthTransaction
	case datalogger.ExternalObservation:
		return TruthObserved
	case datalogger.Claim:
		return TruthClaimed
	default:
		return TruthUnresolved
	}
}

// Coordinate is the bitemporal point an answer was resolved at: the business
// instant it is effective for, and the knowledge horizon it was resolved
// under. Both are always populated on a returned [Result] or [State], even
// when the request left them to default, so an answer can be replayed
// exactly without knowing what "now" was when it ran.
type Coordinate struct {
	EffectiveAt time.Time
	KnownAt     time.Time
}

// AuthorityLabel is the authority assignment an assertion cites, resolved
// from authority_assignment rather than echoed back as a bare string.
//
// Present is false for an assertion that cites no authority at all - which
// is legitimate for the three classes that do not bear authority
// (internal/data/ledger.AssertionClass.RequiresAuthority). Resolved is false
// when the assertion cites an authority_ref that this tenant has no
// assignment row for; the answer still carries the ref so the gap is
// visible, and Kind/DomainScope stay empty rather than being guessed.
type AuthorityLabel struct {
	Present     bool
	Resolved    bool
	Ref         string
	Kind        string
	DomainScope string
	// EffectiveFrom/EffectiveTo are the assignment's half-open business
	// interval. EffectiveTo is nil for an open-ended assignment.
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	// CoversEffectiveAt reports whether the half-open interval actually
	// covers the assertion's own effective instant. The appender proves this
	// at append time; it is recomputed here because an assignment's interval
	// can be closed after the fact, and an answer must say so rather than
	// present a lapsed authority as a live one.
	CoversEffectiveAt bool
}

// Assertion is one authorized ledger event as an answer: the event's own
// identity and content, plus the three things a caller cannot derive for
// itself - the resolved truth class, the resolved authority label, and the
// exact source event the value came from.
type Assertion struct {
	Tenant uuid.UUID
	// Ref is the assertion's exact (stream, sequence) address.
	Ref datalogger.EventRef
	// SourceEventID is the immutable ledger event this answer is derived
	// from. Every value this package returns is traceable to exactly one.
	SourceEventID uuid.UUID

	SchemaRef      string
	AssertionClass datalogger.AssertionClass
	CorrectionKind CorrectionKind
	TruthClass     TruthClass
	Authority      AuthorityLabel
	SourceRef      string

	Payload         []byte
	ArtifactRef     string
	Digest          string
	DigestAlgorithm string

	OccurredAt  time.Time
	EffectiveAt time.Time
	RecordedAt  time.Time

	CorrelationID uuid.UUID
	CausationID   uuid.UUID
	// Corrects names the assertion this one corrects or supersedes, nil when
	// it corrects nothing.
	Corrects *datalogger.EventRef
	// Superseded reports that a later, visible, authorized assertion corrects
	// this one. A superseded assertion never wins resolution; it is still
	// returned by the listing modes, because history is the point.
	Superseded bool
}

// Request describes one temporal query.
type Request struct {
	Tenant uuid.UUID
	Mode   Mode

	// Subject restricts the query to one ledger stream. RECONSTRUCT requires
	// it: reconstruction is always of a named subject.
	Subject string
	// Field restricts the query to one schema_ref.
	Field string

	EffectiveAt   time.Time
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	KnownAt       time.Time

	Cursor string
	Limit  int
}

// Validate reports whether the request is well formed for its mode.
func (r Request) Validate() error {
	if r.Tenant == uuid.Nil {
		return ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if !r.Mode.Valid() {
		return ErrRequestInvalid{
			Field:  "Mode",
			Reason: fmt.Sprintf("%q is not one of CURRENT, EFFECTIVE_AS_OF, KNOWN_AS_OF, BETWEEN, HISTORY, RECONSTRUCT", string(r.Mode)),
		}
	}
	if r.Mode == ModeReconstruct {
		if r.Subject == "" {
			return ErrRequestInvalid{Field: "Subject", Reason: "is required for RECONSTRUCT; a reconstruction is always of a named subject"}
		}
		if r.Limit < 0 {
			return ErrRequestInvalid{Field: "Limit", Reason: "cannot be negative"}
		}
		return nil
	}
	return r.delegate().Validate()
}

// delegate projects the request onto internal/data/bitemporal's Request. It
// is only meaningful for the five delegated modes; RECONSTRUCT is answered
// by this package's own statement.
func (r Request) delegate() bitemporal.Request {
	return bitemporal.Request{
		Tenant:        r.Tenant,
		Mode:          bitemporal.Mode(r.Mode),
		Subject:       r.Subject,
		Field:         r.Field,
		EffectiveAt:   r.EffectiveAt,
		EffectiveFrom: r.EffectiveFrom,
		EffectiveTo:   r.EffectiveTo,
		KnownAt:       r.KnownAt,
		Cursor:        r.Cursor,
		Limit:         r.Limit,
	}
}

// Result is one page of answers from a listing or point-resolution mode.
type Result struct {
	Mode       Mode
	Coordinate Coordinate
	Assertions []Assertion
	// NextCursor resumes after the last assertion; empty when exhausted.
	NextCursor string
}

// State is one subject's reconstructed state at one bitemporal coordinate.
//
// Domain and Transaction each hold at most one winning assertion per
// schema_ref, sorted by schema_ref. Unpromoted holds every visible external
// observation, claim and unresolvable correction, unresolved and sorted
// chronologically: it is evidence about the subject, not the subject's
// state, and it is deliberately a separate field so that no caller can
// present it as one (see the package doc).
type State struct {
	Tenant     uuid.UUID
	Subject    string
	Coordinate Coordinate

	Domain      []Assertion
	Transaction []Assertion
	Unpromoted  []Assertion

	// Considered is how many visible, authorized assertions the fold read to
	// produce this state. It is evidence about the answer's own cost, and it
	// is bound into the state digest so two states built from different
	// amounts of history never digest alike.
	Considered int
}

// MaxReconstructEvents bounds how much history one reconstruction reads. A
// reconstruction that would exceed it fails with [ErrReconstructTooLarge]
// rather than allocating without limit; a subject with more history than
// this needs a checkpoint (LEDGER-010), not a bigger slice.
const MaxReconstructEvents = 50_000

// ErrRequestInvalid reports a malformed request.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

func (ErrRequestInvalid) Code() string { return "LEDGER_TEMPORAL_REQUEST_INVALID" }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", e.Code(), e.Field, e.Reason)
}

// ErrTenantMismatch reports a request whose tenant is not the tenant its
// authorization decision was evaluated for.
type ErrTenantMismatch struct {
	RequestTenant  uuid.UUID
	DecisionTenant uuid.UUID
}

func (ErrTenantMismatch) Code() string { return "LEDGER_TEMPORAL_TENANT_MISMATCH" }

func (e ErrTenantMismatch) Error() string {
	return fmt.Sprintf("%s: request tenant %s is not the decision's tenant %s",
		e.Code(), e.RequestTenant, e.DecisionTenant)
}

// ErrReconstructTooLarge reports that a subject has more visible history
// than [MaxReconstructEvents].
type ErrReconstructTooLarge struct {
	Subject string
	Limit   int
}

func (ErrReconstructTooLarge) Code() string { return "LEDGER_TEMPORAL_RECONSTRUCT_TOO_LARGE" }

func (e ErrReconstructTooLarge) Error() string {
	return fmt.Sprintf("%s: subject %s has more than %d visible assertions at this coordinate",
		e.Code(), e.Subject, e.Limit)
}

// ErrPlansDisagree reports that two query plans produced different answers
// for the same request and decision. It names both plans and both digests,
// because the point of running more than one plan is to locate the
// disagreement, not merely to notice it.
type ErrPlansDisagree struct {
	Subject string
	PlanA   string
	DigestA string
	PlanB   string
	DigestB string
}

func (ErrPlansDisagree) Code() string { return "LEDGER_TEMPORAL_PLANS_DISAGREE" }

func (e ErrPlansDisagree) Error() string {
	return fmt.Sprintf("%s: subject %s reconstructs to %s under plan %q and %s under plan %q",
		e.Code(), e.Subject, e.DigestA, e.PlanA, e.DigestB, e.PlanB)
}
