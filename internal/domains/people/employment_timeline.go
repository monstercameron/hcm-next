package people

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	EmploymentTimelineIntentType    = "hcmnext.people.employment.timeline.read"
	EmploymentTimelineIntentVersion = "v1"
	EmploymentTimelineRulePack      = "people.employment.timeline/2026.1"
	employmentTimelineSchema        = "hcmnext.domains.people.EmploymentTimeline"
)

var (
	ErrEmploymentTimelineInvalid = errors.New("people: invalid employment timeline")
	ErrEmploymentPeriodInvalid   = errors.New("people: invalid employment period")
	ErrFutureKnownPeriod         = errors.New("people: employment period is known after the requested cutoff")
)

// EmploymentTimelineFields is the closed projection owned by the employment
// timeline read. A caller cannot widen it into Person, Assignment,
// compensation, identity-resolution, medical, immigration or payroll fields.
var employmentTimelineFields = []FieldID{
	FieldEmploymentID,
	FieldLegalEntity,
	FieldWorkerType,
	FieldHireDate,
	FieldEmploymentStatus,
}

// EmploymentTimelineFields returns a copy of the fixed employment projection.
func EmploymentTimelineFields() []FieldID {
	return append([]FieldID(nil), employmentTimelineFields...)
}

// EmploymentPeriodKind is the typed lifecycle meaning of an employment
// period. WorkerStatus is deliberately not used here: it is a derived
// projection and cannot replace the legal relationship's asserted state.
type EmploymentPeriodKind string

const (
	EmploymentPeriodProposed   EmploymentPeriodKind = "PROPOSED"
	EmploymentPeriodActive     EmploymentPeriodKind = "ACTIVE"
	EmploymentPeriodSuspended  EmploymentPeriodKind = "SUSPENDED"
	EmploymentPeriodLeave      EmploymentPeriodKind = "LEAVE"
	EmploymentPeriodEnded      EmploymentPeriodKind = "ENDED"
	EmploymentPeriodReinstated EmploymentPeriodKind = "REINSTATED"
)

// Short aliases make the lifecycle vocabulary convenient without introducing
// a second set of wire values.
const (
	PeriodKindProposed   = EmploymentPeriodProposed
	PeriodKindActive     = EmploymentPeriodActive
	PeriodKindSuspended  = EmploymentPeriodSuspended
	PeriodKindLeave      = EmploymentPeriodLeave
	PeriodKindEnded      = EmploymentPeriodEnded
	PeriodKindReinstated = EmploymentPeriodReinstated
)

var employmentPeriodKinds = map[EmploymentPeriodKind]struct{}{
	EmploymentPeriodProposed: {}, EmploymentPeriodActive: {},
	EmploymentPeriodSuspended: {}, EmploymentPeriodLeave: {},
	EmploymentPeriodEnded: {}, EmploymentPeriodReinstated: {},
}

// Valid reports whether k is a declared employment lifecycle kind.
func (k EmploymentPeriodKind) Valid() bool {
	_, ok := employmentPeriodKinds[k]
	return ok
}

func (k EmploymentPeriodKind) String() string { return string(k) }

// EmploymentTimelineQuery is the fixed-mask query sent to the employment
// timeline reader. Fields is a subset only because field authorization may
// reduce the mask; ReadEmploymentTimeline never adds caller-supplied fields.
type EmploymentTimelineQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   AsOf
	Fields []FieldID
}

func (q EmploymentTimelineQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrEmploymentTimelineInvalid, err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrEmploymentTimelineInvalid, err)
	}
	if q.Worker.Tenant != q.Tenant || q.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: worker must be a worker in the requested tenant", ErrEmploymentTimelineInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %w", ErrEmploymentTimelineInvalid, err)
	}
	if len(q.Fields) == 0 {
		return fmt.Errorf("%w: empty fixed field mask", ErrEmploymentTimelineInvalid)
	}
	allowed := make(map[FieldID]struct{}, len(employmentTimelineFields))
	for _, field := range employmentTimelineFields {
		allowed[field] = struct{}{}
	}
	seen := make(map[FieldID]struct{}, len(q.Fields))
	for _, field := range q.Fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%w: field %s is outside the employment mask", ErrEmploymentTimelineInvalid, field)
		}
		if _, duplicate := seen[field]; duplicate {
			return fmt.Errorf("%w: duplicate field %s", ErrEmploymentTimelineInvalid, field)
		}
		seen[field] = struct{}{}
	}
	return nil
}

// EmploymentPeriodFact is one authoritative or observed employment
// assertion. PeriodID distinguishes revisions of the same legal relationship
// so a retro correction is retained rather than replacing history.
type EmploymentPeriodFact struct {
	PeriodID     string
	EmploymentID string
	LegalEntity  string
	WorkerType   string
	HireDate     string
	Status       string
	Kind         EmploymentPeriodKind

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

func (p EmploymentPeriodFact) Validate() error {
	if p.PeriodID == "" || p.EmploymentID == "" {
		return fmt.Errorf("%w: period and employment ids are required", ErrEmploymentPeriodInvalid)
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: unknown kind %q", ErrEmploymentPeriodInvalid, p.Kind)
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %w", ErrEmploymentPeriodInvalid, err)
	}
	if p.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrEmploymentPeriodInvalid)
	}
	if p.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: known-at is required", ErrEmploymentPeriodInvalid)
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrEmploymentPeriodInvalid)
	}
	if err := p.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %w", ErrEmploymentPeriodInvalid, err)
	}
	if err := p.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %w", ErrEmploymentPeriodInvalid, err)
	}
	return values.ValidateKnowledgeOrder(p.KnownAt, p.Provenance.RecordedAt, false)
}

// EmploymentTimelineFactSet is the bitemporal response from a repository.
type EmploymentTimelineFactSet struct {
	Worker    values.EntityRef
	Exists    bool
	Periods   []EmploymentPeriodFact
	Watermark values.RevisionToken
}

func (s EmploymentTimelineFactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrEmploymentTimelineInvalid, err)
	}
	if s.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: fact-set subject is not a worker", ErrEmploymentTimelineInvalid)
	}
	if !s.Exists {
		if len(s.Periods) != 0 {
			return fmt.Errorf("%w: absent worker carries periods", ErrEmploymentTimelineInvalid)
		}
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: existing worker has no read watermark", ErrEmploymentTimelineInvalid)
	}
	seen := make(map[string]struct{}, len(s.Periods))
	for _, period := range s.Periods {
		if err := period.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[period.PeriodID]; duplicate {
			return fmt.Errorf("%w: duplicate period %s", ErrEmploymentTimelineInvalid, period.PeriodID)
		}
		seen[period.PeriodID] = struct{}{}
	}
	return nil
}

// EmploymentTimelineFacts is the storage-neutral port used by the closed
// timeline specialization. The reader owns selection at effective/known
// coordinates; the domain validates that it did not return future knowledge.
type EmploymentTimelineFacts interface {
	EmploymentTimelineAt(context.Context, EmploymentTimelineQuery) (EmploymentTimelineFactSet, error)
}

// EmploymentPeriod is a disclosed timeline period. The period kind and
// bitemporal evidence are always typed; field values remain field-level
// ExplainedFacts so partial authorization cannot turn a denial into a blank
// authoritative value.
type EmploymentPeriod struct {
	PeriodID string
	Kind     EmploymentPeriodKind

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance

	Fields []ExplainedFact
}

func (p EmploymentPeriod) Value(field FieldID) (string, bool) {
	for _, fact := range p.Fields {
		if fact.Field == field && fact.Access == AccessAuthorized {
			return fact.Value.Get()
		}
	}
	return "", false
}

// EmploymentGap is a finite half-open gap between adjacent covered periods.
type EmploymentGap struct {
	From           values.LocalDate
	To             values.LocalDate
	BeforePeriodID string
	AfterPeriodID  string
}

// EmploymentOverlap is a typed overlap finding. Open-ended overlap has no
// upper date and is represented by HasEnd=false.
type EmploymentOverlap struct {
	From           values.LocalDate
	To             values.LocalDate
	HasEnd         bool
	FirstPeriodID  string
	SecondPeriodID string
}

type EmploymentFindingCode string

const (
	EmploymentFindingGap     EmploymentFindingCode = "GAP"
	EmploymentFindingOverlap EmploymentFindingCode = "OVERLAP"
)

type EmploymentTimelineFinding struct {
	Code           EmploymentFindingCode
	From           values.LocalDate
	To             values.LocalDate
	HasEnd         bool
	FirstPeriodID  string
	SecondPeriodID string
}

// EmploymentTimeline is the closed, zero-effect result of the employment
// timeline read. Periods remain independent legal relationships and
// revisions; no WorkerStatus is synthesized as an authoritative replacement.
type EmploymentTimeline struct {
	Worker         values.EntityRef
	Disclosure     Disclosure
	Presence       SubjectPresence
	WithheldReason string
	AsOf           AsOf

	Periods  []EmploymentPeriod
	Gaps     []EmploymentGap
	Overlaps []EmploymentOverlap
	Findings []EmploymentTimelineFinding

	Watermark     values.RevisionToken
	PolicyVersion string
	InputsDigest  string
	ResultDigest  string
	Effects       evidence.EffectCounters
	Receipt       evidence.ZeroEffectReceipt
}

func (p EmploymentPeriod) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.people.EmploymentPeriod", peopleSchemaVer).
		String("period_id", p.PeriodID).
		String("kind", p.Kind.String()).
		Value("effective", p.Effective).
		Value("known_at", p.KnownAt).
		Value("revision", p.Revision).
		Value("authority", p.Authority).
		Value("provenance", p.Provenance).
		Count("fields", len(p.Fields))
	for _, field := range p.Fields {
		w.Field("field", field.Canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (t EmploymentTimeline) canonicalBody() []byte {
	w := canonicalbytes.New(employmentTimelineSchema, peopleSchemaVer).
		Value("worker", t.Worker).
		String("disclosure", t.Disclosure.String()).
		String("presence", t.Presence.String()).
		String("withheld_reason", t.WithheldReason).
		Value("as_of", t.AsOf).
		Count("periods", len(t.Periods))
	for _, period := range t.Periods {
		w.Field("period", period.canonical())
	}
	w.Count("gaps", len(t.Gaps))
	for _, gap := range t.Gaps {
		w.Value("gap.from", gap.From).Value("gap.to", gap.To).
			String("gap.before", gap.BeforePeriodID).String("gap.after", gap.AfterPeriodID)
	}
	w.Count("overlaps", len(t.Overlaps))
	for _, overlap := range t.Overlaps {
		w.Value("overlap.from", overlap.From).
			Bool("overlap.has_end", overlap.HasEnd).
			String("overlap.first", overlap.FirstPeriodID).
			String("overlap.second", overlap.SecondPeriodID)
		if overlap.HasEnd {
			w.Value("overlap.to", overlap.To)
		}
	}
	w.Count("findings", len(t.Findings))
	for _, finding := range t.Findings {
		w.String("finding.code", string(finding.Code)).
			Value("finding.from", finding.From).
			Bool("finding.has_end", finding.HasEnd).
			String("finding.first", finding.FirstPeriodID).
			String("finding.second", finding.SecondPeriodID)
		if finding.HasEnd {
			w.Value("finding.to", finding.To)
		}
	}
	w.Bool("watermark?", t.Watermark.IsSpecified())
	if t.Watermark.IsSpecified() {
		w.Value("watermark", t.Watermark)
	}
	raw, err := w.String("policy_version", t.PolicyVersion).
		String("inputs_digest", t.InputsDigest).
		Value("effects", t.Effects).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Canonical returns a stable result encoding, or nil when a nested result
// value cannot be encoded.
func (t EmploymentTimeline) Canonical() []byte {
	body := t.canonicalBody()
	if len(body) == 0 {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.people.EmploymentTimelineEnvelope", peopleSchemaVer).
		Field("body", body).
		Field("receipt", t.Receipt.Canonical()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r EmploymentTimelineQuery) inputDigest(auth AuthorizationDecision) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.people.EmploymentTimelineQuery", peopleSchemaVer).
		String("intent_type", EmploymentTimelineIntentType).
		String("intent_version", EmploymentTimelineIntentVersion).
		String("tenant", string(r.Tenant)).
		Value("worker", r.Worker).
		Value("as_of", r.AsOf).
		Count("fields", len(employmentTimelineFields))
	for _, field := range employmentTimelineFields {
		w.String("field", string(field))
	}
	return w.Value("authorization", auth).Digest()
}

func timelineField(f EmploymentPeriodFact, field FieldID) string {
	switch field {
	case FieldEmploymentID:
		return f.EmploymentID
	case FieldLegalEntity:
		return f.LegalEntity
	case FieldWorkerType:
		return f.WorkerType
	case FieldHireDate:
		return f.HireDate
	case FieldEmploymentStatus:
		return f.Status
	default:
		return ""
	}
}

func explainEmploymentField(f EmploymentPeriodFact, field FieldID, ruling FieldRuling) ExplainedFact {
	if ruling.Effect == EffectDeny {
		return ExplainedFact{Field: field, Access: AccessDenied, DenialReason: ruling.Reason, Value: values.Redacted[string](ruling.Reason)}
	}
	value := values.Absent[string]()
	if raw := timelineField(f, field); raw != "" {
		value = values.Value(raw)
	}
	return ExplainedFact{
		Field: field, Access: AccessAuthorized, Value: value,
		Effective: f.Effective, KnownAt: f.KnownAt, Revision: f.Revision,
		Authority: f.Authority, Provenance: f.Provenance,
	}
}

func periodStart(p EmploymentPeriodFact) values.LocalDate {
	d, _ := p.Effective.StartDate()
	return d
}

func lessEmploymentPeriod(a, b EmploymentPeriodFact) bool {
	if cmp := periodStart(a).Compare(periodStart(b)); cmp != 0 {
		return cmp < 0
	}
	if cmp := a.KnownAt.Instant().Compare(b.KnownAt.Instant()); cmp != 0 {
		return cmp < 0
	}
	return a.PeriodID < b.PeriodID
}

func dateRange(p EmploymentPeriodFact) (values.LocalDate, values.LocalDate, bool) {
	start, _ := p.Effective.StartDate()
	end, hasEnd := p.Effective.EndDate()
	return start, end, hasEnd
}

func overlapOf(a, b EmploymentPeriodFact) (EmploymentOverlap, bool) {
	aStart, aEnd, aHasEnd := dateRange(a)
	bStart, bEnd, bHasEnd := dateRange(b)
	from := aStart
	if bStart.Compare(from) > 0 {
		from = bStart
	}
	to := aEnd
	hasEnd := aHasEnd
	if !bHasEnd {
		if !aHasEnd {
			hasEnd = false
		}
	} else if !aHasEnd || bEnd.Compare(to) < 0 {
		to, hasEnd = bEnd, true
	}
	if hasEnd && from.Compare(to) >= 0 {
		return EmploymentOverlap{}, false
	}
	return EmploymentOverlap{From: from, To: to, HasEnd: hasEnd, FirstPeriodID: a.PeriodID, SecondPeriodID: b.PeriodID}, true
}

func timelineShape(periods []EmploymentPeriodFact) ([]EmploymentGap, []EmploymentOverlap, []EmploymentTimelineFinding) {
	if len(periods) == 0 {
		return nil, nil, nil
	}
	ordered := append([]EmploymentPeriodFact(nil), periods...)
	sort.Slice(ordered, func(i, j int) bool { return lessEmploymentPeriod(ordered[i], ordered[j]) })

	var gaps []EmploymentGap
	var overlaps []EmploymentOverlap
	var findings []EmploymentTimelineFinding
	_, furthestEnd, furthestHasEnd := dateRange(ordered[0])
	furthestID := ordered[0].PeriodID
	for i := 1; i < len(ordered); i++ {
		start, end, hasEnd := dateRange(ordered[i])
		if furthestHasEnd && start.Compare(furthestEnd) > 0 {
			gap := EmploymentGap{From: furthestEnd, To: start, BeforePeriodID: furthestID, AfterPeriodID: ordered[i].PeriodID}
			gaps = append(gaps, gap)
			findings = append(findings, EmploymentTimelineFinding{Code: EmploymentFindingGap, From: gap.From, To: gap.To, HasEnd: true, FirstPeriodID: gap.BeforePeriodID, SecondPeriodID: gap.AfterPeriodID})
		}
		if overlap, ok := overlapOf(ordered[i-1], ordered[i]); ok {
			overlaps = append(overlaps, overlap)
			findings = append(findings, EmploymentTimelineFinding{Code: EmploymentFindingOverlap, From: overlap.From, To: overlap.To, HasEnd: overlap.HasEnd, FirstPeriodID: overlap.FirstPeriodID, SecondPeriodID: overlap.SecondPeriodID})
		}
		if !furthestHasEnd || (hasEnd && end.Compare(furthestEnd) > 0) {
			furthestEnd, furthestHasEnd, furthestID = end, hasEnd, ordered[i].PeriodID
		}
	}
	// A period can overlap an earlier long-lived period without overlapping its
	// immediate predecessor, so retain the complete pairwise overlap set.
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			hasOverlap, err := ordered[j].Effective.Overlaps(ordered[i].Effective)
			if err != nil || !hasOverlap {
				continue
			}
			overlap, ok := overlapOf(ordered[i], ordered[j])
			if !ok {
				continue
			}
			duplicate := false
			for _, existing := range overlaps {
				if existing.FirstPeriodID == overlap.FirstPeriodID && existing.SecondPeriodID == overlap.SecondPeriodID {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			overlaps = append(overlaps, overlap)
			findings = append(findings, EmploymentTimelineFinding{Code: EmploymentFindingOverlap, From: overlap.From, To: overlap.To, HasEnd: overlap.HasEnd, FirstPeriodID: overlap.FirstPeriodID, SecondPeriodID: overlap.SecondPeriodID})
		}
	}
	return gaps, overlaps, findings
}

func finishEmploymentTimeline(t EmploymentTimeline) (EmploymentTimeline, error) {
	body := t.canonicalBody()
	if len(body) == 0 {
		return EmploymentTimeline{}, fmt.Errorf("%w: result could not be canonically encoded", ErrEmploymentTimelineInvalid)
	}
	t.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		EmploymentTimelineIntentType, EmploymentTimelineIntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		[]evidence.ControlVersion{{Name: "authorization_policy", Version: t.PolicyVersion}, {Name: "employment_timeline_rule_pack", Version: EmploymentTimelineRulePack}},
		t.InputsDigest, t.ResultDigest, t.Effects,
	)
	if err != nil {
		return EmploymentTimeline{}, err
	}
	t.Receipt = receipt
	return t, nil
}

// ReadEmploymentTimeline is the PEOPLE-002 closed specialization. It reads
// only the employment mask, keeps independent periods/revisions, and derives
// gap/overlap findings without mutating domain state.
func ReadEmploymentTimeline(ctx context.Context, reader EmploymentTimelineFacts, req PersonWorkerReadRequest) (EmploymentTimeline, error) {
	if reader == nil {
		return EmploymentTimeline{}, fmt.Errorf("%w: no employment timeline reader", ErrEmploymentTimelineInvalid)
	}
	if err := req.Validate(); err != nil {
		return EmploymentTimeline{}, err
	}
	fields := EmploymentTimelineFields()
	if err := req.Authorization.Covers(fields); err != nil {
		return EmploymentTimeline{}, err
	}
	inputsDigest, err := (EmploymentTimelineQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: fields}).inputDigest(req.Authorization)
	if err != nil {
		return EmploymentTimeline{}, err
	}
	base := EmploymentTimeline{Worker: req.Worker, AsOf: req.AsOf, PolicyVersion: req.Authorization.PolicyVersion, InputsDigest: inputsDigest, Effects: evidence.ZeroEffects()}
	if !req.Authorization.SubjectDisclosable {
		base.Disclosure = DisclosureWithheld
		base.Presence = SubjectPresenceUnspecified
		base.WithheldReason = req.Authorization.SubjectDenialReason
		return finishEmploymentTimeline(base)
	}

	authorizedFields := make([]FieldID, 0, len(fields))
	denied := 0
	for _, field := range fields {
		ruling, _ := req.Authorization.RulingFor(field)
		if ruling.Effect == EffectAllow {
			authorizedFields = append(authorizedFields, field)
		} else {
			denied++
		}
	}
	base.Disclosure = DisclosureFull
	if denied > 0 {
		base.Disclosure = DisclosurePartial
	}
	base.Presence = SubjectPresent
	if len(authorizedFields) == 0 {
		return finishEmploymentTimeline(base)
	}

	set, err := reader.EmploymentTimelineAt(ctx, EmploymentTimelineQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: authorizedFields})
	if err != nil {
		return EmploymentTimeline{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
	}
	if err := set.Validate(); err != nil {
		return EmploymentTimeline{}, err
	}
	if set.Worker != req.Worker {
		return EmploymentTimeline{}, fmt.Errorf("%w: asked %s, answered %s", ErrFactSubjectMismatch, req.Worker, set.Worker)
	}
	if !set.Exists {
		base.Presence = SubjectAbsent
		return finishEmploymentTimeline(base)
	}
	for _, period := range set.Periods {
		if period.KnownAt.Instant().After(req.AsOf.KnownAt.Instant()) {
			return EmploymentTimeline{}, fmt.Errorf("%w: %s", ErrFutureKnownPeriod, period.PeriodID)
		}
	}

	sorted := append([]EmploymentPeriodFact(nil), set.Periods...)
	sort.Slice(sorted, func(i, j int) bool { return lessEmploymentPeriod(sorted[i], sorted[j]) })
	base.Periods = make([]EmploymentPeriod, 0, len(sorted))
	for _, fact := range sorted {
		period := EmploymentPeriod{PeriodID: fact.PeriodID, Kind: fact.Kind, Effective: fact.Effective, KnownAt: fact.KnownAt, Revision: fact.Revision, Authority: fact.Authority, Provenance: fact.Provenance}
		period.Fields = make([]ExplainedFact, 0, len(fields))
		for _, field := range fields {
			ruling, _ := req.Authorization.RulingFor(field)
			period.Fields = append(period.Fields, explainEmploymentField(fact, field, ruling))
		}
		base.Periods = append(base.Periods, period)
	}
	base.Gaps, base.Overlaps, base.Findings = timelineShape(sorted)
	base.Watermark = set.Watermark
	return finishEmploymentTimeline(base)
}
