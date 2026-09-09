package dataops_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// diffFields is the projection the cross-system diff is exercised over.
var diffFields = []dataops.FieldID{fieldBase, fieldGrade, fieldJobCode}

const evaluatedAt = "2026-08-31T12:00:00Z"

// canonicalSide projects the canonical record through the effective-date
// debugger, which is the dependency DATAOPS-008 declares on DATAOPS-007: the
// value a diff compares is the value the debugger would explain.
func canonicalSide(t *testing.T, auth dataops.Authorization) dataops.CanonicalRecord {
	t.Helper()
	explanation, err := dataops.ExplainFieldHistory(context.Background(), corpus(t),
		dataops.ExplainFieldHistoryRequest{
			Tenant:        subject(t).Tenant,
			Subject:       subject(t),
			Fields:        diffFields,
			AsOfEffective: localDate(t, "2026-08-15"),
			AsKnownAt:     knownAt(t, "2026-08-31T00:00:00Z"),
			Authorization: auth,
		})
	if err != nil {
		t.Fatalf("project canonical side: %v", err)
	}
	record, err := dataops.ProjectCanonicalRecord(explanation)
	if err != nil {
		t.Fatalf("ProjectCanonicalRecord: %v", err)
	}
	return record
}

// observedSide builds the incumbent's view: it agrees on grade, is behind on
// the locally mastered compensation, and is ahead on the externally mastered
// job code.
func observedSide(t *testing.T) dataops.ObservedRecord {
	t.Helper()
	return dataops.ObservedRecord{
		Subject:    subject(t),
		ExternalID: "WD-100234",
		Exists:     true,
		Digest:     "sha256:record-1",
		Fields: []dataops.ObservedField{
			observedField(t, fieldBase, fielddiff.KindMoney,
				values.Value("130000.00 USD"), "2026-05-20T00:00:00Z"),
			observedField(t, fieldGrade, fielddiff.KindEnum,
				values.Value("P3"), "2026-01-05T00:00:00Z"),
			observedField(t, fieldJobCode, fielddiff.KindEnum,
				values.Value("ENG-4"), "2026-08-25T00:00:00Z"),
		},
	}
}

// diffRequest assembles the fixture comparison input.
func diffRequest(t *testing.T, auth dataops.Authorization) dataops.DiffRecordRequest {
	t.Helper()
	page := observationPage(t, "2026-08-31T00:00:00Z", observedSide(t))
	return dataops.DiffRecordRequest{
		Canonical:       canonicalSide(t, auth),
		Observed:        observedSide(t),
		ObservedPresent: true,
		Observation:     page.Watermark(),
		Fields:          diffFields,
		Authorization:   auth,
		Freshness:       freshness(),
		EvaluatedAt:     instantAt(t, evaluatedAt),
	}
}

// TestTodo_DATAOPS_008 is the DATAOPS-008 primary test: every comparison
// returns exactly one of MATCH, MISMATCH, STALE, UNKNOWN, REDACTED or
// NOT_APPLICABLE, with its sources, timestamps and allowed next capabilities,
// and a repair-safe classification that never acts on itself.
func TestTodo_DATAOPS_008(t *testing.T) {
	auth := allowAll(diffFields...)

	t.Run("GREEN: each field is classified with sources, timestamps and next capabilities", func(t *testing.T) {
		diff, err := dataops.DiffRecord(diffRequest(t, auth))
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		if got := fieldsOf(diff.Findings); !equalStrings(got,
			[]string{"assignment.grade", "assignment.job_code", "compensation.base"}) {
			t.Fatalf("findings are not in deterministic field order: %v", got)
		}
		if diff.Verdicts.Total() != len(diff.Findings) || diff.Safety.Total() != len(diff.Findings) {
			t.Fatalf("counts do not partition the findings: %d verdicts, %d safety, %d findings",
				diff.Verdicts.Total(), diff.Safety.Total(), len(diff.Findings))
		}
		for _, f := range diff.Findings {
			if !f.Verdict.Valid() || !f.Safety.Valid() {
				t.Fatalf("%s: verdict=%s safety=%s", f.Field, f.Verdict, f.Safety)
			}
			if f.Reason == "" || f.SafetyReason == "" {
				t.Fatalf("%s carries no reason token", f.Field)
			}
			if f.Observation.Validate() != nil {
				t.Fatalf("%s does not cite the observation it rests on", f.Field)
			}
			if f.CanonicalAuthority.Validate() != nil {
				t.Fatalf("%s does not name the authority for the field", f.Field)
			}
		}
	})

	t.Run("GREEN: authority decides the repair direction", func(t *testing.T) {
		diff, err := dataops.DiffRecord(diffRequest(t, auth))
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		for _, want := range []struct {
			field    dataops.FieldID
			verdict  dataops.Verdict
			relation fielddiff.Relation
			safety   dataops.RepairSafety
			next     []string
		}{
			{fieldGrade, dataops.VerdictMatch, fielddiff.RelationMatch,
				dataops.SafetyNotRequired, nil},
			{fieldBase, dataops.VerdictMismatch, fielddiff.RelationCanonicalAhead,
				dataops.SafetySafe,
				[]string{dataops.CapabilityCreateRepairPlan, dataops.CapabilitySimulateRepair}},
			{fieldJobCode, dataops.VerdictMismatch, fielddiff.RelationExternalAhead,
				dataops.SafetySafe,
				[]string{dataops.CapabilityCreateRepairPlan, dataops.CapabilitySimulateRepair}},
		} {
			finding, ok := diff.Finding(want.field)
			if !ok {
				t.Fatalf("no finding for %s", want.field)
			}
			if finding.Verdict != want.verdict || finding.Relation != want.relation ||
				finding.Safety != want.safety {
				t.Fatalf("%s: verdict=%s relation=%s safety=%s, want %s/%s/%s",
					want.field, finding.Verdict, finding.Relation, finding.Safety,
					want.verdict, want.relation, want.safety)
			}
			if !equalStrings(sortedCopy(finding.AllowedNext), sortedCopy(want.next)) {
				t.Fatalf("%s allowed next = %v, want %v", want.field,
					finding.AllowedNext, want.next)
			}
		}
	})

	t.Run("GREEN: an externally mastered field the projection is ahead of is unsafe", func(t *testing.T) {
		// The incumbent masters the job code. If our projection somehow carries
		// a newer value than its master, refreshing is not a mechanical fix.
		req := diffRequest(t, auth)
		for i := range req.Observed.Fields {
			if req.Observed.Fields[i].Field == fieldJobCode {
				req.Observed.Fields[i].UpdatedAt = instantAt(t, "2025-06-01T00:00:00Z")
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, _ := diff.Finding(fieldJobCode)
		if finding.Safety != dataops.SafetyUnsafe ||
			finding.SafetyReason != dataops.ReasonAuthorityExternalCanonicalAhead {
			t.Fatalf("safety=%s reason=%q, want REPAIR_UNSAFE/%s",
				finding.Safety, finding.SafetyReason, dataops.ReasonAuthorityExternalCanonicalAhead)
		}
	})

	t.Run("RED: a stale observation is reported as a mismatch", func(t *testing.T) {
		req := diffRequest(t, auth)
		page := observationPage(t, "2026-08-28T00:00:00Z", observedSide(t))
		req.Observation = page.Watermark()
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		for _, field := range []dataops.FieldID{fieldBase, fieldJobCode} {
			finding, _ := diff.Finding(field)
			if finding.Verdict != dataops.VerdictStale ||
				finding.Reason != dataops.ReasonObservationStale {
				t.Fatalf("%s: verdict=%s reason=%q, want STALE", field, finding.Verdict, finding.Reason)
			}
			if finding.Safety != dataops.SafetyUndecidable {
				t.Fatalf("%s: a stale comparison was classified %s", field, finding.Safety)
			}
			if !equalStrings(finding.AllowedNext, []string{dataops.CapabilityDetectDrift}) {
				t.Fatalf("%s allowed next = %v on a stale observation", field, finding.AllowedNext)
			}
		}
		// Agreement does not go out of date: a stale page that agrees is still
		// a MATCH.
		grade, _ := diff.Finding(fieldGrade)
		if grade.Verdict != dataops.VerdictMatch {
			t.Fatalf("a stale but agreeing field was reported %s", grade.Verdict)
		}
	})

	t.Run("RED: an unknown or unavailable side is reported as a mismatch", func(t *testing.T) {
		for _, unreadable := range []values.Presence[string]{
			values.Unknown[string]("source_did_not_return_the_field"),
			values.Unavailable[string]("source_timed_out"),
		} {
			req := diffRequest(t, auth)
			for i := range req.Observed.Fields {
				if req.Observed.Fields[i].Field == fieldBase {
					req.Observed.Fields[i].Value = unreadable
				}
			}
			diff, err := dataops.DiffRecord(req)
			if err != nil {
				t.Fatalf("DiffRecord: %v", err)
			}
			finding, _ := diff.Finding(fieldBase)
			if finding.Verdict != dataops.VerdictUnknown {
				t.Fatalf("%s side reported as %s, want UNKNOWN", unreadable.State(), finding.Verdict)
			}
			if finding.Safety != dataops.SafetyUndecidable {
				t.Fatalf("an undecidable comparison was classified %s", finding.Safety)
			}
		}
	})

	t.Run("GREEN: a not-applicable field is neither a match nor a mismatch", func(t *testing.T) {
		req := diffRequest(t, auth)
		for i := range req.Observed.Fields {
			if req.Observed.Fields[i].Field == fieldBase {
				req.Observed.Fields[i].Value =
					values.NotApplicable[string]("contractor_has_no_base_salary")
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, _ := diff.Finding(fieldBase)
		if finding.Verdict != dataops.VerdictNotApplicable ||
			finding.Safety != dataops.SafetyNotRequired {
			t.Fatalf("verdict=%s safety=%s, want NOT_APPLICABLE/NO_REPAIR_REQUIRED",
				finding.Verdict, finding.Safety)
		}
	})

	t.Run("GREEN: a type disagreement is a mapping defect, not a data defect", func(t *testing.T) {
		req := diffRequest(t, auth)
		for i := range req.Observed.Fields {
			if req.Observed.Fields[i].Field == fieldGrade {
				req.Observed.Fields[i].Kind = fielddiff.KindString
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, _ := diff.Finding(fieldGrade)
		if finding.Relation != fielddiff.RelationTypeMismatch ||
			finding.Safety != dataops.SafetyUnsafe ||
			finding.SafetyReason != dataops.ReasonTypeMismatch {
			t.Fatalf("relation=%s safety=%s reason=%q, want TYPE_MISMATCH/REPAIR_UNSAFE",
				finding.Relation, finding.Safety, finding.SafetyReason)
		}
	})

	t.Run("GREEN: a disagreement nothing orders is a conflict and never repair-safe", func(t *testing.T) {
		req := diffRequest(t, auth)
		for i := range req.Observed.Fields {
			if req.Observed.Fields[i].Field == fieldBase {
				req.Observed.Fields[i].UpdatedAt = values.Instant{}
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, _ := diff.Finding(fieldBase)
		if finding.Relation != fielddiff.RelationConflict ||
			finding.Safety != dataops.SafetyUnsafe {
			t.Fatalf("relation=%s safety=%s, want CONFLICT/REPAIR_UNSAFE",
				finding.Relation, finding.Safety)
		}
	})

	t.Run("GREEN: a source that said nothing is UNKNOWN, not a mismatch", func(t *testing.T) {
		req := diffRequest(t, auth)
		req.ObservedPresent = false
		req.Observed = dataops.ObservedRecord{}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		if diff.Verdicts.Unknown != len(diffFields) {
			t.Fatalf("unknown count = %d, want %d", diff.Verdicts.Unknown, len(diffFields))
		}
		for _, f := range diff.Findings {
			if f.Reason != dataops.ReasonNoObservation {
				t.Fatalf("%s reason = %q", f.Field, f.Reason)
			}
		}
	})

	t.Run("GREEN: the comparison is pure and reproducible", func(t *testing.T) {
		req := diffRequest(t, auth)
		first, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		second, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord replay: %v", err)
		}
		if first.Digest != second.Digest {
			t.Fatalf("digest is not reproducible: %s vs %s", first.Digest, second.Digest)
		}
		// Reordering the caller's projection must not move the answer.
		shuffled := req
		shuffled.Fields = []dataops.FieldID{fieldJobCode, fieldBase, fieldGrade}
		third, err := dataops.DiffRecord(shuffled)
		if err != nil {
			t.Fatalf("DiffRecord reordered: %v", err)
		}
		if third.Digest != first.Digest {
			t.Fatal("the digest depends on the order the caller listed its fields")
		}
	})

	t.Run("RED: the comparison reads a clock", func(t *testing.T) {
		req := diffRequest(t, auth)
		req.EvaluatedAt = values.Instant{}
		if _, err := dataops.DiffRecord(req); !errors.Is(err, dataops.ErrEvaluationTime) {
			t.Fatalf("err = %v, want ErrEvaluationTime", err)
		}
		req = diffRequest(t, auth)
		req.EvaluatedAt = instantAt(t, "2020-01-01T00:00:00Z")
		if _, err := dataops.DiffRecord(req); !errors.Is(err, dataops.ErrEvaluationTime) {
			t.Fatalf("evaluating before the observation: err = %v, want ErrEvaluationTime", err)
		}
	})
}

// TestTodo_DATAOPS_008_Security proves that a denied field is never compared
// and never disclosed, and that repair is never performed by the comparison.
func TestTodo_DATAOPS_008_Security(t *testing.T) {
	t.Run("RED: a denied field is compared or disclosed", func(t *testing.T) {
		auth := denyField(allowAll(diffFields...), fieldBase, "compensation_restricted")
		req := diffRequest(t, auth)
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, ok := diff.Finding(fieldBase)
		if !ok {
			t.Fatal("the denied field was dropped instead of being named")
		}
		if finding.Verdict != dataops.VerdictRedacted || finding.Access != dataops.AccessDenied {
			t.Fatalf("verdict=%s access=%s, want REDACTED/DENIED", finding.Verdict, finding.Access)
		}
		if finding.Relation != fielddiff.RelationUnspecified {
			t.Fatalf("a denied field was compared: relation=%s", finding.Relation)
		}
		if finding.CanonicalAuthority.Validate() == nil || finding.Observation.Validate() == nil {
			t.Fatal("a denied field disclosed its authority or its observation provenance")
		}
		if len(finding.AllowedNext) != 0 {
			t.Fatalf("a denied field offered next capabilities: %v", finding.AllowedNext)
		}
		raw := finding.Canonical()
		if raw == nil {
			t.Fatal("the denied finding has no canonical encoding")
		}
		for _, leaked := range []string{
			"135000.00 USD", "130000.00 USD", externalSystem, authorityPol, sourceSchema,
		} {
			if bytes.Contains(raw, []byte(leaked)) {
				t.Fatalf("the denied finding leaked %q", leaked)
			}
		}
	})

	t.Run("RED: any finding carries a compared value", func(t *testing.T) {
		auth := allowAll(diffFields...)
		diff, err := dataops.DiffRecord(diffRequest(t, auth))
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		raw := diff.Canonical()
		if raw == nil {
			t.Fatal("diff has no canonical encoding")
		}
		for _, value := range []string{"135000.00 USD", "130000.00 USD", "ENG-3", "ENG-4", "P3"} {
			if bytes.Contains(raw, []byte(value)) {
				t.Fatalf("a finding carried the compared value %q", value)
			}
		}
	})

	t.Run("RED: a mismatch is repaired without an authority proof", func(t *testing.T) {
		// The comparison is pure. Its only output about repair is a
		// classification, and a safe classification still points at a separate
		// governed capability rather than performing anything.
		auth := allowAll(diffFields...)
		before := diffRequest(t, auth)
		diff, err := dataops.DiffRecord(before)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		after, err := dataops.DiffRecord(before)
		if err != nil {
			t.Fatalf("DiffRecord replay: %v", err)
		}
		if diff.Digest != after.Digest {
			t.Fatal("the comparison changed the state it compared")
		}
		for _, f := range diff.Mismatches() {
			if f.Safety == dataops.SafetySafe &&
				!containsString(f.AllowedNext, dataops.CapabilityCreateRepairPlan) {
				t.Fatalf("%s is repair-safe but names no governed next capability", f.Field)
			}
		}
	})

	t.Run("RED: an unattributed field is classified repair-safe", func(t *testing.T) {
		// A derived value has no authority of its own, so no mismatch on it is
		// ever mechanically repairable.
		auth := allowAll(diffFields...)
		req := diffRequest(t, auth)
		for i := range req.Canonical.Fields {
			if req.Canonical.Fields[i].Field == fieldBase {
				req.Canonical.Fields[i].Authority.Kind = derivedAuthorityKind()
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		finding, _ := diff.Finding(fieldBase)
		if finding.Safety != dataops.SafetyUnsafe ||
			finding.SafetyReason != dataops.ReasonDerivedNotRepairable {
			t.Fatalf("safety=%s reason=%q, want REPAIR_UNSAFE/%s",
				finding.Safety, finding.SafetyReason, dataops.ReasonDerivedNotRepairable)
		}
	})
}

// FuzzTodo_DATAOPS_008 drives the observed side with arbitrary values, kinds
// and timestamps. No input may panic, every field must receive exactly one
// verdict, and no epistemic gap may be reported as a mismatch.
func FuzzTodo_DATAOPS_008(f *testing.F) {
	f.Add("130000.00 USD", "MONEY", "2026-05-20T00:00:00Z", true, int64(86400))
	f.Add("", "", "", false, int64(0))
	f.Add("135000.00 USD", "STRING", "", true, int64(1))
	f.Add("\x00\xff", "ENUM", "not-a-time", false, int64(-5))

	f.Fuzz(func(t *testing.T, value, kind, updated string, present bool, maxAge int64) {
		auth := allowAll(diffFields...)
		req := diffRequest(t, auth)
		req.ObservedPresent = present
		req.Freshness = dataops.FreshnessPolicy{Version: "fuzz/1", MaxAgeSeconds: maxAge}
		for i := range req.Observed.Fields {
			if req.Observed.Fields[i].Field != fieldBase {
				continue
			}
			req.Observed.Fields[i].Value = values.Value(value)
			req.Observed.Fields[i].Kind = fielddiff.ValueKind(kind)
			req.Observed.Fields[i].UpdatedAt = values.Instant{}
			if parsed, err := parseInstant(updated); err == nil {
				req.Observed.Fields[i].UpdatedAt = parsed
			}
		}
		diff, err := dataops.DiffRecord(req)
		if err != nil {
			return
		}
		if len(diff.Findings) != len(diffFields) {
			t.Fatalf("%d finding(s) for %d field(s)", len(diff.Findings), len(diffFields))
		}
		if diff.Verdicts.Total() != len(diff.Findings) {
			t.Fatalf("verdict counts do not partition the findings")
		}
		for _, finding := range diff.Findings {
			if !finding.Verdict.Valid() {
				t.Fatalf("%s has no verdict", finding.Field)
			}
			if finding.Verdict == dataops.VerdictMismatch && finding.Safety == dataops.SafetyUndecidable {
				t.Fatalf("%s is a mismatch and undecidable at once", finding.Field)
			}
			if finding.Safety == dataops.SafetySafe && finding.Verdict != dataops.VerdictMismatch {
				t.Fatalf("%s is repair-safe without a mismatch", finding.Field)
			}
		}
		if diff.Digest == "" {
			t.Fatal("a completed comparison produced no digest")
		}
	})
}

// containsString reports whether a slice contains a value.
func containsString(in []string, want string) bool {
	for _, v := range in {
		if v == want {
			return true
		}
	}
	return false
}
