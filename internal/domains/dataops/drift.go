package dataops

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for the detect_drift slice of DATAOPS-008.
const (
	// DetectDriftIntentType is the catalog identifier this function
	// implements.
	DetectDriftIntentType = "hcmnext.operations.detect_drift"
	// DetectDriftIntentVersion is the contract version.
	DetectDriftIntentVersion = "v1"
	// DriftRulePackVersion versions the population bounding, page handling and
	// aggregation rules below.
	DriftRulePackVersion = "dataops.drift.rules/1.0.0"
)

// Population and paging bounds. A drift run is an operator diagnostic, not a
// bulk export: it states its own ceiling and reports truncation rather than
// silently walking a tenant.
const (
	// DefaultMaxPopulation is the population ceiling when a request states
	// none.
	DefaultMaxPopulation = 250
	// MaxPopulationCeiling is the largest ceiling a request may ask for.
	MaxPopulationCeiling = 1000
	// MaxObservationPages bounds how many pages one run will read. A source
	// that never exhausts its cursor stops the run instead of hanging it.
	MaxObservationPages = 64
	// DefaultPageLimit is the page size when a request states none.
	DefaultPageLimit = 100
)

// Drift errors. All are matchable with errors.Is.
var (
	// ErrPopulationEmpty is returned for a run with no subjects.
	ErrPopulationEmpty = errors.New("dataops: drift population is empty")
	// ErrPopulationBound is returned for an out-of-range population ceiling.
	ErrPopulationBound = errors.New("dataops: drift population ceiling is out of range")
	// ErrObservationPaging is returned when the source does not terminate its
	// cursor within the page bound, or repeats a cursor.
	ErrObservationPaging = errors.New("dataops: observation source did not terminate its cursor")
)

// SubjectDrift is one subject's comparison inside a drift report, together
// with the digest of the field history the canonical side was projected from.
type SubjectDrift struct {
	Subject values.EntityRef
	Diff    RecordDiff
	// HistoryDigest is the result digest of the effective-date explanation the
	// canonical side came from, so a reader can reproduce the exact projection
	// this comparison used.
	HistoryDigest string
	// Observed reports whether the source returned a record for this subject.
	Observed bool
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (s SubjectDrift) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.SubjectDrift", dataopsSchemaVer).
		Value("subject", s.Subject).
		Value("diff", s.Diff).
		String("history_digest", s.HistoryDigest).
		Bool("observed", s.Observed).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DriftReport is the detect_drift result: a bounded, authority-aware
// comparison of intended and observed state across a population, with every
// field classified and nothing repaired.
type DriftReport struct {
	IntentType    string
	IntentVersion string

	Tenant     values.TenantId
	Source     string
	Disclosure Disclosure
	// WithheldReason is the policy token when Disclosure is WITHHELD.
	WithheldReason string

	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
	EvaluatedAt   values.Instant
	Fields        []FieldID

	// Subjects is one entry per examined subject, in canonical subject order.
	Subjects []SubjectDrift
	Verdicts VerdictCounts
	Safety   SafetyCounts

	// PopulationRequested and PopulationExamined differ exactly when the run
	// was truncated by its own ceiling. Both are reported so a reader never
	// mistakes a bounded run for a complete one.
	PopulationRequested int
	PopulationExamined  int
	PopulationCeiling   int
	Truncated           bool

	// Watermarks is the provenance of every observation page the run read, in
	// read order.
	Watermarks []ObservationWatermark
	Narrative  []string

	PolicyVersion    string
	RulePackVersion  string
	DiffRuleVersion  string
	FreshnessVersion string
	InputsDigest     string
	ResultDigest     string
	Effects          evidence.EffectCounters
	Receipt          evidence.ZeroEffectReceipt
}

// Drift returns the subject entry for a subject.
func (r DriftReport) Drift(subject values.EntityRef) (SubjectDrift, bool) {
	for _, s := range r.Subjects {
		if s.Subject == subject {
			return s, true
		}
	}
	return SubjectDrift{}, false
}

// canonicalBody encodes everything except the receipt.
func (r DriftReport) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.dataops.DriftReport", dataopsSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("tenant", string(r.Tenant)).
		String("source", r.Source).
		String("disclosure", r.Disclosure.String()).
		String("withheld_reason", r.WithheldReason)
	if r.Disclosure != DisclosureWithheld {
		w.Value("as_of_effective", r.AsOfEffective).
			Value("as_known_at", r.AsKnownAt).
			Value("evaluated_at", r.EvaluatedAt)
	}
	w.Count("fields", len(r.Fields))
	for _, f := range r.Fields {
		w.String("field", string(f))
	}
	w.Count("subjects", len(r.Subjects))
	for _, s := range r.Subjects {
		w.Value("subject", s)
	}
	w.Value("verdicts", r.Verdicts).
		Value("safety", r.Safety).
		Int("population_requested", int64(r.PopulationRequested)).
		Int("population_examined", int64(r.PopulationExamined)).
		Int("population_ceiling", int64(r.PopulationCeiling)).
		Bool("truncated", r.Truncated).
		Count("watermarks", len(r.Watermarks))
	for _, wm := range r.Watermarks {
		w.Value("watermark", wm)
	}
	w.Count("narrative", len(r.Narrative))
	for _, line := range r.Narrative {
		w.String("narrative", line)
	}
	return w.
		String("policy_version", r.PolicyVersion).
		String("rule_pack_version", r.RulePackVersion).
		String("diff_rule_version", r.DiffRuleVersion).
		String("freshness_version", r.FreshnessVersion).
		Value("effects", r.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding including the receipt, or nil
// when the report is incoherent.
func (r DriftReport) Canonical() []byte {
	body, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.DriftReportEnvelope", dataopsSchemaVer).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Value("receipt", r.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DetectDriftRequest is the governed drift-detection request.
type DetectDriftRequest struct {
	Tenant values.TenantId
	// Source is the observing system to compare against.
	Source string
	// Population is the bounded set of subjects to examine.
	Population []values.EntityRef
	// Fields is the exact projection to compare.
	Fields []FieldID

	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
	// EvaluatedAt is the instant freshness is judged at. It is an input, not a
	// clock read, so a run can be replayed.
	EvaluatedAt values.Instant
	// IncludeClaims opts the canonical projection into unaccepted statements.
	IncludeClaims bool

	Authorization Authorization
	Freshness     FreshnessPolicy

	// MaxPopulation is the ceiling for this run. Zero means
	// DefaultMaxPopulation.
	MaxPopulation int
	// PageLimit is the observation page size. Zero means DefaultPageLimit.
	PageLimit int
}

// ceiling returns the effective population ceiling.
func (r DetectDriftRequest) ceiling() int {
	if r.MaxPopulation <= 0 {
		return DefaultMaxPopulation
	}
	return r.MaxPopulation
}

// pageLimit returns the effective observation page size.
func (r DetectDriftRequest) pageLimit() int {
	if r.PageLimit <= 0 {
		return DefaultPageLimit
	}
	return r.PageLimit
}

// Validate reports whether the request is well formed.
func (r DetectDriftRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if r.Source == "" {
		return fmt.Errorf("%w: no observation source named", ErrRequestInvalid)
	}
	if len(r.Population) == 0 {
		return ErrPopulationEmpty
	}
	if r.MaxPopulation < 0 || r.MaxPopulation > MaxPopulationCeiling {
		return fmt.Errorf("%w: %d exceeds %d", ErrPopulationBound, r.MaxPopulation, MaxPopulationCeiling)
	}
	if r.PageLimit < 0 || r.PageLimit > MaxObservationPageRecords {
		return fmt.Errorf("%w: page limit %d exceeds %d",
			ErrRequestInvalid, r.PageLimit, MaxObservationPageRecords)
	}
	for _, s := range r.Population {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("%w: population subject: %w", ErrRequestInvalid, err)
		}
		if s.Tenant != r.Tenant {
			return fmt.Errorf("%w: %s", ErrObservationOutOfTenant, s)
		}
	}
	if err := r.AsOfEffective.Validate(); err != nil {
		return fmt.Errorf("%w: as-of effective date: %w", ErrRequestInvalid, err)
	}
	if r.AsKnownAt.Canonical() == nil {
		return fmt.Errorf("%w: as-known-at is unset", ErrRequestInvalid)
	}
	if !r.EvaluatedAt.IsSet() {
		return fmt.Errorf("%w: no evaluation instant", ErrEvaluationTime)
	}
	if _, err := normalizeFields(r.Fields); err != nil {
		return fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	if err := r.Freshness.Validate(); err != nil {
		return err
	}
	return r.Authorization.Validate()
}

// population returns the deduplicated, canonically ordered population and
// whether the ceiling truncated it.
func (r DetectDriftRequest) population() ([]values.EntityRef, int, bool) {
	seen := make(map[values.EntityRef]struct{}, len(r.Population))
	unique := make([]values.EntityRef, 0, len(r.Population))
	for _, s := range r.Population {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		unique = append(unique, s)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].String() < unique[j].String() })
	requested := len(unique)
	if requested > r.ceiling() {
		return unique[:r.ceiling()], requested, true
	}
	return unique, requested, false
}

// inputsDigest digests exactly what the report depends on.
func (r DetectDriftRequest) inputsDigest() (string, error) {
	fields, err := normalizeFields(r.Fields)
	if err != nil {
		return "", err
	}
	subjects, requested, truncated := r.population()
	w := canonicalbytes.New("hcmnext.domains.dataops.DetectDriftRequest", dataopsSchemaVer).
		String("intent_type", DetectDriftIntentType).
		String("intent_version", DetectDriftIntentVersion).
		String("tenant", string(r.Tenant)).
		String("source", r.Source).
		Value("as_of_effective", r.AsOfEffective).
		Value("as_known_at", r.AsKnownAt).
		Value("evaluated_at", r.EvaluatedAt).
		Bool("include_claims", r.IncludeClaims).
		Int("population_requested", int64(requested)).
		Bool("truncated", truncated).
		Count("subjects", len(subjects))
	for _, s := range subjects {
		w.Value("subject", s)
	}
	w.Count("fields", len(fields))
	for _, f := range fields {
		w.String("field", string(f))
	}
	return w.
		Value("freshness", r.Freshness).
		Value("authorization", r.Authorization).
		Digest()
}

// DetectDrift is the P1A read-only implementation of
// hcmnext.operations.detect_drift/v1: it projects the canonical side of a
// bounded population through the effective-date debugger, reads the external
// observations of the same population, and classifies every requested field.
//
// It causes nothing. There is no repair argument, no execution mode and no
// escalation path: a mismatch produces a finding that names the capabilities
// somebody else may invoke, and the run's own effect counters are asserted to
// be zero before a receipt is minted.
//
// The population is bounded by the request's own ceiling and the report says
// when it truncated. An operator diagnostic that quietly walked an entire
// tenant would be a bulk export wearing a diagnostic's name.
func DetectDrift(
	ctx context.Context,
	history FieldHistory,
	observations ObservationReader,
	req DetectDriftRequest,
) (DriftReport, error) {
	if history == nil {
		return DriftReport{}, fmt.Errorf("%w: no field history reader", ErrRequestInvalid)
	}
	if observations == nil {
		return DriftReport{}, fmt.Errorf("%w: no observation reader", ErrRequestInvalid)
	}
	if err := req.Validate(); err != nil {
		return DriftReport{}, err
	}
	fields, err := normalizeFields(req.Fields)
	if err != nil {
		return DriftReport{}, err
	}
	if err := req.Authorization.Covers(fields); err != nil {
		return DriftReport{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return DriftReport{}, err
	}
	subjects, requested, truncated := req.population()

	report := DriftReport{
		IntentType:          DetectDriftIntentType,
		IntentVersion:       DetectDriftIntentVersion,
		Tenant:              req.Tenant,
		Source:              req.Source,
		AsOfEffective:       req.AsOfEffective,
		AsKnownAt:           req.AsKnownAt,
		EvaluatedAt:         req.EvaluatedAt,
		Fields:              fields,
		PopulationRequested: requested,
		PopulationCeiling:   req.ceiling(),
		Truncated:           truncated,
		PolicyVersion:       req.Authorization.PolicyVersion,
		RulePackVersion:     DriftRulePackVersion,
		DiffRuleVersion:     DiffRulePackVersion,
		FreshnessVersion:    req.Freshness.Version,
		InputsDigest:        inputsDigest,
		Effects:             evidence.ZeroEffects(),
	}

	if !req.Authorization.SubjectDisclosable {
		report.Disclosure = DisclosureWithheld
		report.WithheldReason = req.Authorization.SubjectDenialReason
		report.PopulationExamined = 0
		report.Narrative = []string{
			"population is not disclosable to this caller under the evaluated policy",
		}
		return finishDrift(report)
	}

	records, watermarks, err := readObservations(ctx, observations, req, subjects, fields)
	if err != nil {
		return DriftReport{}, err
	}
	report.Watermarks = watermarks
	// Subjects the source returned nothing for are still examined: their
	// fields come back UNKNOWN against the watermark of the read that did not
	// find them, which is a finding about coverage rather than a silent gap.
	sourceWatermark := watermarks[0]

	denied := 0
	for _, f := range fields {
		if !req.Authorization.Allows(f) {
			denied++
		}
	}

	for _, subject := range subjects {
		explanation, err := ExplainFieldHistory(ctx, history, ExplainFieldHistoryRequest{
			Tenant:        req.Tenant,
			Subject:       subject,
			Fields:        fields,
			AsOfEffective: req.AsOfEffective,
			AsKnownAt:     req.AsKnownAt,
			IncludeClaims: req.IncludeClaims,
			Authorization: req.Authorization,
		})
		if err != nil {
			return DriftReport{}, err
		}
		canonical, err := ProjectCanonicalRecord(explanation)
		if err != nil {
			return DriftReport{}, err
		}
		observed, present := records[subject]
		watermark := sourceWatermark
		if present {
			watermark = observed.watermark
		}
		diff, err := DiffRecord(DiffRecordRequest{
			Canonical:       canonical,
			Observed:        observed.record,
			ObservedPresent: present,
			Observation:     watermark,
			Fields:          fields,
			Authorization:   req.Authorization,
			Freshness:       req.Freshness,
			EvaluatedAt:     req.EvaluatedAt,
		})
		if err != nil {
			return DriftReport{}, err
		}
		report.Verdicts.merge(diff.Verdicts)
		report.Safety.merge(diff.Safety)
		report.Subjects = append(report.Subjects, SubjectDrift{
			Subject:       subject,
			Diff:          diff,
			HistoryDigest: explanation.ResultDigest,
			Observed:      present,
		})
	}

	report.PopulationExamined = len(report.Subjects)
	report.Disclosure = DisclosureFull
	if denied > 0 {
		report.Disclosure = DisclosurePartial
	}
	report.Narrative = narrateDrift(report, denied)
	return finishDrift(report)
}

// observedEntry pairs one observed record with the page provenance it came
// from, so a finding cites the page it actually rests on rather than the last
// page the run happened to read.
type observedEntry struct {
	record    ObservedRecord
	watermark ObservationWatermark
}

// readObservations walks the source's cursor until it is exhausted, collecting
// records for the requested population.
//
// A record about a subject outside the requested population is a refusal, not
// a filter. A port that widens the population has either misunderstood the
// query or been handed the wrong tenant's data, and quietly dropping the extra
// records would hide both.
func readObservations(
	ctx context.Context,
	reader ObservationReader,
	req DetectDriftRequest,
	subjects []values.EntityRef,
	fields []FieldID,
) (map[values.EntityRef]observedEntry, []ObservationWatermark, error) {
	inPopulation := make(map[values.EntityRef]struct{}, len(subjects))
	for _, s := range subjects {
		inPopulation[s] = struct{}{}
	}

	records := make(map[values.EntityRef]observedEntry, len(subjects))
	watermarks := make([]ObservationWatermark, 0, 1)
	seenCursors := map[string]struct{}{}
	cursor := ""

	for page := 0; page < MaxObservationPages; page++ {
		if _, repeat := seenCursors[cursor]; repeat {
			return nil, nil, fmt.Errorf("%w: cursor %q repeated", ErrObservationPaging, cursor)
		}
		seenCursors[cursor] = struct{}{}

		got, err := reader.ObservationsAt(ctx, ObservationQuery{
			Tenant:   req.Tenant,
			Source:   req.Source,
			Subjects: subjects,
			Fields:   fields,
			Cursor:   cursor,
			Limit:    req.pageLimit(),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrReaderFailed, err)
		}
		if err := got.Validate(); err != nil {
			return nil, nil, err
		}
		if got.Source != req.Source {
			return nil, nil, fmt.Errorf("%w: asked %s, answered %s",
				ErrPortWidenedProjection, req.Source, got.Source)
		}
		watermark := got.Watermark()
		for _, record := range got.Records {
			if _, ok := inPopulation[record.Subject]; !ok {
				return nil, nil, fmt.Errorf("%w: observation for %s outside the requested population",
					ErrPortWidenedProjection, record.Subject)
			}
			if _, dup := records[record.Subject]; dup {
				return nil, nil, fmt.Errorf("%w: %s appears on two pages",
					ErrObservationDuplicate, record.Subject)
			}
			for _, f := range record.Fields {
				if !containsField(fields, f.Field) {
					return nil, nil, fmt.Errorf("%w: observation returned %s",
						ErrPortWidenedProjection, f.Field)
				}
			}
			records[record.Subject] = observedEntry{record: record, watermark: watermark}
		}
		watermarks = append(watermarks, watermark)
		if got.NextCursor == "" {
			return records, watermarks, nil
		}
		cursor = got.NextCursor
	}
	return nil, nil, fmt.Errorf("%w: stopped after %d pages", ErrObservationPaging, MaxObservationPages)
}

// containsField reports whether a sorted projection contains a field.
func containsField(fields []FieldID, f FieldID) bool {
	for _, candidate := range fields {
		if candidate == f {
			return true
		}
	}
	return false
}

// narrateDrift builds the bounded, value-free narrative.
func narrateDrift(r DriftReport, denied int) []string {
	unobserved := 0
	for _, s := range r.Subjects {
		if !s.Observed {
			unobserved++
		}
	}
	lines := []string{
		fmt.Sprintf("compared %d subject(s) of %d requested against source %s",
			len(r.Subjects), r.PopulationRequested, r.Source),
		fmt.Sprintf("effective on %s, as known at %s, freshness judged at %s",
			r.AsOfEffective, r.AsKnownAt, r.EvaluatedAt),
		fmt.Sprintf("%d field(s) per subject, %d denied to this caller", len(r.Fields), denied),
		fmt.Sprintf("verdicts: %d match, %d mismatch, %d stale, %d unknown, %d redacted, %d not applicable",
			r.Verdicts.Match, r.Verdicts.Mismatch, r.Verdicts.Stale,
			r.Verdicts.Unknown, r.Verdicts.Redacted, r.Verdicts.NotApplicable),
		fmt.Sprintf("repair classification: %d safe, %d unsafe, %d undecidable, %d not required",
			r.Safety.Safe, r.Safety.Unsafe, r.Safety.Undecidable, r.Safety.NotRequired),
		fmt.Sprintf("%d observation page(s) read", len(r.Watermarks)),
	}
	if r.Truncated {
		lines = append(lines, fmt.Sprintf(
			"population truncated at the ceiling of %d; this report is not a complete tenant view",
			r.PopulationCeiling))
	}
	if unobserved > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d subject(s) had no record at the source; their fields are UNKNOWN, not mismatched", unobserved))
	}
	lines = append(lines, "no repair was performed; every mismatch names the capabilities allowed next")
	return lines
}

// finishDrift digests the body, mints the zero-effect receipt and returns the
// completed report.
func finishDrift(r DriftReport) (DriftReport, error) {
	if len(r.Narrative) > MaxNarrativeLines {
		return DriftReport{}, fmt.Errorf("%w: %d lines", ErrNarrativeOverflow, len(r.Narrative))
	}
	if r.Verdicts.Total() != r.Safety.Total() {
		return DriftReport{}, fmt.Errorf(
			"dataops: drift report counts disagree: %d verdicts, %d safety classes",
			r.Verdicts.Total(), r.Safety.Total())
	}
	body, err := r.canonicalBody()
	if err != nil {
		return DriftReport{}, err
	}
	r.ResultDigest = canonicalbytes.Digest(body)
	controls := []evidence.ControlVersion{
		{Name: "authorization_policy", Version: r.PolicyVersion},
		{Name: "drift_rule_pack", Version: r.RulePackVersion},
		{Name: "diff_rule_pack", Version: r.DiffRuleVersion},
		{Name: "history_rule_pack", Version: HistoryRulePackVersion},
	}
	if r.FreshnessVersion != "" {
		controls = append(controls, evidence.ControlVersion{
			Name: "freshness_policy", Version: r.FreshnessVersion,
		})
	}
	receipt, err := evidence.NewZeroEffectReceipt(
		r.IntentType, r.IntentVersion,
		evidence.ModeSimulate,
		evidence.RequestStateSimulated,
		controls,
		r.InputsDigest, r.ResultDigest, r.Effects,
	)
	if err != nil {
		return DriftReport{}, err
	}
	r.Receipt = receipt
	return r, nil
}
