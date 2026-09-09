// Package org owns the governed manager-relationship read. It is deliberately
// storage-neutral: a worker-facts adapter supplies one consistent projection
// for each worker and this package resolves only the relationship semantics.
package org

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	ManagerResolutionIntentType    = "hcmnext.org.manager_relationship.read"
	ManagerResolutionIntentVersion = "v1"
	ManagerResolutionRulePack      = "org.manager_relationship/2026.1"
)

var (
	ErrInvalidRequest       = errors.New("org: manager resolution request is invalid")
	ErrInvalidFact          = errors.New("org: manager relationship fact is invalid")
	ErrReaderFailed         = errors.New("org: manager relationship reader failed")
	ErrSubjectMismatch      = errors.New("org: manager relationship reader answered about another worker")
	ErrRelationshipCycle    = errors.New("org: manager relationship chain contains a cycle")
	ErrDepthExceeded        = errors.New("org: manager relationship chain exceeds the declared depth")
	ErrAuthorizationMissing = errors.New("org: manager relationship authorization is missing")
	ErrAuthorizationInvalid = errors.New("org: manager relationship authorization is invalid")
)

// RelationshipType distinguishes the exclusive direct manager edge from a
// matrix relationship. Dotted-line edges never become a chain parent.
type RelationshipType string

const (
	RelationshipDirectManager RelationshipType = "DIRECT_MANAGER"
	RelationshipDottedLine    RelationshipType = "DOTTED_LINE"
)

func (t RelationshipType) Valid() bool {
	return t == RelationshipDirectManager || t == RelationshipDottedLine
}

// ResolutionStatus is the result of resolving the primary direct edge.
type ResolutionStatus string

const (
	StatusResolved    ResolutionStatus = "RESOLVED"
	StatusVacant      ResolutionStatus = "VACANT"
	StatusAmbiguous   ResolutionStatus = "AMBIGUOUS"
	StatusStale       ResolutionStatus = "STALE"
	StatusDisagreeing ResolutionStatus = "DISAGREEING"
)

// ManagerRelationshipFact is one bitemporal assertion from Worker to Manager.
// AssignmentID is required: a manager relationship cannot be silently
// detached from the assignment that made it effective.
type ManagerRelationshipFact struct {
	RelationshipID string
	Type           RelationshipType
	Worker         values.EntityRef
	Manager        values.EntityRef
	AssignmentID   string

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

func (f ManagerRelationshipFact) Validate() error {
	if f.RelationshipID == "" || f.AssignmentID == "" {
		return fmt.Errorf("%w: relationship and assignment ids are required", ErrInvalidFact)
	}
	if !f.Type.Valid() {
		return fmt.Errorf("%w: relationship type %q", ErrInvalidFact, f.Type)
	}
	if err := f.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidFact, err)
	}
	if err := f.Manager.Validate(); err != nil {
		return fmt.Errorf("%w: manager: %v", ErrInvalidFact, err)
	}
	if f.Worker.Kind != people.KindWorker || f.Manager.Kind != people.KindWorker {
		return fmt.Errorf("%w: both endpoints must be workers", ErrInvalidFact)
	}
	if f.Worker.Tenant != f.Manager.Tenant {
		return fmt.Errorf("%w: endpoints cross tenants", ErrInvalidFact)
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidFact, err)
	}
	if f.Effective.Kind() != values.IntervalKindInstant {
		return fmt.Errorf("%w: effective interval must be INSTANT", ErrInvalidFact)
	}
	if f.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: known-at is required", ErrInvalidFact)
	}
	if !f.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrInvalidFact)
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %v", ErrInvalidFact, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %v", ErrInvalidFact, err)
	}
	return values.ValidateKnowledgeOrder(f.KnownAt, f.Provenance.RecordedAt, false)
}

// WorkerFactsQuery is the exact bitemporal question sent to the reader.
type WorkerFactsQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   values.Instant
	// KnownAt is optional for callers that ask for the reader's current
	// knowledge. When set, facts known after it are stale and must not be used.
	KnownAt values.KnownAt
}

func (q WorkerFactsQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidRequest, err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidRequest, err)
	}
	if q.Worker.Tenant != q.Tenant || q.Worker.Kind != people.KindWorker {
		return fmt.Errorf("%w: worker is outside the requested tenant or is not a worker", ErrInvalidRequest)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidRequest, err)
	}
	return nil
}

// WorkerFactSet is one reader answer. Stale and Disagreeing are explicit
// adapter findings; the resolver never guesses around either condition.
type WorkerFactSet struct {
	Worker        values.EntityRef
	Exists        bool
	Relationships []ManagerRelationshipFact
	Watermark     values.RevisionToken
	PolicyVersion string
	Stale         bool
	Disagreeing   bool
}

func (s WorkerFactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidFact, err)
	}
	if !s.Exists {
		if len(s.Relationships) != 0 {
			return fmt.Errorf("%w: absent worker has relationships", ErrInvalidFact)
		}
		return nil
	}
	if !s.Watermark.IsSpecified() || s.PolicyVersion == "" {
		return fmt.Errorf("%w: existing worker needs watermark and policy version", ErrInvalidFact)
	}
	seen := make(map[string]struct{}, len(s.Relationships))
	for _, f := range s.Relationships {
		if err := f.Validate(); err != nil {
			return err
		}
		if f.Worker != s.Worker {
			return fmt.Errorf("%w: fact worker does not match set worker", ErrSubjectMismatch)
		}
		if _, ok := seen[f.RelationshipID]; ok {
			return fmt.Errorf("%w: duplicate relationship %s", ErrInvalidFact, f.RelationshipID)
		}
		seen[f.RelationshipID] = struct{}{}
	}
	return nil
}

// WorkerFacts is the narrow port used by the resolver. It is shaped like the
// people WorkerFacts port but returns the relationship projection owned here.
type WorkerFacts interface {
	WorkerFactsAt(context.Context, WorkerFactsQuery) (WorkerFactSet, error)
}

// Authorizer evaluates disclosure for the one relationship being considered.
// Authorization is deliberately outside this package; the resolver only
// applies the returned WITHHELD/DENIED/ALLOW decision.
type Authorizer func(ManagerRelationshipFact) people.AuthorizationDecision

type ManagerResolutionRequest struct {
	Tenant    values.TenantId
	Worker    values.EntityRef
	AsOf      values.Instant
	KnownAt   values.KnownAt
	MaxDepth  int
	Authorize Authorizer
}

func (r ManagerResolutionRequest) Validate() error {
	q := WorkerFactsQuery{Tenant: r.Tenant, Worker: r.Worker, AsOf: r.AsOf, KnownAt: r.KnownAt}
	if err := q.Validate(); err != nil {
		return err
	}
	if r.MaxDepth < 1 {
		return fmt.Errorf("%w: max depth must be positive", ErrInvalidRequest)
	}
	if r.Authorize == nil {
		return ErrAuthorizationMissing
	}
	return nil
}

// ManagerReference is the manager endpoint after the per-hop disclosure
// decision. A denied reference has no value; a withheld hop has no reference
// object at all in the result.
type ManagerReference struct {
	Access       people.Access
	Value        values.EntityRef
	DenialReason string
}

// ManagerHop is a disclosed relationship. Evidence coordinates remain on an
// authorized or field-denied hop, while a WITHHELD hop carries only its reason.
type ManagerHop struct {
	Level          int
	RelationshipID string
	Type           RelationshipType
	AssignmentID   string
	Subject        values.EntityRef
	Disclosure     people.Disclosure
	WithheldReason string
	Manager        ManagerReference

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

type ManagerResolution struct {
	Worker     values.EntityRef
	AsOf       values.Instant
	MaxDepth   int
	Status     ResolutionStatus
	Disclosure people.Disclosure

	Direct        *ManagerHop
	Chain         []ManagerHop
	DottedLines   []ManagerHop
	Watermark     values.RevisionToken
	PolicyVersion string

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
	Receipt      evidence.ZeroEffectReceipt
}

func (r ManagerResolution) canonicalBody() []byte {
	w := canonicalbytes.New("hcmnext.domains.org.ManagerResolution", 1).
		Value("worker", r.Worker).
		Value("as_of", r.AsOf).
		String("status", string(r.Status)).
		String("disclosure", r.Disclosure.String()).
		Int("max_depth", int64(r.MaxDepth)).
		String("policy_version", r.PolicyVersion).
		Bool("watermark?", r.Watermark.IsSpecified())
	if r.Watermark.IsSpecified() {
		w.Value("watermark", r.Watermark)
	}
	w.Count("chain", len(r.Chain))
	for _, h := range r.Chain {
		w.Field("hop", canonicalHop(h))
	}
	w.Count("dotted_lines", len(r.DottedLines))
	for _, h := range r.DottedLines {
		w.Field("dotted_hop", canonicalHop(h))
	}
	w.Value("effects", r.Effects)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func canonicalHop(h ManagerHop) []byte {
	w := canonicalbytes.New("hcmnext.domains.org.ManagerHop", 1).
		Int("level", int64(h.Level)).
		String("relationship_id", h.RelationshipID).
		String("type", string(h.Type)).
		String("assignment_id", h.AssignmentID).
		String("disclosure", h.Disclosure.String()).
		String("withheld_reason", h.WithheldReason).
		String("manager.access", h.Manager.Access.String()).
		String("manager.denial_reason", h.Manager.DenialReason)
	if h.Manager.Access == people.AccessAuthorized {
		w.Value("manager", h.Manager.Value)
	}
	if h.Disclosure != people.DisclosureWithheld {
		w.Value("effective", h.Effective).
			Value("known_at", h.KnownAt).
			Value("revision", h.Revision).
			Value("authority", h.Authority).
			Value("provenance", h.Provenance)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r ManagerResolution) Canonical() []byte {
	body := r.canonicalBody()
	if len(body) == 0 {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.org.ManagerResolutionEnvelope", 1).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Value("receipt", r.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func authorizeHop(f ManagerRelationshipFact, authorize Authorizer) (ManagerHop, error) {
	decision := authorize(f)
	if err := decision.Validate(); err != nil {
		return ManagerHop{}, fmt.Errorf("%w: %v", ErrAuthorizationInvalid, err)
	}
	if err := decision.Covers([]people.FieldID{people.FieldManagerRelation}); err != nil {
		return ManagerHop{}, fmt.Errorf("%w: %v", ErrAuthorizationInvalid, err)
	}
	if !decision.SubjectDisclosable {
		return ManagerHop{Disclosure: people.DisclosureWithheld, WithheldReason: decision.SubjectDenialReason}, nil
	}
	hop := ManagerHop{
		RelationshipID: f.RelationshipID,
		Type:           f.Type,
		AssignmentID:   f.AssignmentID,
		Subject:        f.Worker,
		Effective:      f.Effective,
		KnownAt:        f.KnownAt,
		Revision:       f.Revision,
		Authority:      f.Authority,
		Provenance:     f.Provenance,
	}
	ruling, _ := decision.RulingFor(people.FieldManagerRelation)
	if ruling.Effect == people.EffectDeny {
		hop.Disclosure = people.DisclosurePartial
		hop.Manager = ManagerReference{Access: people.AccessDenied, DenialReason: ruling.Reason}
		return hop, nil
	}
	hop.Disclosure = people.DisclosureFull
	hop.Manager = ManagerReference{Access: people.AccessAuthorized, Value: f.Manager}
	return hop, nil
}

func finish(r ManagerResolution) (ManagerResolution, error) {
	body := r.canonicalBody()
	if len(body) == 0 {
		return ManagerResolution{}, fmt.Errorf("%w: result is not canonical", ErrInvalidRequest)
	}
	r.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		ManagerResolutionIntentType, ManagerResolutionIntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		[]evidence.ControlVersion{{Name: "manager_resolution_policy", Version: r.PolicyVersion}, {Name: "manager_resolution_rules", Version: ManagerResolutionRulePack}},
		r.InputsDigest, r.ResultDigest, r.Effects)
	if err != nil {
		return ManagerResolution{}, err
	}
	r.Receipt = receipt
	return r, nil
}

func mergeDisclosure(current people.Disclosure, hop people.Disclosure) people.Disclosure {
	if hop == people.DisclosureWithheld {
		if current == people.DisclosureUnspecified {
			return people.DisclosureWithheld
		}
		return people.DisclosurePartial
	}
	if hop == people.DisclosurePartial {
		return people.DisclosurePartial
	}
	if current == people.DisclosureUnspecified {
		return people.DisclosureFull
	}
	return current
}

// ResolveManagerRelationships resolves the direct manager, the direct chain,
// and dotted-line edges from each traversed worker. It never treats a dotted
// line as the primary parent and never follows a withheld or denied reference.
func ResolveManagerRelationships(ctx context.Context, reader WorkerFacts, req ManagerResolutionRequest) (ManagerResolution, error) {
	if reader == nil {
		return ManagerResolution{}, fmt.Errorf("%w: no worker facts reader", ErrInvalidRequest)
	}
	if err := req.Validate(); err != nil {
		return ManagerResolution{}, err
	}
	inputs, err := canonicalbytes.New("hcmnext.domains.org.ManagerResolutionRequest", 1).
		String("tenant", string(req.Tenant)).
		Value("worker", req.Worker).
		Value("as_of", req.AsOf).
		Int("max_depth", int64(req.MaxDepth)).
		Bytes()
	if err != nil {
		return ManagerResolution{}, err
	}
	r := ManagerResolution{Worker: req.Worker, AsOf: req.AsOf, MaxDepth: req.MaxDepth, InputsDigest: canonicalbytes.Digest(inputs), Effects: evidence.ZeroEffects()}
	current := req.Worker
	visited := map[string]bool{current.String(): true}
	var watermark values.RevisionToken
	var policy string
	for depth := 0; ; depth++ {
		set, readErr := reader.WorkerFactsAt(ctx, WorkerFactsQuery{Tenant: req.Tenant, Worker: current, AsOf: req.AsOf, KnownAt: req.KnownAt})
		if readErr != nil {
			return ManagerResolution{}, fmt.Errorf("%w: %v", ErrReaderFailed, readErr)
		}
		if err := set.Validate(); err != nil {
			return ManagerResolution{}, err
		}
		if set.Worker != current {
			return ManagerResolution{}, fmt.Errorf("%w: asked %s, answered %s", ErrSubjectMismatch, current, set.Worker)
		}
		if depth == 0 {
			watermark, policy = set.Watermark, set.PolicyVersion
		} else if !set.Watermark.Equal(watermark) || set.PolicyVersion != policy {
			r.Status = StatusDisagreeing
			break
		}
		r.Watermark, r.PolicyVersion = watermark, policy
		if !set.Exists {
			if depth == 0 {
				r.Status = StatusVacant
			} else {
				r.Status = StatusResolved
			}
			break
		}
		if r.Disclosure == people.DisclosureUnspecified {
			r.Disclosure = people.DisclosureFull
		}
		if set.Stale {
			r.Status = StatusStale
			break
		}
		if set.Disagreeing {
			r.Status = StatusDisagreeing
			break
		}
		active := make([]ManagerRelationshipFact, 0, len(set.Relationships))
		for _, f := range set.Relationships {
			covered, containsErr := f.Effective.ContainsInstant(req.AsOf)
			if containsErr != nil {
				return ManagerResolution{}, fmt.Errorf("%w: effective interval: %v", ErrInvalidFact, containsErr)
			}
			if !covered {
				continue
			}
			if req.KnownAt.Canonical() != nil && f.KnownAt.Instant().After(req.KnownAt.Instant()) {
				r.Status = StatusStale
				break
			}
			active = append(active, f)
		}
		if r.Status == StatusStale {
			break
		}
		sort.Slice(active, func(i, j int) bool { return active[i].RelationshipID < active[j].RelationshipID })
		for _, f := range active {
			if f.Type != RelationshipDottedLine {
				continue
			}
			hop, hopErr := authorizeHop(f, req.Authorize)
			if hopErr != nil {
				return ManagerResolution{}, hopErr
			}
			hop.Level = depth
			r.DottedLines = append(r.DottedLines, hop)
			r.Disclosure = mergeDisclosure(r.Disclosure, hop.Disclosure)
		}
		var direct []ManagerRelationshipFact
		for _, f := range active {
			if f.Type == RelationshipDirectManager {
				direct = append(direct, f)
			}
		}
		if len(direct) == 0 {
			if depth == 0 {
				r.Status = StatusVacant
			} else {
				r.Status = StatusResolved
			}
			break
		}
		if len(direct) > 1 {
			r.Status = StatusAmbiguous
			for _, f := range direct {
				hop, hopErr := authorizeHop(f, req.Authorize)
				if hopErr != nil {
					return ManagerResolution{}, hopErr
				}
				hop.Level = depth
				r.Chain = append(r.Chain, hop)
				r.Disclosure = mergeDisclosure(r.Disclosure, hop.Disclosure)
			}
			break
		}
		if depth >= req.MaxDepth {
			return ManagerResolution{}, fmt.Errorf("%w: worker %s at depth %d", ErrDepthExceeded, current, depth)
		}
		hop, hopErr := authorizeHop(direct[0], req.Authorize)
		if hopErr != nil {
			return ManagerResolution{}, hopErr
		}
		hop.Level = depth
		r.Chain = append(r.Chain, hop)
		if depth == 0 {
			copyHop := hop
			r.Direct = &copyHop
		}
		r.Disclosure = mergeDisclosure(r.Disclosure, hop.Disclosure)
		if hop.Disclosure == people.DisclosureWithheld || hop.Manager.Access != people.AccessAuthorized {
			r.Status = StatusResolved
			break
		}
		next := hop.Manager.Value
		if visited[next.String()] {
			return ManagerResolution{}, fmt.Errorf("%w: manager %s", ErrRelationshipCycle, next)
		}
		visited[next.String()] = true
		current = next
	}
	if r.Status == "" {
		r.Status = StatusResolved
	}
	return finish(r)
}

// ManagerExplanation is a bounded, value-free explanation. It names the
// inputs and disclosure shape but never repeats a manager reference or any
// other protected relationship value.
type ManagerExplanation struct {
	Worker         values.EntityRef
	Status         ResolutionStatus
	Disclosure     people.Disclosure
	ChainHops      int
	DottedLineHops int
	WithheldHops   int
	DeniedHops     int
	MaxDepth       int
	Watermark      values.RevisionToken
	PolicyVersion  string
	Inputs         []string
}

func (r ManagerResolution) Explain() ManagerExplanation {
	x := ManagerExplanation{
		Worker: r.Worker, Status: r.Status, Disclosure: r.Disclosure,
		ChainHops: len(r.Chain), DottedLineHops: len(r.DottedLines),
		MaxDepth: r.MaxDepth, Watermark: r.Watermark, PolicyVersion: r.PolicyVersion,
		Inputs: []string{"worker_ref", "as_of_instant", "effective_interval", "known_at", "assignment_id", "watermark", "authorization_policy"},
	}
	for _, h := range append(append([]ManagerHop{}, r.Chain...), r.DottedLines...) {
		switch {
		case h.Disclosure == people.DisclosureWithheld:
			x.WithheldHops++
		case h.Manager.Access == people.AccessDenied:
			x.DeniedHops++
		}
	}
	return x
}

func Explain(r ManagerResolution) ManagerExplanation { return r.Explain() }
