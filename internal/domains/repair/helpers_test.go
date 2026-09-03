package repair_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/repair"
	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Fields the repair fixtures compare.
const (
	fieldBase     dataops.FieldID = "compensation.base"
	fieldGrade    dataops.FieldID = "assignment.grade"
	fieldJobCode  dataops.FieldID = "assignment.job_code"
	fieldLocation dataops.FieldID = "assignment.location"
)

const (
	localSystem    = "hcmnext"
	externalSystem = "incumbent-hris"
	authorityPol   = "authority.by_field/2026.1"
	policyVersion  = "authz.policy/2026.1"
	approvalPol    = "approval.policy/2026.1"
	purpose        = "workforce_administration"
	evaluatedAt    = "2026-08-31T12:00:00Z"
	retrievedAt    = "2026-08-31T00:00:00Z"
)

// instantAt parses an RFC3339 timestamp.
func instantAt(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(parsed)
}

// parseInstant parses an RFC3339 timestamp without failing the test.
func parseInstant(text string) (values.Instant, error) {
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return values.Instant{}, err
	}
	return values.NewInstant(parsed), nil
}

// knownAt wraps an RFC3339 timestamp as a knowledge time.
func knownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("known at %q: %v", text, err)
	}
	return k
}

// recordedAt wraps an RFC3339 timestamp as a recorded time.
func recordedAt(t *testing.T, text string) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(instantAt(t, text))
	if err != nil {
		t.Fatalf("recorded at %q: %v", text, err)
	}
	return r
}

// localDate parses an ISO date.
func localDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("parse date %q: %v", text, err)
	}
	return d
}

// openInterval builds an open-ended date interval under the fixture calendar.
func openInterval(t *testing.T, start string) values.EffectiveInterval {
	t.Helper()
	cal, err := fixtures.Calendar()
	if err != nil {
		t.Fatalf("fixture calendar: %v", err)
	}
	iv, err := values.NewOpenLocalDateInterval(localDate(t, start), cal)
	if err != nil {
		t.Fatalf("open interval [%s,): %v", start, err)
	}
	return iv
}

// revision builds a sequence revision on the worker stream.
func revision(t *testing.T, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision("worker", seq)
	if err != nil {
		t.Fatalf("revision %d: %v", seq, err)
	}
	return r
}

// subject returns the fixture worker reference.
func subject(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("jane-doe")
	if err != nil {
		t.Fatalf("fixture worker: %v", err)
	}
	return ref
}

// authority builds a source authority of the given kind.
func authority(kind evidence.AuthorityKind, system string) evidence.SourceAuthority {
	return evidence.SourceAuthority{Kind: kind, System: system, PolicyRef: authorityPol}
}

// canonicalField builds one field of the HCM Next side.
func canonicalField(
	t *testing.T,
	field dataops.FieldID,
	kind fielddiff.ValueKind,
	value, updated string,
	auth evidence.SourceAuthority,
	seq uint64,
) dataops.CanonicalField {
	t.Helper()
	return dataops.CanonicalField{
		Field:     field,
		Kind:      kind,
		Value:     values.Value(value),
		UpdatedAt: instantAt(t, updated),
		Effective: openInterval(t, "2026-01-01"),
		KnownAt:   knownAt(t, updated),
		Revision:  revision(t, seq),
		Authority: auth,
		Provenance: evidence.Provenance{
			Source:      auth.System,
			EvidenceRef: "evidence:" + string(field),
			RecordedAt:  recordedAt(t, updated),
		},
	}
}

// canonicalRecord builds the HCM Next side of the fixture comparison.
func canonicalRecord(t *testing.T, fields ...dataops.FieldID) dataops.CanonicalRecord {
	t.Helper()
	all := map[dataops.FieldID]dataops.CanonicalField{
		fieldBase: canonicalField(t, fieldBase, fielddiff.KindMoney,
			"135000.00 USD", "2026-08-14T00:00:00Z",
			authority(evidence.AuthorityLocal, localSystem), 101),
		fieldGrade: canonicalField(t, fieldGrade, fielddiff.KindEnum,
			"P3", "2025-12-15T00:00:00Z",
			authority(evidence.AuthorityLocal, localSystem), 102),
		fieldJobCode: canonicalField(t, fieldJobCode, fielddiff.KindEnum,
			"ENG-3", "2026-01-02T00:00:00Z",
			authority(evidence.AuthorityExternalObservation, externalSystem), 103),
		fieldLocation: canonicalField(t, fieldLocation, fielddiff.KindString,
			"Miami", "2026-01-01T00:00:00Z",
			authority(evidence.AuthorityLocal, localSystem), 104),
	}
	record := dataops.CanonicalRecord{
		Subject:       subject(t),
		Exists:        true,
		Watermark:     revision(t, 999),
		AsOfEffective: localDate(t, "2026-08-15"),
		AsKnownAt:     knownAt(t, "2026-08-31T00:00:00Z"),
	}
	for _, f := range fields {
		record.Fields = append(record.Fields, all[f])
	}
	return record
}

// observedRecord builds the incumbent's side of the fixture comparison.
func observedRecord(t *testing.T, fields ...dataops.FieldID) dataops.ObservedRecord {
	t.Helper()
	all := map[dataops.FieldID]dataops.ObservedField{
		fieldBase: {
			Field: fieldBase, Kind: fielddiff.KindMoney,
			Value:     values.Value("130000.00 USD"),
			UpdatedAt: instantAt(t, "2026-05-20T00:00:00Z"),
		},
		fieldGrade: {
			Field: fieldGrade, Kind: fielddiff.KindEnum,
			Value:     values.Value("P3"),
			UpdatedAt: instantAt(t, "2026-01-05T00:00:00Z"),
		},
		fieldJobCode: {
			Field: fieldJobCode, Kind: fielddiff.KindEnum,
			Value:     values.Value("ENG-4"),
			UpdatedAt: instantAt(t, "2026-08-25T00:00:00Z"),
		},
		fieldLocation: {
			Field: fieldLocation, Kind: fielddiff.KindString,
			Value:     values.Value("Austin"),
			UpdatedAt: instantAt(t, "2026-08-25T00:00:00Z"),
		},
	}
	record := dataops.ObservedRecord{
		Subject:    subject(t),
		ExternalID: "WD-100234",
		Exists:     true,
		Digest:     "sha256:record-1",
	}
	for _, f := range fields {
		record.Fields = append(record.Fields, all[f])
	}
	return record
}

// watermark builds the observation provenance for the fixture page.
func watermark(t *testing.T, retrieved string) dataops.ObservationWatermark {
	t.Helper()
	return dataops.ObservationWatermark{
		Source:        externalSystem,
		SchemaVersion: "incumbent.worker/v3",
		RetrievedAt:   recordedAt(t, retrieved),
		Digest:        "sha256:page-" + retrieved,
		Records:       1,
	}
}

// authorize builds an authorization decision allowing every listed field.
func authorize(fields ...dataops.FieldID) dataops.Authorization {
	d := dataops.Authorization{
		PolicyVersion:      policyVersion,
		Purpose:            purpose,
		SubjectDisclosable: true,
		Fields:             map[dataops.FieldID]dataops.Ruling{},
	}
	for _, f := range fields {
		d.Fields[f] = dataops.Ruling{Effect: dataops.EffectAllow}
	}
	return d
}

// freshness is the fixture observation-age policy: one day.
func freshness() dataops.FreshnessPolicy {
	return dataops.FreshnessPolicy{Version: "freshness.policy/1.0.0", MaxAgeSeconds: 86400}
}

// approvalPolicy is the fixture governance input.
func approvalPolicy() repair.ApprovalPolicy {
	return repair.ApprovalPolicy{
		Version:               approvalPol,
		RolesForExternalWrite: []string{"hris.administrator"},
		RolesForReview:        []string{"data.steward"},
		SoDExcludedRoles:      []string{"drift.reporter"},
	}
}

// scenario is one comparison plus everything a plan and a simulation need.
type scenario struct {
	fields    []dataops.FieldID
	canonical dataops.CanonicalRecord
	observed  dataops.ObservedRecord
	watermark dataops.ObservationWatermark
	auth      dataops.Authorization
	diff      dataops.RecordDiff
}

// buildScenario compares the two fixture sides over the given fields.
func buildScenario(t *testing.T, retrieved string, fields ...dataops.FieldID) scenario {
	t.Helper()
	s := scenario{
		fields:    fields,
		canonical: canonicalRecord(t, fields...),
		observed:  observedRecord(t, fields...),
		watermark: watermark(t, retrieved),
		auth:      authorize(fields...),
	}
	diff, err := dataops.DiffRecord(dataops.DiffRecordRequest{
		Canonical:       s.canonical,
		Observed:        s.observed,
		ObservedPresent: true,
		Observation:     s.watermark,
		Fields:          fields,
		Authorization:   s.auth,
		Freshness:       freshness(),
		EvaluatedAt:     instantAt(t, evaluatedAt),
	})
	if err != nil {
		t.Fatalf("DiffRecord: %v", err)
	}
	s.diff = diff
	return s
}

// safeScenario is a comparison whose only mismatches are repair-safe: one
// locally mastered field the incumbent is behind on, and one externally
// mastered field the projection is behind on.
func safeScenario(t *testing.T) scenario {
	t.Helper()
	return buildScenario(t, retrievedAt, fieldBase, fieldGrade, fieldJobCode)
}

// unsafeScenario adds a locally mastered field the incumbent changed, which no
// automation may resolve.
func unsafeScenario(t *testing.T) scenario {
	t.Helper()
	return buildScenario(t, retrievedAt, fieldBase, fieldGrade, fieldJobCode, fieldLocation)
}

// staleScenario is the same comparison judged against an observation older
// than the freshness policy allows.
func staleScenario(t *testing.T) scenario {
	t.Helper()
	return buildScenario(t, "2026-08-25T00:00:00Z", fieldBase, fieldGrade, fieldJobCode)
}

// diagnose builds the fixture diagnosis over a scenario.
func diagnose(t *testing.T, s scenario) repair.Diagnosis {
	t.Helper()
	d, err := repair.Diagnose("diag-1", s.diff, []string{"evidence:drift-report-1"})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	return d
}

// planRequest assembles the fixture plan input.
func planRequest(t *testing.T, s scenario) repair.CreateRepairPlanRequest {
	t.Helper()
	return repair.CreateRepairPlanRequest{
		PlanID:                 "plan-1",
		Tenant:                 subject(t).Tenant,
		Diagnosis:              diagnose(t, s),
		Diff:                   s.diff,
		Canonical:              s.canonical,
		Observed:               s.observed,
		ObservedPresent:        true,
		Observation:            s.watermark,
		LocalSystem:            localSystem,
		ExternalSystem:         externalSystem,
		Approval:               approvalPolicy(),
		AuthorityPolicyVersion: authorityPol,
	}
}

// plan builds the fixture plan over a scenario.
func plan(t *testing.T, s scenario) repair.RepairPlan {
	t.Helper()
	p, err := repair.CreateRepairPlan(planRequest(t, s))
	if err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	return p
}

// simulateRequest assembles the fixture simulation input.
func simulateRequest(t *testing.T, s scenario, p repair.RepairPlan) repair.SimulateRepairRequest {
	t.Helper()
	return repair.SimulateRepairRequest{
		Tenant:                 subject(t).Tenant,
		Plan:                   p,
		CurrentDiff:            s.diff,
		Canonical:              s.canonical,
		Observed:               s.observed,
		ObservedPresent:        true,
		Observation:            s.watermark,
		Fields:                 s.fields,
		Authorization:          s.auth,
		Freshness:              freshness(),
		EvaluatedAt:            instantAt(t, evaluatedAt),
		AuthorityPolicyVersion: authorityPol,
		LocalSystem:            localSystem,
		ExternalSystem:         externalSystem,
	}
}

// stepFor returns the plan step targeting a field.
func stepFor(t *testing.T, p repair.RepairPlan, field dataops.FieldID) repair.Step {
	t.Helper()
	step, ok := p.Step(field)
	if !ok {
		t.Fatalf("no step targets %s", field)
	}
	return step
}
