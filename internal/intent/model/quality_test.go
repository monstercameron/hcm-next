package model_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func ptrFloat(v float64) *float64 { return &v }

// TestTodo_MODEL_024 is the PRIMARY test for data-quality evaluation
// envelopes.
//
// RED: malformed, impossible, stale or incomplete pilot facts cannot be
// reported as PASS; unknown evidence remains UNKNOWN.
//
// GREEN: evaluator returns status, findings, severity, affected paths,
// source watermark and remediation owner.
func TestTodo_MODEL_024(t *testing.T) {
	asOf := instant(2026, time.June, 1)
	fresh := instant(2026, time.May, 30)
	stale := instant(2025, time.January, 1)

	t.Run("RED", func(t *testing.T) {
		t.Run("malformed range value cannot be PASS", func(t *testing.T) {
			rule := model.QualityRule{
				RuleRef: "quality.pay_rate_range", Kind: model.CheckRange, Severity: model.SeverityBlocking,
				Paths: []string{"assignment.pay_rate"}, Min: ptrFloat(0), Max: ptrFloat(1000),
			}
			facts := map[string]model.QualityFact{
				"assignment.pay_rate": {Present: true, Value: "not-a-number", Watermark: fresh},
			}
			res, err := model.EvaluateRule(rule, facts, asOf, 0, "payroll-ops")
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if res.Status == model.QualityPass {
				t.Fatalf("malformed value reported PASS")
			}
			if res.Status != model.QualityFail {
				t.Fatalf("status = %s, want FAIL for a malformed value", res.Status)
			}
		})

		t.Run("impossible range value cannot be PASS", func(t *testing.T) {
			rule := model.QualityRule{
				RuleRef: "quality.pay_rate_range", Kind: model.CheckRange, Severity: model.SeverityBlocking,
				Paths: []string{"assignment.pay_rate"}, Min: ptrFloat(0), Max: ptrFloat(1000),
			}
			facts := map[string]model.QualityFact{
				"assignment.pay_rate": {Present: true, Value: "5000", Watermark: fresh},
			}
			res, err := model.EvaluateRule(rule, facts, asOf, 0, "payroll-ops")
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if res.Status == model.QualityPass {
				t.Fatalf("out-of-range value reported PASS")
			}
		})

		t.Run("stale evidence remains UNKNOWN, never PASS", func(t *testing.T) {
			rule := model.QualityRule{
				RuleRef: "quality.pay_rate_presence", Kind: model.CheckPresence, Severity: model.SeverityBlocking,
				Paths: []string{"assignment.pay_rate"}, Required: true,
			}
			facts := map[string]model.QualityFact{
				"assignment.pay_rate": {Present: true, Value: "50", Watermark: stale},
			}
			res, err := model.EvaluateRule(rule, facts, asOf, 3600, "payroll-ops")
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if res.Status != model.QualityUnknown {
				t.Fatalf("status = %s, want UNKNOWN for stale evidence", res.Status)
			}
		})

		t.Run("incomplete evidence remains UNKNOWN, never PASS", func(t *testing.T) {
			rule := model.QualityRule{
				RuleRef: "quality.pay_rate_presence", Kind: model.CheckPresence, Severity: model.SeverityBlocking,
				Paths: []string{"assignment.pay_rate"}, Required: true,
			}
			res, err := model.EvaluateRule(rule, map[string]model.QualityFact{}, asOf, 0, "payroll-ops")
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if res.Status != model.QualityUnknown {
				t.Fatalf("status = %s, want UNKNOWN for evidence never reported", res.Status)
			}
		})

		t.Run("composition never coerces UNKNOWN into PASS", func(t *testing.T) {
			passing := model.QualityResult{RuleRef: "a", Status: model.QualityPass}
			unknown := model.QualityResult{RuleRef: "b", Status: model.QualityUnknown}
			composed := model.ComposeQualityResults([]model.QualityResult{passing, unknown})
			if composed.Status != model.QualityUnknown {
				t.Fatalf("composed status = %s, want UNKNOWN to survive composition with PASS", composed.Status)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		cases := []struct {
			name  string
			rule  model.QualityRule
			facts map[string]model.QualityFact
		}{
			{
				"presence", model.QualityRule{
					RuleRef: "quality.presence", Kind: model.CheckPresence, Severity: model.SeverityBlocking,
					Paths: []string{"employment.status"}, Required: true,
				},
				map[string]model.QualityFact{"employment.status": {Present: true, Value: "ACTIVE", Watermark: fresh}},
			},
			{
				"format", model.QualityRule{
					RuleRef: "quality.format", Kind: model.CheckFormat, Severity: model.SeverityWarning,
					Paths: []string{"person.email"}, Pattern: `^[^@]+@[^@]+$`,
				},
				map[string]model.QualityFact{"person.email": {Present: true, Value: "a@example.com", Watermark: fresh}},
			},
			{
				"range", model.QualityRule{
					RuleRef: "quality.range", Kind: model.CheckRange, Severity: model.SeverityBlocking,
					Paths: []string{"assignment.pay_rate"}, Min: ptrFloat(0), Max: ptrFloat(1000),
				},
				map[string]model.QualityFact{"assignment.pay_rate": {Present: true, Value: "42.5", Watermark: fresh}},
			},
			{
				"referential", model.QualityRule{
					RuleRef: "quality.referential", Kind: model.CheckReferential, Severity: model.SeverityBlocking,
					Paths: []string{"position.job_family"}, AllowedValues: []string{"ENG", "OPS"},
				},
				map[string]model.QualityFact{"position.job_family": {Present: true, Value: "ENG", Watermark: fresh}},
			},
			{
				"cross-field", model.QualityRule{
					RuleRef: "quality.cross_field", Kind: model.CheckCrossField, Severity: model.SeverityWarning,
					Paths: []string{"assignment.start_date", "assignment.hire_date"}, Operator: model.CrossFieldEqual,
				},
				map[string]model.QualityFact{
					"assignment.start_date": {Present: true, Value: "2026-01-01", Watermark: fresh},
					"assignment.hire_date":  {Present: true, Value: "2026-01-01", Watermark: fresh},
				},
			},
		}
		const thirtyDaysSeconds = 30 * 24 * 3600
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := model.EvaluateRule(tc.rule, tc.facts, asOf, thirtyDaysSeconds, "hris-data-steward")
				if err != nil {
					t.Fatalf("evaluate %s: %v", tc.name, err)
				}
				if res.Status != model.QualityPass {
					t.Fatalf("%s status = %s, want PASS: %+v", tc.name, res.Status, res.Findings)
				}
				if len(res.Findings) == 0 {
					t.Fatalf("%s produced no findings", tc.name)
				}
				for _, f := range res.Findings {
					if f.Severity != tc.rule.Severity {
						t.Fatalf("%s finding severity = %s, want %s", tc.name, f.Severity, tc.rule.Severity)
					}
					if f.RemediationOwner != "hris-data-steward" {
						t.Fatalf("%s finding carries no remediation owner", tc.name)
					}
					if f.Path == "" {
						t.Fatalf("%s finding carries no affected path", tc.name)
					}
				}
				if !res.SourceWatermark.IsSet() {
					t.Fatalf("%s result carries no source watermark", tc.name)
				}
				if res.Digest == "" {
					t.Fatalf("%s result carries no digest", tc.name)
				}
			})
		}
	})
}

// TestTodo_MODEL_024_Property asserts [ComposeQualityResults] always returns
// the worst status among its inputs (FAIL > UNKNOWN > NOT_APPLICABLE > PASS),
// regardless of input order, and its digest is a pure function of its
// content.
func TestTodo_MODEL_024_Property(t *testing.T) {
	statuses := []model.QualityStatus{model.QualityPass, model.QualityFail, model.QualityUnknown, model.QualityNotApplicable}
	rank := map[model.QualityStatus]int{
		model.QualityPass: 0, model.QualityNotApplicable: 1, model.QualityUnknown: 2, model.QualityFail: 3,
	}
	for i, a := range statuses {
		for j, b := range statuses {
			results := []model.QualityResult{{RuleRef: "a", Status: a}, {RuleRef: "b", Status: b}}
			composed := model.ComposeQualityResults(results)
			want := a
			if rank[b] > rank[a] {
				want = b
			}
			if composed.Status != want {
				t.Fatalf("compose(%v[%d], %v[%d]) = %s, want %s", a, i, b, j, composed.Status, want)
			}
		}
	}

	// Composition is order-independent and its own digest is deterministic.
	r1 := model.QualityResult{RuleRef: "a", Status: model.QualityFail}
	r2 := model.QualityResult{RuleRef: "b", Status: model.QualityPass}
	c1 := model.ComposeQualityResults([]model.QualityResult{r1, r2})
	c2 := model.ComposeQualityResults([]model.QualityResult{r2, r1})
	if c1.Digest != c2.Digest {
		t.Fatalf("compose digest depends on input order: %s vs %s", c1.Digest, c2.Digest)
	}
}

// TestTodo_MODEL_024_Golden pins one evaluation envelope's shape.
func TestTodo_MODEL_024_Golden(t *testing.T) {
	rule := model.QualityRule{
		RuleRef: "quality.pay_rate_range", Kind: model.CheckRange, Severity: model.SeverityBlocking,
		Paths: []string{"assignment.pay_rate"}, Min: ptrFloat(0), Max: ptrFloat(1000),
	}
	facts := map[string]model.QualityFact{
		"assignment.pay_rate": {Present: true, Value: "42.5", Watermark: instant(2026, time.May, 30)},
	}
	res, err := model.EvaluateRule(rule, facts, instant(2026, time.June, 1), 30*24*3600, "payroll-ops")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	goldenJSON(t, "model_024_quality_envelope.json", res)
}

// FuzzTodo_MODEL_024 fuzzes RANGE evaluation: EvaluateRule must never report
// PASS for a value that fails to parse as a number, and must always return
// one of the four declared statuses.
func FuzzTodo_MODEL_024(f *testing.F) {
	f.Add("42.5", true)
	f.Add("not-a-number", true)
	f.Add("", false)
	f.Add("5000", true)
	f.Fuzz(func(t *testing.T, value string, present bool) {
		rule := model.QualityRule{
			RuleRef: "quality.fuzz_range", Kind: model.CheckRange, Severity: model.SeverityBlocking,
			Paths: []string{"fuzz.value"}, Min: ptrFloat(0), Max: ptrFloat(1000),
		}
		facts := map[string]model.QualityFact{
			"fuzz.value": {Present: present, Value: value, Watermark: instant(2026, time.May, 30)},
		}
		res, err := model.EvaluateRule(rule, facts, instant(2026, time.June, 1), 0, "owner")
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if !res.Status.Valid() {
			t.Fatalf("status %q is not one of the four declared statuses", res.Status)
		}
		if _, parseErr := strconv.ParseFloat(value, 64); parseErr != nil && res.Status == model.QualityPass {
			t.Fatalf("unparseable value %q reported PASS", value)
		}
	})
}
