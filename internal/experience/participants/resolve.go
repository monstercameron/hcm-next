// Package participants resolves the human experience context for one flow
// stage.  It is deliberately a consumer of trust and AuthZ decisions: labels
// such as "manager" or "delegate" never grant authority.
package participants

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

var (
	ErrInvalidStage = errors.New("participants: invalid stage")
	ErrDenied       = errors.New("participants: stage denied")
	ErrExpired      = errors.New("participants: representation or delegation expired")
	ErrRecused      = errors.New("participants: participant recused")
)

type Role string

const (
	RoleSelf            Role = "HUMAN_SELF"
	RoleManager         Role = "MANAGER"
	RoleHRSpecialist    Role = "HR_SPECIALIST"
	RoleReviewer        Role = "REVIEWER_OR_APPROVER"
	RoleDelegate        Role = "DELEGATE_OR_REPRESENTATIVE"
	RoleCaseParticipant Role = "CASE_PARTICIPANT"
)

type RepresentationKind string

const (
	RepresentationNone        RepresentationKind = "NONE"
	RepresentationOnBehalfOf  RepresentationKind = "ON_BEHALF_OF"
	RepresentationInterpreter RepresentationKind = "INTERPRETER"
	RepresentationAssisted    RepresentationKind = "ASSISTED"
	RepresentationSupportView RepresentationKind = "SUPPORT_VIEW"
)

// Representation records communication assistance separately from identity.
// Subject is never replaced by Representative.
type Representation struct {
	Kind           RepresentationKind
	Representative string
	Subject        values.EntityRef
	EvidenceRef    string
	ExpiresAt      values.Instant
}

// Delegation is an experience-layer projection of a previously governed grant.
// AllowedActions is an upper bound and is never authority by itself.
type Delegation struct {
	GrantRef       string
	FromPrincipal  string
	ToPrincipal    string
	AllowedActions []string
	ExpiresAt      values.Instant
}

type Stage struct {
	StageID        string
	Principal      *trust.Principal
	Subject        values.EntityRef
	Purpose        string
	EffectiveAt    values.Instant
	Authorization  authz.Request
	Representation *Representation
	Delegation     *Delegation
	Recused        bool
	// RequestedActions are route/UI labels only; current governance still
	// decides whether the stage may perform them.
	RequestedActions []string
}

type Resolution struct {
	Allowed            bool
	SubjectDisclosable bool
	Principal          string
	Subject            values.EntityRef
	Relationship       authz.RelationshipKind
	Role               Role
	Representation     Representation
	Delegation         Delegation
	Assurance          trust.Assurance
	Purpose            string
	AllowedActions     []string
	Decision           authz.Decision
	Expiry             values.Instant
	Reason             string
	EvidenceID         string
}

// Resolver is stateless; the value exists to make dependency injection at a
// flow boundary explicit while retaining the pure Resolve function.
type Resolver struct{}

func (Resolver) Resolve(s Stage) (Resolution, error) { return Resolve(s) }

// ResolveStage is the named entry point used by flow compilers.
func ResolveStage(s Stage) (Resolution, error) { return Resolve(s) }

// Resolve evaluates current AuthZ and then attaches explicit, attributed
// assistance. Denials intentionally omit subject, relationship and action
// detail to prevent existence and authority disclosure.
func Resolve(s Stage) (Resolution, error) {
	if s.Principal == nil || s.StageID == "" || !s.EffectiveAt.IsSet() {
		return Resolution{}, fmt.Errorf("%w: principal, stage_id and effective_at are required", ErrInvalidStage)
	}
	if err := s.Subject.Validate(); err != nil {
		return Resolution{}, fmt.Errorf("%w: subject: %v", ErrInvalidStage, err)
	}
	if s.Authorization.Principal != s.Principal {
		s.Authorization.Principal = s.Principal
	}
	if s.Authorization.Subject.Validate() == nil && s.Authorization.Subject != s.Subject {
		return Resolution{}, fmt.Errorf("%w: authorization subject mismatch", ErrInvalidStage)
	}
	s.Authorization.Subject = s.Subject
	s.Authorization.Purpose = s.Purpose
	s.Authorization.EffectiveAt = s.EffectiveAt
	d, err := authz.Enforce(s.Authorization)
	if err != nil {
		return Resolution{}, err
	}
	base := Resolution{Allowed: d.SubjectDisclosable, SubjectDisclosable: d.SubjectDisclosable, Principal: s.Principal.Subject(), Assurance: s.Principal.Assurance(), Purpose: d.Purpose, Decision: d, EvidenceID: digest(s.StageID, s.Principal.Fingerprint(), d.EvidenceID)}
	if !d.SubjectDisclosable {
		base.Reason = "not_authorized"
		return base, nil
	}
	base.Subject = s.Subject
	base.Relationship = d.Scope.Relationship
	base.Role = roleFor(d.Scope.Relationship)
	if s.Recused {
		return Resolution{Allowed: false, SubjectDisclosable: true, Principal: s.Principal.Subject(), Assurance: s.Principal.Assurance(), Purpose: d.Purpose, Decision: d, Reason: "recused", EvidenceID: base.EvidenceID}, nil
	}
	if s.Representation != nil {
		r := *s.Representation
		if r.Subject != s.Subject || r.Representative == "" || r.EvidenceRef == "" {
			return Resolution{}, fmt.Errorf("%w: representation must name subject, representative and evidence", ErrInvalidStage)
		}
		if r.ExpiresAt.IsSet() && !r.ExpiresAt.After(s.EffectiveAt) {
			return Resolution{}, ErrExpired
		}
		base.Representation = r
		base.Expiry = r.ExpiresAt
	}
	if s.Delegation != nil {
		del := *s.Delegation
		if del.GrantRef == "" || del.FromPrincipal == "" || del.ToPrincipal != s.Principal.Subject() || !del.ExpiresAt.IsSet() || !del.ExpiresAt.After(s.EffectiveAt) {
			return Resolution{}, fmt.Errorf("%w: invalid bounded grant", ErrInvalidStage)
		}
		base.Delegation = del
		base.Expiry = del.ExpiresAt
	}
	base.AllowedActions = slices.Clone(s.RequestedActions)
	if s.Delegation != nil {
		allowed := make(map[string]bool, len(s.Delegation.AllowedActions))
		for _, a := range s.Delegation.AllowedActions {
			allowed[a] = true
		}
		for _, a := range base.AllowedActions {
			if !allowed[a] {
				base.Allowed = false
				base.AllowedActions = nil
				base.Reason = "delegation_scope"
				return base, nil
			}
		}
	}
	return base, nil
}

func roleFor(r authz.RelationshipKind) Role {
	switch r {
	case authz.RelationshipSelf:
		return RoleSelf
	case authz.RelationshipManagerChain:
		return RoleManager
	case authz.RelationshipHRPartner:
		return RoleHRSpecialist
	default:
		return RoleReviewer
	}
}
func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(fmt.Sprintf("%d:%s;", len(p), p)))
	}
	return "ev:uxflow:" + hex.EncodeToString(h.Sum(nil))[:32]
}
