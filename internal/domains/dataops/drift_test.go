package dataops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// driftRequest is the fixture detect_drift request over a two-worker
// population.
func driftRequest(t *testing.T, auth dataops.Authorization) dataops.DetectDriftRequest {
	t.Helper()
	return dataops.DetectDriftRequest{
		Tenant:        subject(t).Tenant,
		Source:        externalSystem,
		Population:    []values.EntityRef{otherSubject(t), subject(t)},
		Fields:        diffFields,
		AsOfEffective: localDate(t, "2026-08-15"),
		AsKnownAt:     knownAt(t, "2026-08-31T00:00:00Z"),
		EvaluatedAt:   instantAt(t, evaluatedAt),
		Authorization: auth,
		Freshness:     freshness(),
	}
}

// driftObservations serves one page covering only the first fixture worker, so
// a run always exercises both an observed and an unobserved subject.
func driftObservations(t *testing.T) *memoryObservations {
	t.Helper()
	return &memoryObservations{
		pages: []dataops.ObservationPage{observationPage(t, "2026-08-31T00:00:00Z", observedSide(t))},
	}
}

// TestDetectDriftReportsBoundedPopulationWithZeroEffects covers the P1A
// read-only slice of hcmnext.operations.detect_drift/v1: a bounded population
// is compared field by field, every classification is carried through to the
// report, and the run writes nothing.
func TestDetectDriftReportsBoundedPopulationWithZeroEffects(t *testing.T) {
	ctx := context.Background()
	auth := allowAll(diffFields...)

	t.Run("GREEN: every subject is examined and every field classified", func(t *testing.T) {
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), driftRequest(t, auth))
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		if report.IntentType != dataops.DetectDriftIntentType ||
			report.IntentVersion != dataops.DetectDriftIntentVersion {
			t.Fatalf("intent identity = %s/%s", report.IntentType, report.IntentVersion)
		}
		if report.PopulationExamined != 2 || report.PopulationRequested != 2 || report.Truncated {
			t.Fatalf("examined=%d requested=%d truncated=%t",
				report.PopulationExamined, report.PopulationRequested, report.Truncated)
		}
		want := len(diffFields) * 2
		if report.Verdicts.Total() != want || report.Safety.Total() != want {
			t.Fatalf("counts do not partition %d comparison(s): verdicts=%d safety=%d",
				want, report.Verdicts.Total(), report.Safety.Total())
		}
		if len(report.Watermarks) != 1 || report.Watermarks[0].Source != externalSystem {
			t.Fatalf("the report does not cite the observation pages it read: %+v", report.Watermarks)
		}
	})

	t.Run("GREEN: an unobserved subject is a coverage gap, not drift", func(t *testing.T) {
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), driftRequest(t, auth))
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		missing, ok := report.Drift(otherSubject(t))
		if !ok {
			t.Fatal("the unobserved subject was dropped from the report")
		}
		if missing.Observed {
			t.Fatal("a subject with no source record was reported as observed")
		}
		for _, f := range missing.Diff.Findings {
			if f.Verdict != dataops.VerdictUnknown || f.Reason != dataops.ReasonNoObservation {
				t.Fatalf("%s: verdict=%s reason=%q, want UNKNOWN/%s",
					f.Field, f.Verdict, f.Reason, dataops.ReasonNoObservation)
			}
			if f.Observation.Validate() != nil {
				t.Fatal("an unobserved subject does not cite the read that failed to find it")
			}
		}
	})

	t.Run("GREEN: the report binds each subject to the history it projected from", func(t *testing.T) {
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), driftRequest(t, auth))
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		for _, s := range report.Subjects {
			if s.HistoryDigest == "" || s.Diff.Digest == "" {
				t.Fatalf("%s does not bind its history and comparison digests", s.Subject)
			}
		}
	})

	t.Run("GREEN: the population ceiling truncates and says so", func(t *testing.T) {
		req := driftRequest(t, auth)
		req.MaxPopulation = 1
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), req)
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		if !report.Truncated || report.PopulationExamined != 1 || report.PopulationRequested != 2 {
			t.Fatalf("truncated=%t examined=%d requested=%d",
				report.Truncated, report.PopulationExamined, report.PopulationRequested)
		}
		found := false
		for _, line := range report.Narrative {
			if len(line) > 0 && line[0] == 'p' && contains(line, "truncated") {
				found = true
			}
		}
		if !found {
			t.Fatalf("a truncated report does not say so in its narrative: %v", report.Narrative)
		}
	})

	t.Run("GREEN: the run is reproducible and writes nothing", func(t *testing.T) {
		first, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), driftRequest(t, auth))
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		second, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), driftRequest(t, auth))
		if err != nil {
			t.Fatalf("DetectDrift replay: %v", err)
		}
		if first.ResultDigest != second.ResultDigest {
			t.Fatal("the drift report is not reproducible")
		}
		if !first.Effects.IsZero() {
			t.Fatalf("the drift run counted effects: %v", first.Effects.NonZero())
		}
		if err := first.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if first.Receipt.ExecutionState != "NOT_PLANNED" {
			t.Fatalf("execution state = %q", first.Receipt.ExecutionState)
		}
	})

	t.Run("GREEN: the observation port is asked only for the requested population", func(t *testing.T) {
		reader := driftObservations(t)
		if _, err := dataops.DetectDrift(ctx, corpus(t), reader, driftRequest(t, auth)); err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		if reader.Calls != 1 {
			t.Fatalf("the source was read %d time(s) for a single exhausted page", reader.Calls)
		}
		if len(reader.LastQuery.Subjects) != 2 {
			t.Fatalf("the source was asked about %d subject(s)", len(reader.LastQuery.Subjects))
		}
		if len(reader.LastQuery.Fields) != len(diffFields) {
			t.Fatalf("the source was asked for %d field(s)", len(reader.LastQuery.Fields))
		}
	})

	t.Run("RED: a source that answers outside the population is trusted", func(t *testing.T) {
		req := driftRequest(t, auth)
		req.Population = []values.EntityRef{otherSubject(t)}
		reader := driftObservations(t) // the page carries the other worker
		_, err := dataops.DetectDrift(ctx, corpus(t), reader, req)
		if !errors.Is(err, dataops.ErrPortWidenedProjection) {
			t.Fatalf("err = %v, want ErrPortWidenedProjection", err)
		}
	})

	t.Run("RED: a source whose cursor never terminates hangs the run", func(t *testing.T) {
		first := observationPage(t, "2026-08-31T00:00:00Z", observedSide(t))
		first.NextCursor = "c1"
		// The second page points back at itself, so the cursor never
		// terminates. The run must stop rather than loop.
		second := first
		second.Cursor = "c1"
		second.NextCursor = "c1"
		second.Records = nil
		reader := &memoryObservations{pages: []dataops.ObservationPage{first, second}}
		_, err := dataops.DetectDrift(ctx, corpus(t), reader, driftRequest(t, auth))
		if !errors.Is(err, dataops.ErrObservationPaging) {
			t.Fatalf("err = %v, want ErrObservationPaging", err)
		}
	})

	t.Run("RED: an unbounded population is accepted", func(t *testing.T) {
		req := driftRequest(t, auth)
		req.MaxPopulation = dataops.MaxPopulationCeiling + 1
		if _, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), req); !errors.Is(err, dataops.ErrPopulationBound) {
			t.Fatalf("err = %v, want ErrPopulationBound", err)
		}
		req = driftRequest(t, auth)
		req.Population = nil
		if _, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), req); !errors.Is(err, dataops.ErrPopulationEmpty) {
			t.Fatalf("err = %v, want ErrPopulationEmpty", err)
		}
	})

	t.Run("RED: a withheld population leaks its membership", func(t *testing.T) {
		req := driftRequest(t, withheldSubject(auth, "population_out_of_scope"))
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), req)
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		if report.Disclosure != dataops.DisclosureWithheld {
			t.Fatalf("disclosure = %s, want WITHHELD", report.Disclosure)
		}
		if len(report.Subjects) != 0 || report.PopulationExamined != 0 {
			t.Fatalf("a withheld report carried %d subject(s)", len(report.Subjects))
		}
		if report.Verdicts.Total() != 0 {
			t.Fatal("a withheld report carried comparison counts")
		}
	})

	t.Run("RED: a denied field is compared inside a drift run", func(t *testing.T) {
		req := driftRequest(t, denyField(auth, fieldBase, "compensation_restricted"))
		report, err := dataops.DetectDrift(ctx, corpus(t), driftObservations(t), req)
		if err != nil {
			t.Fatalf("DetectDrift: %v", err)
		}
		if report.Disclosure != dataops.DisclosurePartial {
			t.Fatalf("disclosure = %s, want PARTIAL", report.Disclosure)
		}
		if report.Verdicts.Redacted != 2 {
			t.Fatalf("redacted count = %d, want one per subject", report.Verdicts.Redacted)
		}
		for _, s := range report.Subjects {
			finding, ok := s.Diff.Finding(fieldBase)
			if !ok || finding.Verdict != dataops.VerdictRedacted {
				t.Fatalf("%s: the denied field was not reported as REDACTED", s.Subject)
			}
		}
	})
}

// contains reports whether haystack contains needle.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
