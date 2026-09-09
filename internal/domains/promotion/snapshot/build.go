package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// entrySource is the Source the engine resolve reads through: the exact
// entries the domain reads produced, and nothing else.
//
// The engine's read is source-neutral by design, so this package supplies its
// own trivial source rather than reaching for the engine's test fake. It
// answers only what it was given: an input with no entry is simply absent,
// which the engine's own completeness check turns into a refusal rather than
// this type inventing a value.
type entrySource []enginesnapshot.InputEntry

// Resolve implements enginesnapshot.Source.
func (s entrySource) Resolve(
	_ context.Context, tenant values.TenantId, inputs []enginesnapshot.InputRequest,
) ([]enginesnapshot.InputEntry, error) {
	byName := make(map[string]enginesnapshot.InputEntry, len(s))
	for _, entry := range s {
		if entry.Tenant != tenant {
			continue
		}
		byName[entry.Name] = entry
	}
	out := make([]enginesnapshot.InputEntry, 0, len(inputs))
	for _, in := range inputs {
		if entry, ok := byName[in.Name]; ok {
			out = append(out, entry)
		}
	}
	return out, nil
}

// Subject kinds and authority domains the material projection names. They are
// the same tokens ProposePromotion already puts on the intent's own subject
// list, so the snapshot and the intent name the same subjects the same way.
const (
	subjectKindWorker    = "EMPLOYMENT"
	subjectKindPosition  = "POSITION"
	subjectKindBudget    = "BUDGET"
	authorityPeople      = "PEOPLE"
	authorityPosition    = "POSITION"
	authorityFinance     = "FINANCE"
	resourceKindPromoIn  = values.Kind("promotion_input")
	sourceSystemPeople   = "hcmnext.people"
	sourceSystemOrg      = "hcmnext.org"
	sourceSystemPosition = "hcmnext.position"
	sourceSystemRewards  = "hcmnext.rewards"
	sourceSystemBudget   = "hcmnext.budget"
)

// Target is the placement a promotion proposes: the job, grade, organizational
// unit and pay zone the subject is being moved into. It is the caller's
// intention, and it is the only part of a Build request that is not read.
type Target struct {
	JobCode string
	Grade   string
	OrgUnit string
	PayZone string
}

// Validate reports whether the target is fully stated. All four coordinates
// are required because the pay band both band-position inputs are evaluated
// against is keyed by job code, grade and pay zone, and a band resolved
// against a partially stated target is a band for a different job.
func (t Target) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"job code", t.JobCode},
		{"grade", t.Grade},
		{"organizational unit", t.OrgUnit},
		{"pay zone", t.PayZone},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: target %s is required", ErrRequestInvalid, field.name)
		}
	}
	return nil
}

// Authorization is the already-evaluated set of disclosure decisions the reads
// are performed under.
//
// This package computes none of them. Each is the decision its own domain's
// read already takes as an input, carried here unchanged so that the snapshot
// discloses exactly what the caller was entitled to see and no more. A build
// with a permissive decision and a build with a restrictive one over identical
// records produce different snapshots -- which is correct, because they are
// different answers to different callers.
type Authorization struct {
	// Worker is the people read's decision for the subject.
	Worker people.AuthorizationDecision
	// ManagerHop decides disclosure per manager relationship hop.
	ManagerHop org.Authorizer
	// Position decides whether the caller may see the target position at all.
	Position position.Authorizer
	// Compensation is the compensation read's decision for the subject.
	Compensation rewards.CompensationAuthorization
	// PayBand is the band-position read's decision.
	PayBand rewards.PayBandPositionAuthorization
	// BudgetDisclosable reports whether the caller may learn the pool
	// observation at all. A false makes the budget input WITHHELD rather than
	// absent, and BudgetDenialReason is then required.
	BudgetDisclosable bool
	// BudgetDenialReason is the policy token for a withheld budget input.
	BudgetDenialReason string
}

// Validate reports whether every decision is present and well formed.
func (a Authorization) Validate() error {
	if err := a.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker authorization: %w", ErrRequestInvalid, err)
	}
	if a.ManagerHop == nil {
		return fmt.Errorf("%w: manager hop authorizer is required", ErrRequestInvalid)
	}
	if err := a.Compensation.Validate(); err != nil {
		return fmt.Errorf("%w: compensation authorization: %w", ErrRequestInvalid, err)
	}
	if err := a.PayBand.Validate(); err != nil {
		return fmt.Errorf("%w: pay band authorization: %w", ErrRequestInvalid, err)
	}
	if !a.BudgetDisclosable && strings.TrimSpace(a.BudgetDenialReason) == "" {
		return fmt.Errorf("%w: a withheld budget input needs a reason token", ErrRequestInvalid)
	}
	return nil
}

// Request is one ProposePromotion request's read plan: who is being promoted,
// into what, for how much, at which bitemporal coordinate, under which
// authorization, against which declared rules and reference versions.
//
// There is deliberately no field on it that carries a current fact. Current
// pay, current placement, position vacancy and budget availability are all
// read; a caller can state only what it intends.
type Request struct {
	// Tenant is the single tenant every read is scoped to.
	Tenant values.TenantId
	// Subject is the worker being promoted.
	Subject values.EntityRef
	// TargetPosition is the position the promotion moves them into.
	TargetPosition values.EntityRef
	// Target is the proposed placement.
	Target Target
	// DesiredBasePay is the proposed base pay, in the subject's own currency.
	DesiredBasePay values.Money
	// DesiredPayBasis is the basis the desired amount is expressed in.
	DesiredPayBasis rewards.PayBasis

	// EffectiveOn is the business date every input is asked about.
	EffectiveOn values.LocalDate
	// KnownAt is the knowledge cut-off every input is resolved under.
	KnownAt values.KnownAt
	// Calendar pins the business calendar the effective interval is expressed
	// against.
	Calendar values.CalendarRef

	// ManagerChainDepth bounds the manager chain resolution.
	ManagerChainDepth int
	// Occupants and Pending are the placements the target position's capacity
	// is calculated against (POSITION-002 takes them as inputs rather than
	// discovering them, so the capacity answer is auditable).
	Occupants []position.Occupant
	Pending   []position.PendingProposal

	// Annualization is the declared COMP-002 rule set both band positions are
	// computed through.
	Annualization rewards.CompensationAnnualizationRule

	// BudgetScope and BudgetPeriod address the compensation pool.
	BudgetScope  string
	BudgetPeriod string

	// Authorization is the already-evaluated disclosure decision set.
	Authorization Authorization

	// ReferenceVersion pins the reference/configuration dataset version whose
	// meaning every input in this snapshot depends on.
	ReferenceVersion string
	// SourceConnection names the connection the reads were taken over, which
	// every entry's source reference records.
	SourceConnection string
}

// Validate reports whether the request is well formed on its own terms.
func (r Request) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrRequestInvalid, err)
	}
	if r.Subject.Tenant != r.Tenant || r.Subject.Kind != people.KindWorker {
		return fmt.Errorf("%w: subject must be a worker in the requested tenant", ErrRequestInvalid)
	}
	if err := r.TargetPosition.Validate(); err != nil {
		return fmt.Errorf("%w: target position: %w", ErrRequestInvalid, err)
	}
	if r.TargetPosition.Tenant != r.Tenant || r.TargetPosition.Kind != position.KindPosition {
		return fmt.Errorf("%w: target position must be a position in the requested tenant", ErrRequestInvalid)
	}
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if err := r.DesiredBasePay.Validate(); err != nil {
		return fmt.Errorf("%w: desired base pay: %w", ErrRequestInvalid, err)
	}
	if !r.DesiredPayBasis.Valid() {
		return fmt.Errorf("%w: desired pay basis is unspecified", ErrRequestInvalid)
	}
	if err := r.EffectiveOn.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %w", ErrRequestInvalid, err)
	}
	if r.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: known-at is unset", ErrRequestInvalid)
	}
	if err := r.Calendar.Validate(); err != nil {
		return fmt.Errorf("%w: calendar: %w", ErrRequestInvalid, err)
	}
	if r.ManagerChainDepth < 1 {
		return fmt.Errorf("%w: manager chain depth must be positive", ErrRequestInvalid)
	}
	if err := r.Annualization.Validate(); err != nil {
		return fmt.Errorf("%w: annualization rules: %w", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(r.BudgetScope) == "" || strings.TrimSpace(r.BudgetPeriod) == "" {
		return fmt.Errorf("%w: budget scope and period are required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.ReferenceVersion) == "" {
		return fmt.Errorf("%w: reference/config version is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.SourceConnection) == "" {
		return fmt.Errorf("%w: source connection is required", ErrRequestInvalid)
	}
	return r.Authorization.Validate()
}

// asOfInstant is the instant coordinate the ports that take one are asked at:
// the known-at horizon. Using the horizon rather than a second, independently
// supplied instant is what keeps every input on one knowledge cut-off.
func (r Request) asOfInstant() values.Instant { return r.KnownAt.Instant() }

// Build assembles the immutable Promotion input snapshot for one
// ProposePromotion request.
//
// Every input is read through its owning domain's authorized read, at the one
// as-of/known-at coordinate the request names. The eight results are turned
// into typed entries and resolved into one engine ReadSnapshot, which enforces
// the shared tenant and known-at horizon and mints the read digest. The
// snapshot's own digest is then the kernel's material encoding over the
// material inputs (see [PromotionInputSnapshot.MaterialInputs]).
//
// A required input -- or a conditional one whose condition is satisfied --
// that is MISSING or UNKNOWN refuses the build with an [InputError] naming it.
// The refusal is deliberate rather than a partial snapshot: a promotion
// proposal computed from a baseline that does not know the subject's pay is
// not a weaker proposal, it is a different one.
//
// Build writes nothing and reserves nothing.
func Build(ctx context.Context, readers Readers, req Request) (PromotionInputSnapshot, error) {
	if err := readers.Validate(); err != nil {
		return PromotionInputSnapshot{}, err
	}
	if err := req.Validate(); err != nil {
		return PromotionInputSnapshot{}, err
	}
	effective, err := values.NewOpenLocalDateInterval(req.EffectiveOn, req.Calendar)
	if err != nil {
		return PromotionInputSnapshot{}, fmt.Errorf("%w: effective interval: %w", ErrRequestInvalid, err)
	}

	b := builder{req: req, readers: readers}
	if err := b.readWorker(ctx); err != nil {
		return PromotionInputSnapshot{}, err
	}
	if err := b.readManagerChain(ctx); err != nil {
		return PromotionInputSnapshot{}, err
	}
	if err := b.readPosition(ctx); err != nil {
		return PromotionInputSnapshot{}, err
	}
	if err := b.readCompensation(ctx); err != nil {
		return PromotionInputSnapshot{}, err
	}
	if err := b.readBudget(ctx); err != nil {
		return PromotionInputSnapshot{}, err
	}

	inputs, err := b.ordered()
	if err != nil {
		return PromotionInputSnapshot{}, err
	}

	entries := make([]enginesnapshot.InputEntry, 0, len(inputs))
	requests := make([]enginesnapshot.InputRequest, 0, len(inputs))
	observations := make([]enginesnapshot.SnapshotInput, 0, len(inputs))
	for _, in := range inputs {
		entries = append(entries, in.Entry)
		requests = append(requests, enginesnapshot.InputRequest{
			Name:              in.Name,
			RequiredAuthority: in.Entry.Authority,
		})
		observations = append(observations, in.observation())
	}

	reads, err := enginesnapshot.Resolve(ctx, entrySource(entries),
		enginesnapshot.ConsistencyRequirement{Tenant: req.Tenant, KnownAtHorizon: req.KnownAt}, requests)
	if err != nil {
		return PromotionInputSnapshot{}, fmt.Errorf("promotion/snapshot: resolving the read snapshot: %w", err)
	}

	completeness, completenessErr := enginesnapshot.EvaluateCompleteness(reads, Specification(), observations)

	out := PromotionInputSnapshot{
		Tenant:         req.Tenant,
		Subject:        req.Subject,
		TargetPosition: req.TargetPosition,
		EffectiveOn:    req.EffectiveOn,
		KnownAt:        req.KnownAt,
		EffectiveTime:  effective,
		Reads:          reads,
		Completeness:   completeness,
		inputs:         inputs,
	}
	out.Digest = computeDigest(out.MaterialInputs())

	if refusal := refuse(out, completenessErr); refusal != nil {
		return out, refusal
	}
	return out, nil
}

// refuse turns a non-satisfied completeness verdict into the typed refusal
// PROMO-001 requires: the exact input, its verdict and what this snapshot was
// able to say about it. The engine refuses only an UNKNOWN required input;
// a required input that is a known absence is equally fatal to a promotion
// proposal, so this adds that half rather than letting a MISSING baseline pass
// as a buildable snapshot.
func refuse(s PromotionInputSnapshot, completenessErr error) error {
	if completenessErr != nil && !errors.Is(completenessErr, enginesnapshot.ErrCompletenessRefused) {
		return completenessErr
	}
	if name := enginesnapshot.CompletenessInputOf(completenessErr); name != "" {
		return inputErrorFor(s, name, enginesnapshot.VerdictUnknown, "the input's presence could not be established")
	}
	for _, d := range s.Completeness.Dispositions {
		if d.Verdict != enginesnapshot.VerdictMissing {
			continue
		}
		if d.Policy == enginesnapshot.InputOptional {
			continue
		}
		if d.Policy == enginesnapshot.InputConditional && !d.ConditionActive {
			continue
		}
		return inputErrorFor(s, d.Name, enginesnapshot.VerdictMissing, "the record holds no such input at this coordinate")
	}
	return nil
}

// inputErrorFor builds the refusal for one named input, carrying the
// availability the snapshot recorded and never the value it could not
// disclose.
func inputErrorFor(s PromotionInputSnapshot, name string, verdict enginesnapshot.CompletenessVerdict, detail string) error {
	availability := AvailabilityUnknown
	if in, ok := s.Lookup(name); ok {
		availability = in.Availability
		if in.Reason != "" {
			detail = in.Reason
		}
	}
	return &InputError{InputName: name, Verdict: verdict, Availability: availability, Detail: detail}
}

// builder accumulates the bound inputs as each domain read answers.
type builder struct {
	req     Request
	readers Readers
	inputs  map[string]Input
}

// add records one bound input, refusing a duplicate rather than overwriting
// it: two reads answering the same semantic input would mean the snapshot has
// two baselines for one fact.
func (b *builder) add(in Input) error {
	if b.inputs == nil {
		b.inputs = make(map[string]Input, len(inputOrder))
	}
	if _, dup := b.inputs[in.Name]; dup {
		return fmt.Errorf("promotion/snapshot: input %q was bound twice", in.Name)
	}
	b.inputs[in.Name] = in
	return nil
}

// ordered returns the bound inputs in declaration order, refusing a set that
// does not answer every declared input. Absence here is a programming error in
// this package, not a business fact: a read that found nothing still binds an
// ABSENT input.
func (b *builder) ordered() ([]Input, error) {
	out := make([]Input, 0, len(inputOrder))
	for _, name := range inputOrder {
		in, ok := b.inputs[name]
		if !ok {
			return nil, fmt.Errorf("promotion/snapshot: no read bound the declared input %q", name)
		}
		if err := in.Entry.Validate(); err != nil {
			return nil, fmt.Errorf("promotion/snapshot: input %q: %w", name, err)
		}
		out = append(out, in)
	}
	return out, nil
}

// entryFor builds the descriptor set for one input from the evidence its
// owning domain's read returned. Every descriptor is mandatory on an
// InputEntry, so a read that cannot supply one produces an entry that fails
// validation rather than an entry with a plausible default.
func (b *builder) entryFor(
	name string,
	authority evidence.SourceAuthority,
	provenance evidence.Provenance,
	revision values.RevisionToken,
	watermark values.RevisionToken,
	classification enginesnapshot.Classification,
	sourceSystem string,
) enginesnapshot.InputEntry {
	return enginesnapshot.InputEntry{
		Name:      name,
		Owner:     inputOwners[name],
		Tenant:    b.req.Tenant,
		Authority: authorityClassOf(authority.Kind),
		Source: enginesnapshot.SourceRef{
			System:     sourceSystem,
			Connection: b.req.SourceConnection,
			Ref:        name,
		},
		EffectiveAt:    b.req.EffectiveOn,
		KnownAt:        b.req.KnownAt,
		Revision:       revision,
		Head:           headOf(watermark),
		Watermark:      watermark,
		Freshness:      provenance.RecordedAt,
		Classification: classification,
		Provenance: enginesnapshot.Provenance{
			Source:      provenance.Source,
			EvidenceRef: provenance.EvidenceRef,
			RecordedAt:  provenance.RecordedAt,
		},
		ReferenceVersion: b.req.ReferenceVersion,
	}
}

// authorityClassOf maps the domains' evidence authority kind onto the snapshot
// engine's authority class. DERIVED becomes REFERENCE_CONFIG because a derived
// value's authority is the rule set that derived it, not a system of record;
// an unspecified kind maps to nothing, which fails entry validation rather
// than defaulting to native state.
func authorityClassOf(kind evidence.AuthorityKind) enginesnapshot.AuthorityClass {
	switch kind {
	case evidence.AuthorityLocal:
		return enginesnapshot.AuthorityNativeState
	case evidence.AuthorityExternalObservation:
		return enginesnapshot.AuthorityExternalObservation
	case evidence.AuthorityDerived:
		return enginesnapshot.AuthorityReferenceConfig
	default:
		return enginesnapshot.AuthorityUnspecified
	}
}

// headOf renders the source-declared head token for a watermark. The watermark
// is the read position the source itself published, so its canonical text is
// the head this entry was read at.
func headOf(watermark values.RevisionToken) string {
	if !watermark.IsSpecified() {
		return ""
	}
	return watermark.String()
}

// subjectWorker and subjectPosition are the material subjects the inputs are
// facts about.
func (b *builder) subjectWorker() intent.SubjectReference {
	return intent.SubjectReference{Kind: subjectKindWorker, SubjectID: b.req.Subject.Id, AuthorityDomain: authorityPeople}
}

func (b *builder) subjectPosition() intent.SubjectReference {
	return intent.SubjectReference{Kind: subjectKindPosition, SubjectID: b.req.TargetPosition.Id, AuthorityDomain: authorityPosition}
}

func (b *builder) subjectBudget() intent.SubjectReference {
	return intent.SubjectReference{Kind: subjectKindBudget, SubjectID: b.req.BudgetScope + "@" + b.req.BudgetPeriod, AuthorityDomain: authorityFinance}
}

// resourceKey addresses one input within the promotion slice. The segments are
// the owning domain and the input name, so two tenants' or two subjects' keys
// can never collide.
func (b *builder) resourceKey(name, subject string) (values.ResourceKey, error) {
	return values.NewResourceKey(b.req.Tenant, resourceKindPromoIn, inputOwners[name], name, subject)
}

// -------------------------------------------------------------------------
// People: subject worker facts and current placement
// -------------------------------------------------------------------------

// workerFactFields are the worker-state fields the two people inputs are
// projected from. The snapshot asks for exactly these and no more.
var workerFactFields = []people.FieldID{
	people.FieldLifecycleStatus,
	people.FieldEmploymentStatus,
	people.FieldHireDate,
	people.FieldJobCode,
	people.FieldGrade,
	people.FieldOrgUnit,
	people.FieldPositionID,
	people.FieldPayZone,
}

// subjectFactFields and placementFactFields split those between the two
// inputs: who the subject is, and where they sit.
var (
	subjectFactFields   = []people.FieldID{people.FieldLifecycleStatus, people.FieldEmploymentStatus, people.FieldHireDate}
	placementFactFields = []people.FieldID{people.FieldJobCode, people.FieldGrade, people.FieldOrgUnit, people.FieldPositionID, people.FieldPayZone}
)

// WorkerFactFields returns the exact worker projection a Promotion input
// snapshot reads, so a caller can build the authorization decision for it
// without guessing which fields will be asked for.
func WorkerFactFields() []people.FieldID {
	return append([]people.FieldID(nil), workerFactFields...)
}

// readWorker binds the two people inputs from one governed worker read.
//
// One read answers both because they are one consistent observation of one
// subject: splitting them into two reads would let the subject's status come
// from one watermark and their placement from another, which is exactly the
// inconsistency the snapshot exists to prevent.
func (b *builder) readWorker(ctx context.Context) error {
	explanation, err := people.ExplainWorkerState(ctx, b.readers.Worker, people.ExplainWorkerStateRequest{
		Tenant:        b.req.Tenant,
		Worker:        b.req.Subject,
		AsOf:          people.AsOf{EffectiveOn: b.req.EffectiveOn, KnownAt: b.req.KnownAt},
		Fields:        workerFactFields,
		Authorization: b.req.Authorization.Worker,
	})
	if err != nil {
		return fmt.Errorf("%w: worker state: %w", ErrReadFailed, err)
	}
	for _, spec := range []struct {
		name   string
		fields []people.FieldID
	}{
		{InputSubjectWorkerFacts, subjectFactFields},
		{InputCurrentPlacement, placementFactFields},
	} {
		in, err := b.peopleInput(spec.name, spec.fields, explanation)
		if err != nil {
			return err
		}
		if err := b.add(in); err != nil {
			return err
		}
	}
	return nil
}

// peopleInput projects one governed explanation onto one input.
//
// The three outcomes are kept apart deliberately. A WITHHELD subject makes the
// input WITHHELD with the policy's own reason. A subject the record does not
// carry makes it ABSENT. A subject that exists makes it DISCLOSED, unless any
// of its fields was denied or unresolved -- in which case the whole input is
// WITHHELD or UNKNOWN respectively, because a placement missing its pay zone
// is not a partially disclosed placement, it is an input the proposal cannot
// be built from.
func (b *builder) peopleInput(name string, fields []people.FieldID, e people.Explanation) (Input, error) {
	key, err := b.resourceKey(name, b.req.Subject.Id)
	if err != nil {
		return Input{}, err
	}
	in := Input{Name: name, Subject: b.subjectWorker(), ResourceKey: key}

	if e.Disclosure == people.DisclosureWithheld {
		in.Availability = AvailabilityWithheld
		in.Reason = e.WithheldReason
		in.Entry = b.unreadableEntry(name, enginesnapshot.ClassificationConfidential, sourceSystemPeople)
		return in, nil
	}

	authority, provenance, revision, ok := peopleEvidence(e, fields)
	if !ok {
		in.Availability = AvailabilityUnknown
		in.Reason = "the governed read disclosed no evidence for this input"
		in.Entry = b.unreadableEntry(name, enginesnapshot.ClassificationConfidential, sourceSystemPeople)
		return in, nil
	}
	in.Entry = b.entryFor(name, authority, provenance, revision, e.Watermark,
		enginesnapshot.ClassificationConfidential, sourceSystemPeople)

	if e.Presence == people.SubjectAbsent {
		in.Availability = AvailabilityAbsent
		in.Reason = "the governed read discloses no such worker at this coordinate"
		return in, nil
	}

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		fact, found := lookupFact(e, field)
		switch {
		case !found:
			in.Availability = AvailabilityUnknown
			in.Reason = "the governed read did not cover " + field.String()
			return in, nil
		case fact.Access == people.AccessDenied:
			in.Availability = AvailabilityWithheld
			in.Reason = fact.DenialReason
			return in, nil
		}
		value, present := fact.Value.Get()
		if !present {
			// The five non-value presences are not interchangeable. "The
			// record asserts nothing here" and "a value exists but this read
			// could not learn it" are different facts about the promotion, and
			// only the first is a business answer.
			in.Availability = availabilityForPresence(fact.Value.State())
			in.Reason = "the record disclosed " + fact.Value.State().String() + " for " + field.String()
			return in, nil
		}
		parts = append(parts, field.String()+"="+value)
	}
	in.Availability = AvailabilityDisclosed
	in.CanonicalText = strings.Join(parts, ";")
	return in, nil
}

// availabilityForPresence maps a kernel presence state onto what this snapshot
// may say about the input. REDACTED is a denial and therefore WITHHELD;
// UNKNOWN is an unestablished presence and stays UNKNOWN; ABSENT, NULL and
// NOT_APPLICABLE are all "the record answered and there is nothing here",
// which is a known absence.
func availabilityForPresence(state values.PresenceState) Availability {
	switch state {
	case values.PresenceRedacted:
		return AvailabilityWithheld
	case values.PresenceUnknown:
		return AvailabilityUnknown
	default:
		return AvailabilityAbsent
	}
}

// lookupFact returns the explained fact for a field.
func lookupFact(e people.Explanation, field people.FieldID) (people.ExplainedFact, bool) {
	for _, f := range e.Fields {
		if f.Field == field {
			return f, true
		}
	}
	return people.ExplainedFact{}, false
}

// peopleEvidence returns the authority, provenance and revision the input's
// own fields were read under, taking the first authorized field's coordinates:
// one governed read is one consistent observation, so its fields share them.
func peopleEvidence(e people.Explanation, fields []people.FieldID) (evidence.SourceAuthority, evidence.Provenance, values.RevisionToken, bool) {
	for _, field := range fields {
		fact, found := lookupFact(e, field)
		if !found || fact.Access != people.AccessAuthorized {
			continue
		}
		return fact.Authority, fact.Provenance, fact.Revision, true
	}
	for _, fact := range e.Fields {
		if fact.Access == people.AccessAuthorized {
			return fact.Authority, fact.Provenance, fact.Revision, true
		}
	}
	return evidence.SourceAuthority{}, evidence.Provenance{}, values.RevisionToken{}, false
}

// unreadableEntry is the descriptor set for an input no read could supply
// evidence for. Every mandatory descriptor is filled from the request itself
// -- the coordinate, the tenant, the reference version -- and the authority is
// the platform's own derived authority, because the assertion being made is
// "this system could not establish the input", which is a fact this system
// owns rather than one it observed somewhere else.
func (b *builder) unreadableEntry(name string, classification enginesnapshot.Classification, sourceSystem string) enginesnapshot.InputEntry {
	revision, err := values.NewSequenceRevision("promotion.snapshot."+strings.ReplaceAll(name, ".", "_"), 0)
	if err != nil {
		return enginesnapshot.InputEntry{}
	}
	recorded, err := values.NewRecordedAt(b.req.KnownAt.Instant())
	if err != nil {
		return enginesnapshot.InputEntry{}
	}
	return b.entryFor(name,
		evidence.SourceAuthority{Kind: evidence.AuthorityDerived, System: sourceSystem, PolicyRef: b.req.ReferenceVersion},
		evidence.Provenance{Source: sourceSystem, EvidenceRef: "evd_unreadable_" + name, RecordedAt: recorded},
		revision, revision, classification, sourceSystem)
}

// -------------------------------------------------------------------------
// Org: manager chain
// -------------------------------------------------------------------------

// readManagerChain binds the manager-chain input from ORG-002's resolution.
func (b *builder) readManagerChain(ctx context.Context) error {
	key, err := b.resourceKey(InputManagerChain, b.req.Subject.Id)
	if err != nil {
		return err
	}
	in := Input{Name: InputManagerChain, Subject: b.subjectWorker(), ResourceKey: key}

	resolution, err := org.ResolveManagerRelationships(ctx, b.readers.Org, org.ManagerResolutionRequest{
		Tenant:    b.req.Tenant,
		Worker:    b.req.Subject,
		AsOf:      b.req.asOfInstant(),
		KnownAt:   b.req.KnownAt,
		MaxDepth:  b.req.ManagerChainDepth,
		Authorize: b.req.Authorization.ManagerHop,
	})
	if err != nil {
		return fmt.Errorf("%w: manager chain: %w", ErrReadFailed, err)
	}

	switch {
	case resolution.Disclosure == people.DisclosureWithheld:
		in.Availability = AvailabilityWithheld
		in.Reason = "the caller is not authorized to learn this subject's manager chain"
		in.Entry = b.unreadableEntry(InputManagerChain, enginesnapshot.ClassificationConfidential, sourceSystemOrg)
	case resolution.Status == org.StatusStale || resolution.Status == org.StatusDisagreeing || resolution.Status == org.StatusAmbiguous:
		in.Availability = AvailabilityUnknown
		in.Reason = "the manager graph resolved " + string(resolution.Status)
		in.Entry = b.unreadableEntry(InputManagerChain, enginesnapshot.ClassificationConfidential, sourceSystemOrg)
	case resolution.Status == org.StatusVacant || resolution.Direct == nil:
		in.Availability = AvailabilityAbsent
		in.Reason = "the subject has no direct manager at this coordinate"
		in.Entry = b.unreadableEntry(InputManagerChain, enginesnapshot.ClassificationConfidential, sourceSystemOrg)
	default:
		hop := *resolution.Direct
		in.Entry = b.entryFor(InputManagerChain, hop.Authority, hop.Provenance, hop.Revision, resolution.Watermark,
			enginesnapshot.ClassificationConfidential, sourceSystemOrg)
		if hop.Manager.Access != people.AccessAuthorized {
			in.Availability = AvailabilityWithheld
			in.Reason = hop.Manager.DenialReason
			break
		}
		in.Availability = AvailabilityDisclosed
		in.CanonicalText = managerChainText(resolution)
	}
	return b.add(in)
}

// managerChainText renders the disclosed chain: one hop per level, each naming
// the relationship, its type and the manager it points at. Only authorized
// hops contribute; a withheld hop contributes its level and nothing else, so
// the depth of the chain is visible without the identity being disclosed.
func managerChainText(r org.ManagerResolution) string {
	hops := append([]org.ManagerHop(nil), r.Chain...)
	sort.SliceStable(hops, func(i, j int) bool { return hops[i].Level < hops[j].Level })
	parts := make([]string, 0, len(hops))
	for _, hop := range hops {
		if hop.Manager.Access != people.AccessAuthorized {
			parts = append(parts, fmt.Sprintf("%d:WITHHELD", hop.Level))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d:%s:%s:%s", hop.Level, hop.RelationshipID, hop.Type, hop.Manager.Value.Id))
	}
	return strings.Join(parts, ";")
}

// -------------------------------------------------------------------------
// Position: capacity and vacancy
// -------------------------------------------------------------------------

// readPosition binds the two position inputs from one POSITION-002 capacity
// calculation, for the same reason the two people inputs come from one read.
func (b *builder) readPosition(ctx context.Context) error {
	capacityKey, err := b.resourceKey(InputTargetPositionCapacity, b.req.TargetPosition.Id)
	if err != nil {
		return err
	}
	vacancyKey, err := b.resourceKey(InputTargetPositionVacancy, b.req.TargetPosition.Id)
	if err != nil {
		return err
	}
	capacityIn := Input{Name: InputTargetPositionCapacity, Subject: b.subjectPosition(), ResourceKey: capacityKey}
	vacancyIn := Input{Name: InputTargetPositionVacancy, Subject: b.subjectPosition(), ResourceKey: vacancyKey}

	result, err := position.CalculateCapacity(ctx, b.readers.Position, position.CapacityRequest{
		Tenant:    b.req.Tenant,
		Position:  b.req.TargetPosition,
		AsOf:      position.AsOf{EffectiveOn: b.req.EffectiveOn, KnownAt: b.req.KnownAt},
		Occupants: b.req.Occupants,
		Pending:   b.req.Pending,
		Authorize: b.req.Authorization.Position,
	})
	switch {
	case errors.Is(err, position.ErrUnauthorized):
		capacityIn.Availability, capacityIn.Reason = AvailabilityWithheld, "the caller is not authorized to see the target position"
		vacancyIn.Availability, vacancyIn.Reason = AvailabilityWithheld, "the caller is not authorized to see the target position"
		capacityIn.Entry = b.unreadableEntry(InputTargetPositionCapacity, enginesnapshot.ClassificationInternal, sourceSystemPosition)
		vacancyIn.Entry = b.unreadableEntry(InputTargetPositionVacancy, enginesnapshot.ClassificationInternal, sourceSystemPosition)
		return b.addBoth(capacityIn, vacancyIn)
	case err != nil:
		return fmt.Errorf("%w: position capacity: %w", ErrReadFailed, err)
	}

	if !result.Exists {
		capacityIn.Availability, capacityIn.Reason = AvailabilityAbsent, "no position revision at this coordinate"
		vacancyIn.Availability, vacancyIn.Reason = AvailabilityAbsent, "no position revision at this coordinate"
		capacityIn.Entry = b.unreadableEntry(InputTargetPositionCapacity, enginesnapshot.ClassificationInternal, sourceSystemPosition)
		vacancyIn.Entry = b.unreadableEntry(InputTargetPositionVacancy, enginesnapshot.ClassificationInternal, sourceSystemPosition)
		return b.addBoth(capacityIn, vacancyIn)
	}

	// The capacity result carries the revision it computed against but not the
	// authority and provenance behind it, so the same revision is re-read for
	// its evidence coordinates. It is the same query CalculateCapacity itself
	// issued, at the same coordinate, so it cannot answer a different
	// revision without the resolve below refusing the mismatch.
	revision, revisionExists, err := b.readers.Position.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant:   b.req.Tenant,
		Position: b.req.TargetPosition,
		AsOf:     position.AsOf{EffectiveOn: b.req.EffectiveOn, KnownAt: b.req.KnownAt},
	})
	if err != nil {
		return fmt.Errorf("%w: position revision: %w", ErrReadFailed, err)
	}
	if !revisionExists {
		return fmt.Errorf("%w: position capacity reported a revision the position read no longer holds", ErrReadFailed)
	}
	capacityIn.Entry = b.entryFor(InputTargetPositionCapacity, revision.Authority, revision.Provenance,
		result.Revision, result.Revision, enginesnapshot.ClassificationInternal, sourceSystemPosition)
	vacancyIn.Entry = b.entryFor(InputTargetPositionVacancy, revision.Authority, revision.Provenance,
		result.Revision, result.Revision, enginesnapshot.ClassificationInternal, sourceSystemPosition)

	capacityIn.Availability = AvailabilityDisclosed
	capacityIn.CanonicalText = CapacityAvailable
	if result.AvailableHeads <= 0 {
		capacityIn.CanonicalText = CapacityExhausted
	}

	if result.HasVacantAfter {
		vacancyIn.Availability = AvailabilityDisclosed
		vacancyIn.CanonicalText = result.VacantAfter.String()
	} else {
		vacancyIn.Availability = AvailabilityAbsent
		vacancyIn.Reason = "no date is known on which the position sits vacant with no successor"
	}
	return b.addBoth(capacityIn, vacancyIn)
}

// addBoth records two inputs, stopping at the first refusal.
func (b *builder) addBoth(first, second Input) error {
	if err := b.add(first); err != nil {
		return err
	}
	return b.add(second)
}

// -------------------------------------------------------------------------
// Rewards: pay band position of current and desired pay
// -------------------------------------------------------------------------

// readCompensation binds the two band-position inputs.
//
// Both are evaluated against the same band, resolved once for the target job,
// grade and pay zone: comparing current pay against the band it is leaving and
// desired pay against the band it is entering would answer a question nobody
// asked. Current pay comes from COMP-001's authorized read, desired pay from
// the request; both are annualized through COMP-002's declared rule set before
// COMP-003 places them.
func (b *builder) readCompensation(ctx context.Context) error {
	currentKey, err := b.resourceKey(InputPayBandPositionCurrent, b.req.Subject.Id)
	if err != nil {
		return err
	}
	desiredKey, err := b.resourceKey(InputPayBandPositionDesired, b.req.Subject.Id)
	if err != nil {
		return err
	}
	currentIn := Input{Name: InputPayBandPositionCurrent, Subject: b.subjectWorker(), ResourceKey: currentKey}
	desiredIn := Input{Name: InputPayBandPositionDesired, Subject: b.subjectWorker(), ResourceKey: desiredKey}

	read, err := rewards.ReadAuthorizedCompensation(ctx, b.readers.Compensation, rewards.CompensationReadRequest{
		Tenant:        b.req.Tenant,
		Worker:        b.req.Subject,
		AsOf:          b.req.asOfInstant(),
		Authorization: b.req.Authorization.Compensation,
	})
	if err != nil {
		return fmt.Errorf("%w: compensation: %w", ErrReadFailed, err)
	}

	band, bandErr := b.readers.Bands.LookupBand(ctx, rewards.BandQuery{
		Tenant:   b.req.Tenant,
		JobCode:  b.req.Target.JobCode,
		Grade:    b.req.Target.Grade,
		PayZone:  b.req.Target.PayZone,
		Currency: b.req.DesiredBasePay.Currency(),
		AsOf:     b.req.EffectiveOn,
	})
	switch {
	case errors.Is(bandErr, rewards.ErrBandNotFound):
		currentIn.Availability, currentIn.Reason = AvailabilityAbsent, "the catalog publishes no band for the target placement"
		desiredIn.Availability, desiredIn.Reason = AvailabilityAbsent, "the catalog publishes no band for the target placement"
		currentIn.Entry = b.unreadableEntry(InputPayBandPositionCurrent, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
		desiredIn.Entry = b.unreadableEntry(InputPayBandPositionDesired, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
		return b.addBoth(currentIn, desiredIn)
	case bandErr != nil:
		return fmt.Errorf("%w: pay band catalog: %w", ErrReadFailed, bandErr)
	}

	// The desired band position depends on the band and on the request, never
	// on the subject's own compensation record: its evidence is the catalog's.
	// The current band position depends on both, so it is bound to the
	// compensation read's own revision and watermark -- and falls back to an
	// unreadable entry when that read had no evidence to give.
	bandRevision, err := values.NewSequenceRevision("rewards.band."+band.Band.ID+"@"+band.Band.Version, 1)
	if err != nil {
		return fmt.Errorf("%w: band revision: %w", ErrReadFailed, err)
	}
	desiredIn.Entry = b.entryFor(InputPayBandPositionDesired, band.Authority, band.Provenance,
		bandRevision, bandRevision, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
	if read.Revision.IsSpecified() && read.Watermark.IsSpecified() && read.Authority.Validate() == nil && read.Provenance.Validate() == nil {
		currentIn.Entry = b.entryFor(InputPayBandPositionCurrent, read.Authority, read.Provenance,
			read.Revision, read.Watermark, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
	} else {
		currentIn.Entry = b.unreadableEntry(InputPayBandPositionCurrent, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
	}

	if read.Disclosure == people.DisclosureWithheld {
		currentIn.Availability, currentIn.Reason = AvailabilityWithheld, read.WithheldReason
		desiredIn.Availability, desiredIn.Reason = AvailabilityWithheld, read.WithheldReason
		currentIn.Entry = b.unreadableEntry(InputPayBandPositionCurrent, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
		desiredIn.Entry = b.unreadableEntry(InputPayBandPositionDesired, enginesnapshot.ClassificationRestricted, sourceSystemRewards)
		return b.addBoth(currentIn, desiredIn)
	}

	basis := b.req.DesiredPayBasis
	if declared, ok := read.PayBasis.Value.Get(); ok {
		if parsed, parsedOK := payBasisFromToken(declared); parsedOK {
			basis = parsed
		}
	}

	switch {
	case read.Presence == people.SubjectAbsent:
		currentIn.Availability, currentIn.Reason = AvailabilityAbsent, "the record holds no compensation for this worker"
	case read.BasePay.Access == people.AccessDenied:
		currentIn.Availability, currentIn.Reason = AvailabilityWithheld, read.BasePay.DenialReason
	default:
		amount, present := read.BasePay.Value.Get()
		if !present {
			currentIn.Availability = availabilityForPresence(read.BasePay.Value.State())
			currentIn.Reason = "the compensation read disclosed " + read.BasePay.Value.State().String() + " for base pay"
			break
		}
		text, err := b.bandPositionText(band.Band, amount, basis)
		if err != nil {
			return err
		}
		currentIn.Availability, currentIn.CanonicalText = AvailabilityDisclosed, text
	}

	desiredText, err := b.bandPositionText(band.Band, b.req.DesiredBasePay, b.req.DesiredPayBasis)
	if err != nil {
		return err
	}
	desiredIn.Availability, desiredIn.CanonicalText = AvailabilityDisclosed, desiredText

	return b.addBoth(currentIn, desiredIn)
}

// payBasisFromToken parses the compensation read's own pay-basis token.
func payBasisFromToken(token string) (rewards.PayBasis, bool) {
	for _, basis := range []rewards.PayBasis{
		rewards.PayBasisHourly, rewards.PayBasisDaily, rewards.PayBasisMonthlySalary,
		rewards.PayBasisAnnualSalary, rewards.PayBasisPieceRate,
	} {
		if basis.String() == token {
			return basis, true
		}
	}
	return rewards.PayBasisUnspecified, false
}

// bandPositionText annualizes one amount and renders where it sits in the
// band. A band position the caller was not authorized to see renders as its
// disclosure state and its band identity, never as a ratio.
func (b *builder) bandPositionText(band payband.Band, amount values.Money, basis rewards.PayBasis) (string, error) {
	annualized, err := rewards.AnnualizeCompensation(
		rewards.CompensationPackage{Amount: amount, Basis: basis},
		b.req.Annualization)
	if err != nil {
		return "", fmt.Errorf("%w: annualization: %w", ErrReadFailed, err)
	}
	placement, err := rewards.EvaluateCompensationPayBandPosition(rewards.PayBandPositionRequest{
		Band:          band,
		Annualized:    annualized.Annualized,
		Authorization: b.req.Authorization.PayBand,
	})
	if err != nil {
		return "", fmt.Errorf("%w: pay band position: %w", ErrReadFailed, err)
	}
	parts := []string{
		"band=" + placement.BandID + "@" + placement.BandVersion,
		"annualized=" + annualized.Annualized.String(),
		"class=" + placement.Class.String(),
		"boundary=" + placement.Boundary.String(),
	}
	if compa, ok := placement.CompaRatioField.Value.Get(); ok {
		parts = append(parts, "compa_ratio="+compa.String())
	} else {
		parts = append(parts, "compa_ratio="+placement.CompaRatioField.Value.State().String())
	}
	if penetration, ok := placement.RangePenetrationField.Value.Get(); ok {
		parts = append(parts, "range_penetration="+penetration.String())
	} else {
		parts = append(parts, "range_penetration="+placement.RangePenetrationField.Value.State().String())
	}
	return strings.Join(parts, ";"), nil
}

// -------------------------------------------------------------------------
// Budget: compensation pool availability
// -------------------------------------------------------------------------

// readBudget binds the budget-availability input from the pool observation.
func (b *builder) readBudget(ctx context.Context) error {
	key, err := b.resourceKey(InputBudgetAvailability, b.req.BudgetScope)
	if err != nil {
		return err
	}
	in := Input{Name: InputBudgetAvailability, Subject: b.subjectBudget(), ResourceKey: key}

	if !b.req.Authorization.BudgetDisclosable {
		in.Availability, in.Reason = AvailabilityWithheld, b.req.Authorization.BudgetDenialReason
		in.Entry = b.unreadableEntry(InputBudgetAvailability, enginesnapshot.ClassificationConfidential, sourceSystemBudget)
		return b.add(in)
	}

	ref, exists, err := b.readers.Budget.CompensationBudgetAt(ctx, BudgetQuery{
		Tenant: b.req.Tenant,
		Scope:  b.req.BudgetScope,
		Period: b.req.BudgetPeriod,
		AsOf:   b.req.asOfInstant(),
	})
	if err != nil {
		return fmt.Errorf("%w: budget observation: %w", ErrReadFailed, err)
	}
	if !exists {
		in.Availability, in.Reason = AvailabilityAbsent, "no compensation pool observation for this scope and period"
		in.Entry = b.unreadableEntry(InputBudgetAvailability, enginesnapshot.ClassificationConfidential, sourceSystemBudget)
		return b.add(in)
	}
	if err := ref.Validate(); err != nil {
		in.Availability, in.Reason = AvailabilityUnknown, "the pool observation is not citable"
		in.Entry = b.unreadableEntry(InputBudgetAvailability, enginesnapshot.ClassificationConfidential, sourceSystemBudget)
		return b.add(in)
	}

	revision, err := values.NewSequenceRevision("budget.pool."+b.req.BudgetScope+"."+b.req.BudgetPeriod, 1)
	if err != nil {
		return fmt.Errorf("%w: budget revision: %w", ErrReadFailed, err)
	}
	recorded, err := values.NewRecordedAt(ref.Evidence.SourceWatermark)
	if err != nil {
		return fmt.Errorf("%w: budget observation watermark: %w", ErrReadFailed, err)
	}
	in.Entry = b.entryFor(InputBudgetAvailability,
		evidence.SourceAuthority{Kind: evidence.AuthorityExternalObservation, System: ref.OwnerSystem, PolicyRef: ref.BaselineVersion},
		evidence.Provenance{Source: ref.OwnerSystem, EvidenceRef: ref.Evidence.ObservationID, RecordedAt: recorded},
		revision, revision, enginesnapshot.ClassificationConfidential, sourceSystemBudget)
	in.Availability = AvailabilityDisclosed
	in.CanonicalText = budgetText(ref)
	return b.add(in)
}

// budgetText renders the observation exactly: the pool it names, the unit it
// is measured in and the quantity it reported.
func budgetText(ref budget.BudgetAuthorityRef) string {
	parts := []string{
		"type=" + string(ref.BudgetType),
		"scope=" + ref.Scope,
		"period=" + ref.Period,
		"unit=" + string(ref.Unit),
		"available=" + ref.AvailableQuantity.String(),
		"baseline=" + ref.BaselineVersion,
	}
	if ref.Currency != "" {
		parts = append(parts, "currency="+ref.Currency)
	}
	return strings.Join(parts, ";")
}
