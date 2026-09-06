package trust

import (
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// GrantKind records why a delegation exists. Acting roles and vacation
// coverage are not separate authority mechanisms with their own rules: they
// are [DelegationGrant] values carrying a kind, evaluated by
// [EvaluateDelegation] exactly like any other grant. The kind is attribution,
// never a widening: no kind grants an authority a direct grant would not.
type GrantKind string

// The recognized grant kinds. The empty kind normalizes to
// [GrantKindDirect] so a grant written before kinds existed keeps its
// meaning.
const (
	GrantKindDirect     GrantKind = "direct"
	GrantKindActingRole GrantKind = "acting_role"
	GrantKindCoverage   GrantKind = "coverage"
)

// normalize maps the zero kind onto the direct kind.
func (k GrantKind) normalize() GrantKind {
	if k == "" {
		return GrantKindDirect
	}
	return k
}

// valid reports whether k is a recognized kind once normalized.
func (k GrantKind) valid() bool {
	switch k.normalize() {
	case GrantKindDirect, GrantKindActingRole, GrantKindCoverage:
		return true
	default:
		return false
	}
}

// ErrInvalidAssignment is returned when an [AuthorityAssignment] cannot be
// lowered onto a bounded delegation grant.
var ErrInvalidAssignment = fmt.Errorf("%w: authority assignment", ErrInvalidDelegation)

// AuthorityAssignment is the business-facing shape of a temporary authority
// transfer: "X acts as the regional HR manager until Friday" or "Y covers Z's
// approvals while Z is on leave". It exists so that both concepts lower onto
// the single bounded-delegation primitive instead of growing a second, weaker
// authority path.
//
// Every field is server-resolved. The assignment can only narrow: it is
// evaluated against the delegator's and delegate's own authority snapshots,
// so naming a capability the absent principal does not hold grants nothing.
type AuthorityAssignment struct {
	// AssignmentID is the durable identifier of the assignment record. It
	// becomes the grant identifier, which keeps the chain attributable back
	// to the HR record that created it.
	AssignmentID string
	// Kind is [GrantKindActingRole] or [GrantKindCoverage].
	Kind GrantKind
	// RootID and ParentGrantID position the assignment in a chain. Both are
	// optional for a first-level assignment.
	RootID        string
	ParentGrantID string
	// From is the principal whose authority is lent - the role holder, or
	// the principal who is away. To is the principal who acts.
	From string
	To   string
	// Role is the acting role identifier. It is required for an acting-role
	// assignment and must be empty for coverage, which lends the absent
	// principal's own authority rather than a named role.
	Role string
	// Tenant and OrganizationScopeID bound the assignment. An assignment
	// never crosses either.
	Tenant              values.TenantId
	OrganizationScopeID string
	// Capabilities, Resources, Fields and Purposes are the requested bounds.
	// They are intersected with both parties' authority at evaluation time.
	Capabilities []string
	Resources    []string
	Fields       []string
	Purposes     []string
	// StartsAt and EndsAt bound the assignment in time.
	StartsAt time.Time
	EndsAt   time.Time
	// RequiredAssurance is the assurance both parties must currently hold.
	RequiredAssurance Assurance
	// AllowRedelegation and MaxDepth apply to acting roles only. Coverage is
	// never re-delegable: a stand-in cannot appoint their own stand-in.
	AllowRedelegation bool
	MaxDepth          uint8
	// RevocationEpoch is the epoch the assignment was issued under.
	RevocationEpoch uint64
	// Revoked marks an assignment withdrawn ahead of its end date.
	Revoked bool
}

// Grant lowers the assignment onto the bounded-delegation primitive. The
// returned grant is validated by [ValidateDelegation], so an assignment that
// could not be evaluated is refused at construction rather than at use.
func (a AuthorityAssignment) Grant() (DelegationGrant, error) {
	kind := a.Kind.normalize()
	switch kind {
	case GrantKindActingRole:
		if !printableASCII(a.Role, 1, 200) {
			return DelegationGrant{}, fmt.Errorf("%w: acting role requires a role identifier", ErrInvalidAssignment)
		}
	case GrantKindCoverage:
		if a.Role != "" {
			return DelegationGrant{}, fmt.Errorf("%w: coverage lends the absent principal's own authority, not a named role", ErrInvalidAssignment)
		}
	default:
		return DelegationGrant{}, fmt.Errorf("%w: kind %q is not an assignment kind", ErrInvalidAssignment, a.Kind)
	}

	allowRedelegation, maxDepth := a.AllowRedelegation, a.MaxDepth
	if kind == GrantKindCoverage {
		// A stand-in cannot appoint their own stand-in. This is a property
		// of the kind, not a policy toggle a caller can flip.
		allowRedelegation, maxDepth = false, 0
	}

	g := DelegationGrant{
		GrantID:             a.AssignmentID,
		RootID:              a.RootID,
		ParentGrantID:       a.ParentGrantID,
		Kind:                kind,
		Delegator:           a.From,
		Delegate:            a.To,
		Tenant:              a.Tenant,
		OrganizationScopeID: a.OrganizationScopeID,
		Capabilities:        a.Capabilities,
		Resources:           a.Resources,
		Fields:              a.Fields,
		Purposes:            a.Purposes,
		NotBefore:           a.StartsAt,
		ExpiresAt:           a.EndsAt,
		RequiredAssurance:   a.RequiredAssurance,
		AllowRedelegation:   allowRedelegation,
		MaxDepth:            maxDepth,
		Revoked:             a.Revoked,
		RevocationEpoch:     a.RevocationEpoch,
	}
	if g.RootID == "" {
		g.RootID = g.GrantID
	}
	if err := ValidateDelegation(g); err != nil {
		return DelegationGrant{}, err
	}
	return g, nil
}

// Evaluate lowers the assignment and evaluates it against the trusted
// authority snapshots of both parties. It is the only supported way to turn
// an acting role or a coverage assignment into effective authority, and it
// runs the same intersection, tenancy, validity, revocation and chain checks
// as any other delegation.
func (a AuthorityAssignment) Evaluate(delegator, delegate AuthorityScope, at time.Time, currentRevocationEpoch uint64) (EffectiveAuthority, error) {
	g, err := a.Grant()
	if err != nil {
		return EffectiveAuthority{}, err
	}
	return EvaluateDelegation(DelegationRequest{
		Grant:                  g,
		Delegator:              delegator,
		Delegate:               delegate,
		EvaluatedAt:            at,
		CurrentRevocationEpoch: currentRevocationEpoch,
	})
}
