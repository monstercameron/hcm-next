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
	AssignmentTimelineIntentType    = "hcmnext.people.assignment.timeline.read"
	AssignmentTimelineIntentVersion = "v1"
	AssignmentTimelineRulePack      = "people.assignment.timeline/2026.1"
	assignmentTimelineSchema        = "hcmnext.domains.people.AssignmentTimeline"
)

var (
	ErrAssignmentTimelineInvalid = errors.New("people: invalid assignment timeline")
	ErrAssignmentPeriodInvalid   = errors.New("people: invalid assignment period")
	ErrFutureKnownAssignment     = errors.New("people: assignment period is known after the requested cutoff")
)

// FieldAssignmentCostCenter is part of the closed Assignment projection. It
// is declared here because cost center belongs to the assignment read and must
// not widen the general worker-state projection.
const FieldAssignmentCostCenter FieldID = "assignment.cost_center"

// FieldCostCenter is a concise compatibility name for the assignment cost
// center field.
const FieldCostCenter = FieldAssignmentCostCenter

var assignmentTimelineFields = []FieldID{
	FieldAssignmentID,
	FieldJobCode,
	FieldPositionID,
	FieldOrgUnit,
	FieldLocation,
	FieldManagerRelation,
	FieldAssignmentCostCenter,
}

// AssignmentTimelineFields returns a copy of the fixed Assignment projection.
func AssignmentTimelineFields() []FieldID {
	return append([]FieldID(nil), assignmentTimelineFields...)
}

// AssignmentPeriodKind is the typed lifecycle meaning of an assignment
// period. Employment remains the legal relationship; this kind describes the
// worker's placement of work within that relationship.
type AssignmentPeriodKind string

const (
	AssignmentPeriodPlanned    AssignmentPeriodKind = "PLANNED"
	AssignmentPeriodActive     AssignmentPeriodKind = "ACTIVE"
	AssignmentPeriodSuperseded AssignmentPeriodKind = "SUPERSEDED"
	AssignmentPeriodEnded      AssignmentPeriodKind = "ENDED"
	AssignmentPeriodCancelled  AssignmentPeriodKind = "CANCELLED"
)

const (
	AssignmentKindPlanned    = AssignmentPeriodPlanned
	AssignmentKindActive     = AssignmentPeriodActive
	AssignmentKindSuperseded = AssignmentPeriodSuperseded
	AssignmentKindEnded      = AssignmentPeriodEnded
	AssignmentKindCancelled  = AssignmentPeriodCancelled
)

var assignmentPeriodKinds = map[AssignmentPeriodKind]struct{}{
	AssignmentPeriodPlanned: {}, AssignmentPeriodActive: {},
	AssignmentPeriodSuperseded: {}, AssignmentPeriodEnded: {},
	AssignmentPeriodCancelled: {},
}

func (k AssignmentPeriodKind) Valid() bool {
	_, ok := assignmentPeriodKinds[k]
	return ok
}

func (k AssignmentPeriodKind) String() string { return string(k) }

// AssignmentTimelineQuery is the fixed-mask query sent to an Assignment
// reader. Fields is retained for the port contract, but can contain only the
// fixed mask owned by this specialization.
type AssignmentTimelineQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   AsOf
	Fields []FieldID
}

func (q AssignmentTimelineQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrAssignmentTimelineInvalid, err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrAssignmentTimelineInvalid, err)
	}
	if q.Worker.Tenant != q.Tenant || q.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: worker must be a worker in the requested tenant", ErrAssignmentTimelineInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %w", ErrAssignmentTimelineInvalid, err)
	}
	if len(q.Fields) == 0 {
		return fmt.Errorf("%w: empty fixed field mask", ErrAssignmentTimelineInvalid)
	}
	allowed := make(map[FieldID]struct{}, len(assignmentTimelineFields))
	for _, field := range assignmentTimelineFields {
		allowed[field] = struct{}{}
	}
	seen := make(map[FieldID]struct{}, len(q.Fields))
	for _, field := range q.Fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%w: field %s is outside the assignment mask", ErrAssignmentTimelineInvalid, field)
		}
		if _, duplicate := seen[field]; duplicate {
			return fmt.Errorf("%w: duplicate field %s", ErrAssignmentTimelineInvalid, field)
		}
		seen[field] = struct{}{}
	}
	return nil
}

// AssignmentPeriodFact is one independent assignment assertion or revision.
// It deliberately carries the placement facts together with their bitemporal
// evidence; no default or synthesized assignment is created by the reader.
type AssignmentPeriodFact struct {
	PeriodID               string
	AssignmentID           string
	EmploymentID           string
	JobCode                string
	PositionID             string
	OrgUnit                string
	Location               string
	ManagerRelationshipRef string
	CostCenter             string
	Status                 string
	Kind                   AssignmentPeriodKind

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

func (p AssignmentPeriodFact) Validate() error {
	if p.PeriodID == "" || p.AssignmentID == "" {
		return fmt.Errorf("%w: period and assignment ids are required", ErrAssignmentPeriodInvalid)
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: unknown kind %q", ErrAssignmentPeriodInvalid, p.Kind)
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %w", ErrAssignmentPeriodInvalid, err)
	}
	if p.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrAssignmentPeriodInvalid)
	}
	if p.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: known-at is required", ErrAssignmentPeriodInvalid)
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrAssignmentPeriodInvalid)
	}
	if err := p.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %w", ErrAssignmentPeriodInvalid, err)
	}
	if err := p.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %w", ErrAssignmentPeriodInvalid, err)
	}
	return values.ValidateKnowledgeOrder(p.KnownAt, p.Provenance.RecordedAt, false)
}

// AssignmentTimelineFactSet is the storage-neutral result returned by an
// Assignment reader.
type AssignmentTimelineFactSet struct {
	Worker    values.EntityRef
	Exists    bool
	Periods   []AssignmentPeriodFact
	Watermark values.RevisionToken
}

func (s AssignmentTimelineFactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrAssignmentTimelineInvalid, err)
	}
	if s.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: fact-set subject is not a worker", ErrAssignmentTimelineInvalid)
	}
	if !s.Exists {
		if len(s.Periods) != 0 {
			return fmt.Errorf("%w: absent worker carries periods", ErrAssignmentTimelineInvalid)
		}
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: existing worker has no read watermark", ErrAssignmentTimelineInvalid)
	}
	seen := make(map[string]struct{}, len(s.Periods))
	for _, period := range s.Periods {
		if err := period.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[period.PeriodID]; duplicate {
			return fmt.Errorf("%w: duplicate period %s", ErrAssignmentTimelineInvalid, period.PeriodID)
		}
		seen[period.PeriodID] = struct{}{}
	}
	return nil
}

// AssignmentTimelineFacts is the storage-neutral Assignment read port.
type AssignmentTimelineFacts interface {
	AssignmentTimelineAt(context.Context, AssignmentTimelineQuery) (AssignmentTimelineFactSet, error)
}

// AssignmentPeriod is one disclosed assignment period. Fields are fixed and
// field-level so a denied placement fact remains DENIED instead of becoming an
// empty authoritative value.
type AssignmentPeriod struct {
	PeriodID string
	Kind     AssignmentPeriodKind

	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Revision   values.RevisionToken
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
	Fields     []ExplainedFact
}

func (p AssignmentPeriod) Value(field FieldID) (string, bool) {
	for _, fact := range p.Fields {
		if fact.Field == field && fact.Access == AccessAuthorized {
			return fact.Value.Get()
		}
	}
	return "", false
}

type AssignmentGap struct {
	From           values.LocalDate
	To             values.LocalDate
	BeforePeriodID string
	AfterPeriodID  string
}

type AssignmentOverlap struct {
	From           values.LocalDate
	To             values.LocalDate
	HasEnd         bool
	FirstPeriodID  string
	SecondPeriodID string
}

type AssignmentFindingCode string

const (
	AssignmentFindingGap     AssignmentFindingCode = "GAP"
	AssignmentFindingOverlap AssignmentFindingCode = "OVERLAP"
)

type AssignmentTimelineFinding struct {
	Code           AssignmentFindingCode
	From           values.LocalDate
	To             values.LocalDate
	HasEnd         bool
	FirstPeriodID  string
	SecondPeriodID string
}

// AssignmentTimeline is the closed, zero-effect Assignment timeline read.
// Periods are ordered by effective start and retain future, temporary,
// secondary and correction overlaps explicitly.
type AssignmentTimeline struct {
	Worker         values.EntityRef
	Disclosure     Disclosure
	Presence       SubjectPresence
	WithheldReason string
	AsOf           AsOf

	Periods  []AssignmentPeriod
	Gaps     []AssignmentGap
	Overlaps []AssignmentOverlap
	Findings []AssignmentTimelineFinding

	Watermark     values.RevisionToken
	PolicyVersion string
	InputsDigest  string
	ResultDigest  string
	Effects       evidence.EffectCounters
	Receipt       evidence.ZeroEffectReceipt
}

func (p AssignmentPeriod) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.people.AssignmentPeriod", peopleSchemaVer).
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

func (t AssignmentTimeline) canonicalBody() []byte {
	w := canonicalbytes.New(assignmentTimelineSchema, peopleSchemaVer).
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

func (t AssignmentTimeline) Canonical() []byte {
	body := t.canonicalBody()
	if len(body) == 0 {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.people.AssignmentTimelineEnvelope", peopleSchemaVer).
		Field("body", body).
		Field("receipt", t.Receipt.Canonical()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func assignmentTimelineInputDigest(q AssignmentTimelineQuery, auth AuthorizationDecision) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.people.AssignmentTimelineQuery", peopleSchemaVer).
		String("intent_type", AssignmentTimelineIntentType).
		String("intent_version", AssignmentTimelineIntentVersion).
		String("tenant", string(q.Tenant)).
		Value("worker", q.Worker).
		Value("as_of", q.AsOf).
		Count("fields", len(assignmentTimelineFields))
	for _, field := range assignmentTimelineFields {
		w.String("field", string(field))
		ruling, ok := auth.RulingFor(field)
		if !ok {
			w.String("effect", "UNRULED").String("reason", "")
			continue
		}
		w.String("effect", ruling.Effect.String()).String("reason", ruling.Reason)
	}
	w.String("policy_version", auth.PolicyVersion).
		String("purpose", auth.Purpose).
		Bool("subject_disclosable", auth.SubjectDisclosable).
		String("subject_denial_reason", auth.SubjectDenialReason)
	return w.Digest()
}

func validateAssignmentAuthorization(auth AuthorizationDecision) error {
	if auth.PolicyVersion == "" || auth.Purpose == "" {
		return fmt.Errorf("%w: policy version and purpose are required", ErrAssignmentTimelineInvalid)
	}
	if !auth.SubjectDisclosable && auth.SubjectDenialReason == "" {
		return fmt.Errorf("%w: withheld subject requires a denial reason", ErrAssignmentTimelineInvalid)
	}
	for field, ruling := range auth.Fields {
		if ruling.Effect == EffectUnspecified || !ruling.Effect.Valid() {
			return fmt.Errorf("%w: invalid ruling for %s", ErrAssignmentTimelineInvalid, field)
		}
		if ruling.Effect == EffectDeny && ruling.Reason == "" {
			return fmt.Errorf("%w: denial for %s requires a reason", ErrAssignmentTimelineInvalid, field)
		}
	}
	return nil
}

func assignmentField(f AssignmentPeriodFact, field FieldID) string {
	switch field {
	case FieldAssignmentID:
		return f.AssignmentID
	case FieldJobCode:
		return f.JobCode
	case FieldPositionID:
		return f.PositionID
	case FieldOrgUnit:
		return f.OrgUnit
	case FieldLocation:
		return f.Location
	case FieldManagerRelation:
		return f.ManagerRelationshipRef
	case FieldAssignmentCostCenter:
		return f.CostCenter
	default:
		return ""
	}
}

func explainAssignmentField(f AssignmentPeriodFact, field FieldID, ruling FieldRuling) ExplainedFact {
	if ruling.Effect == EffectDeny {
		return ExplainedFact{Field: field, Access: AccessDenied, DenialReason: ruling.Reason, Value: values.Redacted[string](ruling.Reason)}
	}
	value := values.Absent[string]()
	if raw := assignmentField(f, field); raw != "" {
		value = values.Value(raw)
	}
	return ExplainedFact{Field: field, Access: AccessAuthorized, Value: value,
		Effective: f.Effective, KnownAt: f.KnownAt, Revision: f.Revision,
		Authority: f.Authority, Provenance: f.Provenance}
}

func assignmentPeriodStart(p AssignmentPeriodFact) values.LocalDate {
	d, _ := p.Effective.StartDate()
	return d
}

func lessAssignmentPeriod(a, b AssignmentPeriodFact) bool {
	if cmp := assignmentPeriodStart(a).Compare(assignmentPeriodStart(b)); cmp != 0 {
		return cmp < 0
	}
	if cmp := a.KnownAt.Instant().Compare(b.KnownAt.Instant()); cmp != 0 {
		return cmp < 0
	}
	return a.PeriodID < b.PeriodID
}

func assignmentDateRange(p AssignmentPeriodFact) (values.LocalDate, values.LocalDate, bool) {
	start, _ := p.Effective.StartDate()
	end, hasEnd := p.Effective.EndDate()
	return start, end, hasEnd
}

func assignmentOverlapOf(a, b AssignmentPeriodFact) (AssignmentOverlap, bool) {
	aStart, aEnd, aHasEnd := assignmentDateRange(a)
	bStart, bEnd, bHasEnd := assignmentDateRange(b)
	from := aStart
	if bStart.Compare(from) > 0 {
		from = bStart
	}
	to, hasEnd := aEnd, aHasEnd
	if !bHasEnd {
		if !aHasEnd {
			hasEnd = false
		}
	} else if !aHasEnd || bEnd.Compare(to) < 0 {
		to, hasEnd = bEnd, true
	}
	if hasEnd && from.Compare(to) >= 0 {
		return AssignmentOverlap{}, false
	}
	return AssignmentOverlap{From: from, To: to, HasEnd: hasEnd, FirstPeriodID: a.PeriodID, SecondPeriodID: b.PeriodID}, true
}

func assignmentTimelineShape(periods []AssignmentPeriodFact) ([]AssignmentGap, []AssignmentOverlap, []AssignmentTimelineFinding) {
	if len(periods) == 0 {
		return nil, nil, nil
	}
	ordered := append([]AssignmentPeriodFact(nil), periods...)
	sort.Slice(ordered, func(i, j int) bool { return lessAssignmentPeriod(ordered[i], ordered[j]) })
	var gaps []AssignmentGap
	var overlaps []AssignmentOverlap
	var findings []AssignmentTimelineFinding
	_, furthestEnd, furthestHasEnd := assignmentDateRange(ordered[0])
	furthestID := ordered[0].PeriodID
	for i := 1; i < len(ordered); i++ {
		start, end, hasEnd := assignmentDateRange(ordered[i])
		if furthestHasEnd && start.Compare(furthestEnd) > 0 {
			gap := AssignmentGap{From: furthestEnd, To: start, BeforePeriodID: furthestID, AfterPeriodID: ordered[i].PeriodID}
			gaps = append(gaps, gap)
			findings = append(findings, AssignmentTimelineFinding{Code: AssignmentFindingGap, From: gap.From, To: gap.To, HasEnd: true, FirstPeriodID: gap.BeforePeriodID, SecondPeriodID: gap.AfterPeriodID})
		}
		if overlap, ok := assignmentOverlapOf(ordered[i-1], ordered[i]); ok {
			overlaps = append(overlaps, overlap)
			findings = append(findings, AssignmentTimelineFinding{Code: AssignmentFindingOverlap, From: overlap.From, To: overlap.To, HasEnd: overlap.HasEnd, FirstPeriodID: overlap.FirstPeriodID, SecondPeriodID: overlap.SecondPeriodID})
		}
		if !furthestHasEnd || (hasEnd && end.Compare(furthestEnd) > 0) {
			furthestEnd, furthestHasEnd, furthestID = end, hasEnd, ordered[i].PeriodID
		}
	}
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			overlapsInInterval, err := ordered[j].Effective.Overlaps(ordered[i].Effective)
			if err != nil || !overlapsInInterval {
				continue
			}
			overlap, ok := assignmentOverlapOf(ordered[i], ordered[j])
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
			findings = append(findings, AssignmentTimelineFinding{Code: AssignmentFindingOverlap, From: overlap.From, To: overlap.To, HasEnd: overlap.HasEnd, FirstPeriodID: overlap.FirstPeriodID, SecondPeriodID: overlap.SecondPeriodID})
		}
	}
	return gaps, overlaps, findings
}

func finishAssignmentTimeline(t AssignmentTimeline) (AssignmentTimeline, error) {
	body := t.canonicalBody()
	if len(body) == 0 {
		return AssignmentTimeline{}, fmt.Errorf("%w: result could not be canonically encoded", ErrAssignmentTimelineInvalid)
	}
	t.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		AssignmentTimelineIntentType, AssignmentTimelineIntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		[]evidence.ControlVersion{{Name: "authorization_policy", Version: t.PolicyVersion}, {Name: "assignment_timeline_rule_pack", Version: AssignmentTimelineRulePack}},
		t.InputsDigest, t.ResultDigest, t.Effects,
	)
	if err != nil {
		return AssignmentTimeline{}, err
	}
	t.Receipt = receipt
	return t, nil
}

// ReadAssignmentTimeline is the PEOPLE-003 closed specialization. It reads
// only the Assignment mask, keeps each placement/revision independent, and
// derives gaps and overlaps without mutation or default assignment invention.
func ReadAssignmentTimeline(ctx context.Context, reader AssignmentTimelineFacts, req PersonWorkerReadRequest) (AssignmentTimeline, error) {
	if reader == nil {
		return AssignmentTimeline{}, fmt.Errorf("%w: no assignment timeline reader", ErrAssignmentTimelineInvalid)
	}
	if err := req.Validate(); err != nil {
		return AssignmentTimeline{}, err
	}
	if err := validateAssignmentAuthorization(req.Authorization); err != nil {
		return AssignmentTimeline{}, err
	}
	fields := AssignmentTimelineFields()
	if err := req.Authorization.Covers(fields); err != nil {
		return AssignmentTimeline{}, err
	}
	query := AssignmentTimelineQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: fields}
	inputsDigest, err := assignmentTimelineInputDigest(query, req.Authorization)
	if err != nil {
		return AssignmentTimeline{}, err
	}
	base := AssignmentTimeline{Worker: req.Worker, AsOf: req.AsOf, PolicyVersion: req.Authorization.PolicyVersion, InputsDigest: inputsDigest, Effects: evidence.ZeroEffects()}
	if !req.Authorization.SubjectDisclosable {
		base.Disclosure = DisclosureWithheld
		base.Presence = SubjectPresenceUnspecified
		base.WithheldReason = req.Authorization.SubjectDenialReason
		return finishAssignmentTimeline(base)
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
		return finishAssignmentTimeline(base)
	}

	set, err := reader.AssignmentTimelineAt(ctx, AssignmentTimelineQuery{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: authorizedFields})
	if err != nil {
		return AssignmentTimeline{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
	}
	if err := set.Validate(); err != nil {
		return AssignmentTimeline{}, err
	}
	if set.Worker != req.Worker {
		return AssignmentTimeline{}, fmt.Errorf("%w: asked %s, answered %s", ErrFactSubjectMismatch, req.Worker, set.Worker)
	}
	if !set.Exists {
		base.Presence = SubjectAbsent
		return finishAssignmentTimeline(base)
	}
	for _, period := range set.Periods {
		if period.KnownAt.Instant().After(req.AsOf.KnownAt.Instant()) {
			return AssignmentTimeline{}, fmt.Errorf("%w: %s", ErrFutureKnownAssignment, period.PeriodID)
		}
	}

	sorted := append([]AssignmentPeriodFact(nil), set.Periods...)
	sort.Slice(sorted, func(i, j int) bool { return lessAssignmentPeriod(sorted[i], sorted[j]) })
	base.Periods = make([]AssignmentPeriod, 0, len(sorted))
	for _, fact := range sorted {
		period := AssignmentPeriod{PeriodID: fact.PeriodID, Kind: fact.Kind, Effective: fact.Effective, KnownAt: fact.KnownAt, Revision: fact.Revision, Authority: fact.Authority, Provenance: fact.Provenance}
		period.Fields = make([]ExplainedFact, 0, len(fields))
		for _, field := range fields {
			ruling, _ := req.Authorization.RulingFor(field)
			period.Fields = append(period.Fields, explainAssignmentField(fact, field, ruling))
		}
		base.Periods = append(base.Periods, period)
	}
	base.Gaps, base.Overlaps, base.Findings = assignmentTimelineShape(sorted)
	base.Watermark = set.Watermark
	return finishAssignmentTimeline(base)
}
