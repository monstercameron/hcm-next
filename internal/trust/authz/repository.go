package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// ErrScopeRequired is returned when a repository operation is attempted
// without a currently evaluated [RepositoryScope], with a scope that was
// never produced by [PlanRepositoryScope], or with one whose evaluated
// instant no longer matches the instant the repository is reading at. It is
// distinct from an authorization denial: a caller receiving it has tried to
// reach the data layer around the planner, and the repository fails closed.
var ErrScopeRequired = errors.New("authz: repository operation requires a currently evaluated AuthorizationScope")

// RepositoryQueryRequest is the caller-proposed query [PlanRepositoryScope]
// evaluates. The candidate list is the only query shape there is: a query
// names every record it proposes to touch, so there is no way to express a
// wildcard population, and the planner intersects the candidates down to the
// authorized subset rather than ever widening anything.
type RepositoryQueryRequest struct {
	// Principal is the authenticated actor. It must come from the identity
	// plane; a caller-constructed tenant claim cannot substitute for it.
	Principal *trust.Principal
	// Purpose is the declared purpose of use, or empty to fall back to the
	// principal's default purpose.
	Purpose string
	// EffectiveAt is the instant every temporal check is evaluated at and
	// the only instant the resulting scope is valid for.
	EffectiveAt values.Instant
	// Tenant is the tenant the query targets. Every candidate subject must
	// carry this tenant; a query is single-tenant by construction.
	Tenant values.TenantId
	// Candidates is every record the query proposes to touch, each with the
	// relationship facts the principal holds over it. An empty candidate
	// list is a wildcard attempt and is rejected.
	Candidates []ScopeInput
	// PrincipalOrg and ResourceOrg, OrgEdges and Sharing feed
	// [ResolveTenantScope] (TRUST-008).
	PrincipalOrg OrgUnitRef
	ResourceOrg  OrgUnitRef
	OrgEdges     []OrgEdge
	Sharing      []SharingGrant
	// Fields is every field the query proposes to project.
	Fields []FieldID
}

// RepositoryScope is the TRUST-012 evaluated scope a sensitive repository
// requires. It is the planner's answer, not a caller's request: its zero
// value is invalid and fails closed, it cannot be constructed outside this
// package with the evaluated flag set, it names its authorized records
// explicitly rather than expressing "all records", and it is valid only for
// the tenant, purpose and instant it was evaluated at. A repository may
// narrow a scope further; nothing downstream may broaden it.
type RepositoryScope struct {
	// evaluated is the construction token. Only [PlanRepositoryScope] sets
	// it, so a composite literal built by a caller is always the invalid
	// zero scope.
	evaluated bool

	effect        Effect
	ruleID        string
	reason        string
	tenant        values.TenantId
	purpose       string
	evaluatedAt   values.Instant
	organizations []OrgUnitRef
	// mandatoryDenies carries the non-delegable restriction rule IDs the
	// tenant stage attached (for example the cross-tenant sensitive-domain
	// deny). A repository applies them even against an allow.
	mandatoryDenies []string
	// subjects is the authorized subset of the request's candidates, in
	// candidate order. An allow with an empty subjects list is a tenant the
	// principal may reach but no record it is authorized on.
	subjects []values.EntityRef
	fields   map[FieldID]FieldRuling

	policyVersion string
	inputsDigest  string
	evidenceID    string
}

// Zero reports whether s is the invalid zero scope: never evaluated, never
// usable, fails closed everywhere.
func (s RepositoryScope) Zero() bool { return !s.evaluated }

// Valid reports whether s was produced by [PlanRepositoryScope] and carries
// a legal effect.
func (s RepositoryScope) Valid() bool { return s.evaluated && s.effect.Valid() }

// Effect returns the scope's effect: EffectAllow means the repository may
// serve the intersection of [RepositoryScope.AllowedSubjects] with what it
// stores; EffectDenied means it serves nothing.
func (s RepositoryScope) Effect() Effect {
	if !s.evaluated {
		return EffectDenied
	}
	return s.effect
}

// RuleID returns the identifier of the rule that decided the scope, or the
// empty string on the zero scope.
func (s RepositoryScope) RuleID() string { return s.ruleID }

// Reason returns the policy reason token, safe to log and to return to a
// caller; it never names a record or discloses existence.
func (s RepositoryScope) Reason() string { return s.reason }

// Tenant returns the tenant the query was evaluated against.
func (s RepositoryScope) Tenant() values.TenantId { return s.tenant }

// Purpose returns the purpose the scope was evaluated under. A repository
// read under a different purpose needs a new scope.
func (s RepositoryScope) Purpose() string { return s.purpose }

// EvaluatedAt returns the instant the scope is valid for. A repository read
// at any other instant fails closed.
func (s RepositoryScope) EvaluatedAt() values.Instant { return s.evaluatedAt }

// Organizations returns the resolved organization closure the tenant stage
// allowed, or nil for a tenant-wide or denied scope. The returned slice is a
// copy; a caller cannot mutate the scope through it.
func (s RepositoryScope) Organizations() []OrgUnitRef {
	return slices.Clone(s.organizations)
}

// MandatoryDenies returns the non-delegable restriction rule IDs still in
// force. The returned slice is a copy.
func (s RepositoryScope) MandatoryDenies() []string {
	return slices.Clone(s.mandatoryDenies)
}

// AllowedSubjects returns the authorized records, explicitly named. The
// returned slice is a copy.
func (s RepositoryScope) AllowedSubjects() []values.EntityRef {
	return slices.Clone(s.subjects)
}

// AuthorizesRecord reports whether the scope permits reading subject at at.
// It is false for the zero scope, for a denied scope, for a subject that is
// not in the authorized set, and for any instant other than the one the
// scope was evaluated at: time is part of the intersection, and a scope
// never outlives its evaluation.
func (s RepositoryScope) AuthorizesRecord(subject values.EntityRef, at values.Instant) bool {
	if !s.evaluated || s.effect != EffectAllow {
		return false
	}
	if at.Compare(s.evaluatedAt) != 0 {
		return false
	}
	return slices.Contains(s.subjects, subject)
}

// FieldRuling returns the ruling for f. A field the query did not request —
// one absent from the evaluated field mask — is denied: the mask is closed,
// not a floor.
func (s RepositoryScope) FieldRuling(f FieldID) FieldRuling {
	if ruling, ok := s.fields[f]; ok {
		return ruling
	}
	return FieldRuling{Effect: EffectDenied, RuleID: "p1a.repository.field_not_in_scope", Reason: "field_not_in_scope"}
}

// Fields returns the evaluated field mask. The returned map is a copy.
func (s RepositoryScope) Fields() map[FieldID]FieldRuling {
	out := make(map[FieldID]FieldRuling, len(s.fields))
	for f, r := range s.fields {
		out[f] = r
	}
	return out
}

// PolicyVersion returns the policy version the scope was evaluated against.
func (s RepositoryScope) PolicyVersion() string { return s.policyVersion }

// InputsDigest returns the canonical digest over every input the scope was
// computed from. Two plans with the same inputs always produce the same
// digest; any difference in input produces a different one, which is what
// keeps two principals' or two tenants' scopes from ever sharing a cache key.
func (s RepositoryScope) InputsDigest() string { return s.inputsDigest }

// EvidenceID returns the durable identifier for this scope's evidence record.
func (s RepositoryScope) EvidenceID() string { return s.evidenceID }

// Validate reports whether s carries the evidence a durable authorization
// record requires. A scope failing this check must never be recorded or
// enforced.
func (s RepositoryScope) Validate() error {
	if !s.evaluated {
		return fmt.Errorf("%w: scope was never evaluated by the planner", ErrScopeRequired)
	}
	if !s.effect.Valid() {
		return fmt.Errorf("%w: scope carries no legal effect", ErrScopeRequired)
	}
	if s.ruleID == "" {
		return fmt.Errorf("%w: scope carries no rule ID", ErrScopeRequired)
	}
	if s.effect == EffectDenied && s.reason == "" {
		return fmt.Errorf("%w: denied scope carries no reason", ErrScopeRequired)
	}
	if err := s.tenant.Validate(); err != nil {
		return fmt.Errorf("%w: scope tenant: %v", ErrScopeRequired, err)
	}
	if !s.evaluatedAt.IsSet() {
		return fmt.Errorf("%w: scope carries no evaluated instant", ErrScopeRequired)
	}
	if s.policyVersion == "" || s.inputsDigest == "" || s.evidenceID == "" {
		return fmt.Errorf("%w: scope carries no policy version, inputs digest or evidence id", ErrScopeRequired)
	}
	for _, sub := range s.subjects {
		if sub.Tenant != s.tenant {
			return fmt.Errorf("%w: scope grants a subject outside its own tenant", ErrScopeRequired)
		}
	}
	return nil
}

// PlanRepositoryScope implements TRUST-012: it runs the tenant (TRUST-008),
// record/population (TRUST-009) and field/purpose (TRUST-010) stages over the
// query's explicit candidates and produces the one [RepositoryScope] a
// sensitive repository accepts. The intersection order is fixed — tenant,
// then organization, then population, then field, then time, then purpose —
// and every stage can only narrow: no caller input can widen the result, and
// there is no input shape that means "everything".
func PlanRepositoryScope(req RepositoryQueryRequest) (RepositoryScope, error) {
	if req.Principal == nil {
		return RepositoryScope{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	if err := req.Tenant.Validate(); err != nil {
		return RepositoryScope{}, fmt.Errorf("%w: query tenant: %v", ErrInvalidPolicyInput, err)
	}
	if !req.EffectiveAt.IsSet() {
		return RepositoryScope{}, fmt.Errorf("%w: query effective instant is not set", ErrInvalidPolicyInput)
	}
	if len(req.Candidates) == 0 {
		return RepositoryScope{}, fmt.Errorf("%w: repository query names no candidates; a wildcard population has no authorized shape", ErrInvalidPolicyInput)
	}
	for _, c := range req.Candidates {
		if err := c.Subject.Validate(); err != nil {
			return RepositoryScope{}, fmt.Errorf("%w: candidate subject: %v", ErrInvalidPolicyInput, err)
		}
		if c.Subject.Tenant != req.Tenant {
			return RepositoryScope{}, fmt.Errorf("%w: candidate subject %s is outside the query's tenant", ErrInvalidPolicyInput, c.Subject.String())
		}
	}

	purpose := req.Purpose
	if purpose == "" {
		purpose = req.Principal.DefaultPurpose()
	}

	// Stage 1: tenant and organization scope. A deny here is the scope's
	// whole answer; nothing downstream runs.
	tenantDecision, err := ResolveTenantScope(req.Principal, TenantScopeInput{
		ResourceTenant: req.Tenant,
		PrincipalOrg:   req.PrincipalOrg,
		ResourceOrg:    req.ResourceOrg,
		Edges:          req.OrgEdges,
		Sharing:        req.Sharing,
		EffectiveAt:    req.EffectiveAt,
	})
	if err != nil {
		return RepositoryScope{}, err
	}

	scope := RepositoryScope{
		evaluated:       true,
		effect:          tenantDecision.Effect,
		ruleID:          "p1a.repository.scope." + tenantDecision.RuleID,
		reason:          tenantDecision.Reason,
		tenant:          req.Tenant,
		purpose:         purpose,
		evaluatedAt:     req.EffectiveAt,
		organizations:   tenantDecision.AllowedOrganizations,
		mandatoryDenies: tenantDecision.MandatoryDenies,
		policyVersion:   PolicyVersion,
	}

	// Stage 2: record/population authorization, candidate by candidate. An
	// unauthorized candidate is dropped, never returned with an error
	// attached, so that the plan's shape cannot be used to enumerate which
	// subjects almost matched.
	if tenantDecision.Effect == EffectAllow {
		for _, c := range req.Candidates {
			recordScope, err := ResolveAuthorizationScope(req.Principal, ScopeInput{
				Subject:       c.Subject,
				EffectiveAt:   req.EffectiveAt,
				Relationships: c.Relationships,
			})
			if err != nil {
				return RepositoryScope{}, err
			}
			if recordScope.Effect == EffectAllow {
				scope.subjects = append(scope.subjects, c.Subject)
			}
		}
	}

	// Stage 3: field and purpose authorization. Only computed when at least
	// one record survived; a scope over nothing projects nothing.
	if tenantDecision.Effect == EffectAllow && len(scope.subjects) > 0 {
		fieldDecision, err := ResolveFields(req.Principal, purpose, req.Fields, tenantDecision.MandatoryDenies)
		if err != nil {
			return RepositoryScope{}, err
		}
		scope.fields = fieldDecision.Rulings
	}

	scope.inputsDigest = canonicalRepositoryDigest(req, purpose)
	scope.evidenceID = "ev:authzrepo:" + scope.inputsDigest[:32]
	return scope, nil
}

// RedactedPlaceholder is the value a repository projects for a
// [EffectRedacted] field. The raw value is never returned in any form.
const RedactedPlaceholder = "[redacted]"

// FieldValue is one projected field: the effect it was disclosed under and,
// for an allow, the value itself. A denied field never appears in a
// projection at all, and a redacted field's Value is only ever
// [RedactedPlaceholder].
type FieldValue struct {
	FieldID FieldID
	Effect  Effect
	Value   string
}

// Projection is one authorized record projection: a subject the scope
// authorized and the fields it may be read with.
type Projection struct {
	Subject values.EntityRef
	Fields  []FieldValue
}

// RepositoryGate is the data-layer defense-in-depth gate every sensitive
// repository method is shaped around: it takes a [RepositoryScope], never
// bare identifiers, and it intersects what it is asked for with what the
// scope authorizes. The gate may narrow a scope's answer; it has no path
// that broadens it.
type RepositoryGate struct {
	records map[values.EntityRef]map[FieldID]string
}

// NewRepositoryGate builds a gate over the given records. It exists for
// wiring and tests; a real repository owns its storage and embeds the gate's
// checks instead.
func NewRepositoryGate(records map[values.EntityRef]map[FieldID]string) *RepositoryGate {
	return &RepositoryGate{records: records}
}

// Query returns projections for requested, narrowed to the records scope
// authorizes at the instant the scope was evaluated at.
//
// It fails closed with [ErrScopeRequired] when scope is the zero value (a
// caller trying to reach the data layer without the planner), when scope
// fails evidence validation, or when the read instant is not the scope's
// evaluated instant. A valid denied scope returns zero rows rather than an
// error, so that the response's shape cannot serve as an existence oracle.
// Requested records the scope does not authorize are dropped silently, for
// the same reason, and every projected field obeys the scope's field mask:
// denied fields are omitted, redacted fields carry only
// [RedactedPlaceholder].
func (g *RepositoryGate) Query(scope RepositoryScope, requested []values.EntityRef, at values.Instant) ([]Projection, error) {
	if scope.Zero() {
		return nil, ErrScopeRequired
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if at.Compare(scope.EvaluatedAt()) != 0 {
		return nil, ErrScopeRequired
	}
	if scope.Effect() != EffectAllow {
		return []Projection{}, nil
	}

	out := make([]Projection, 0, len(requested))
	for _, subject := range requested {
		if !scope.AuthorizesRecord(subject, at) {
			continue
		}
		record := g.records[subject]
		projection := Projection{Subject: subject}
		for f, ruling := range scope.Fields() {
			switch ruling.Effect {
			case EffectAllow:
				value, stored := record[f]
				if !stored {
					continue
				}
				projection.Fields = append(projection.Fields, FieldValue{FieldID: f, Effect: EffectAllow, Value: value})
			case EffectRedacted:
				if _, stored := record[f]; stored {
					projection.Fields = append(projection.Fields, FieldValue{FieldID: f, Effect: EffectRedacted, Value: RedactedPlaceholder})
				}
			default:
				// DENIED, WITHHELD and unknown rulings project nothing.
			}
		}
		slices.SortFunc(projection.Fields, func(a, b FieldValue) int {
			return cmpStrings(string(a.FieldID), string(b.FieldID))
		})
		out = append(out, projection)
	}
	return out, nil
}

func canonicalRepositoryDigest(req RepositoryQueryRequest, purpose string) string {
	h := sha256.New()
	write := func(label, v string) {
		fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v)
	}

	write("principal", req.Principal.Fingerprint())
	write("purpose", purpose)
	write("effective_at", req.EffectiveAt.String())
	write("tenant", req.Tenant.String())
	write("principal_org", req.PrincipalOrg.Tenant.String()+"/"+req.PrincipalOrg.ID)
	write("resource_org", req.ResourceOrg.Tenant.String()+"/"+req.ResourceOrg.ID)

	candidates := slices.Clone(req.Candidates)
	slices.SortFunc(candidates, func(a, b ScopeInput) int {
		return cmpStrings(a.Subject.String(), b.Subject.String())
	})
	fmt.Fprintf(h, "candidates[%d]:", len(candidates))
	for _, c := range candidates {
		write("candidate.subject", c.Subject.String())
		facts := slices.Clone(c.Relationships)
		slices.SortFunc(facts, func(a, b RelationshipFact) int {
			ka := a.Kind.String() + a.Subject.String() + a.Source
			kb := b.Kind.String() + b.Subject.String() + b.Source
			return cmpStrings(ka, kb)
		})
		fmt.Fprintf(h, "facts[%d]:", len(facts))
		for _, rel := range facts {
			write("rel.kind", rel.Kind.String())
			write("rel.subject", rel.Subject.String())
			write("rel.source", rel.Source)
			write("rel.effective", rel.Effective.String())
			write("rel.recorded_at", rel.RecordedAt.String())
			write("rel.known_at", rel.KnownAt.String())
		}
	}

	edges := slices.Clone(req.OrgEdges)
	slices.SortFunc(edges, func(a, b OrgEdge) int {
		ka := a.Child.Tenant.String() + "/" + a.Child.ID + ">" + a.Parent.Tenant.String() + "/" + a.Parent.ID
		kb := b.Child.Tenant.String() + "/" + b.Child.ID + ">" + b.Parent.Tenant.String() + "/" + b.Parent.ID
		return cmpStrings(ka, kb)
	})
	fmt.Fprintf(h, "edges[%d]:", len(edges))
	for _, e := range edges {
		write("edge.child", e.Child.Tenant.String()+"/"+e.Child.ID)
		write("edge.parent", e.Parent.Tenant.String()+"/"+e.Parent.ID)
		write("edge.effective", e.Effective.String())
	}

	sharing := slices.Clone(req.Sharing)
	slices.SortFunc(sharing, func(a, b SharingGrant) int {
		ka := a.OwnerTenant.String() + ">" + a.ViewerTenant.String()
		kb := b.OwnerTenant.String() + ">" + b.ViewerTenant.String()
		return cmpStrings(ka, kb)
	})
	fmt.Fprintf(h, "sharing[%d]:", len(sharing))
	for _, g := range sharing {
		write("sharing.owner", g.OwnerTenant.String())
		write("sharing.viewer", g.ViewerTenant.String())
		write("sharing.direction", g.Direction.String())
		write("sharing.effective", g.Effective.String())
	}

	fields := slices.Clone(req.Fields)
	slices.Sort(fields)
	fmt.Fprintf(h, "fields[%d]:", len(fields))
	for _, f := range fields {
		write("field", string(f))
	}

	return hex.EncodeToString(h.Sum(nil))
}

func cmpStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
