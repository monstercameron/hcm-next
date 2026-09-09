package eligibility_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ELIG_001 proves the RED and GREEN clauses of planning/todos.md
// ELIG-001: a request missing its subject, program/opportunity/action
// revision, requested/effective interval, jurisdiction, population/fact/rule
// snapshot, purpose or authority is rejected, and a result never omits
// missing facts, reasons, obligations or evidence.
func TestTodo_ELIG_001(t *testing.T) {
	t.Run("RED_missing_required_request_fields", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*eligibility.Request)
			want   error
		}{
			{"subject", func(r *eligibility.Request) { r.Subject = values.EntityRef{} }, eligibility.ErrRequestSubject},
			{"subject matter kind", func(r *eligibility.Request) { r.SubjectMatter.Kind = eligibility.SubjectMatterUnspecified }, eligibility.ErrRequestSubjectMatter},
			{"subject matter revision", func(r *eligibility.Request) { r.SubjectMatter.Revision = "" }, eligibility.ErrRequestSubjectMatter},
			{"requested interval", func(r *eligibility.Request) { r.RequestedInterval = values.EffectiveInterval{} }, eligibility.ErrRequestInterval},
			{"effective interval", func(r *eligibility.Request) { r.EffectiveInterval = values.EffectiveInterval{} }, eligibility.ErrRequestInterval},
			{"jurisdiction", func(r *eligibility.Request) { r.Jurisdiction = "" }, eligibility.ErrRequestJurisdiction},
			{"population snapshot", func(r *eligibility.Request) { r.Snapshots.PopulationSnapshotRef = "" }, eligibility.ErrRequestSnapshots},
			{"fact snapshot", func(r *eligibility.Request) { r.Snapshots.FactSnapshotRef = "" }, eligibility.ErrRequestSnapshots},
			{"rule snapshot", func(r *eligibility.Request) { r.Snapshots.RuleSnapshotRef = "" }, eligibility.ErrRequestSnapshots},
			{"purpose", func(r *eligibility.Request) { r.Purpose = "" }, eligibility.ErrRequestPurpose},
			{"authority", func(r *eligibility.Request) { r.Authority = "" }, eligibility.ErrRequestAuthority},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := validRequest()
				tc.mutate(&req)
				err := req.Validate()
				if err == nil {
					t.Fatalf("expected Validate to reject missing %s", tc.name)
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want wrapping %v", err, tc.want)
				}
			})
		}
	})

	t.Run("GREEN_well_formed_request_validates", func(t *testing.T) {
		req := validRequest()
		if err := req.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if req.Canonical() == nil {
			t.Fatal("a valid request must have a canonical encoding")
		}
	})

	t.Run("RED_result_status_must_be_one_of_four_values", func(t *testing.T) {
		result := validResultTemplate(eligibility.StatusEligible)
		result.Status = 99
		if err := result.Validate(); !errors.Is(err, eligibility.ErrResultStatus) {
			t.Fatalf("error = %v, want ErrResultStatus", err)
		}
	})

	t.Run("RED_result_cannot_omit_reasons", func(t *testing.T) {
		result := validResultTemplate(eligibility.StatusEligible)
		result.Reasons = nil
		if err := result.Validate(); !errors.Is(err, eligibility.ErrResultReasons) {
			t.Fatalf("error = %v, want ErrResultReasons", err)
		}
	})

	t.Run("RED_result_cannot_omit_evidence", func(t *testing.T) {
		result := validResultTemplate(eligibility.StatusEligible)
		result.Evidence = nil
		if err := result.Validate(); !errors.Is(err, eligibility.ErrResultEvidence) {
			t.Fatalf("error = %v, want ErrResultEvidence", err)
		}
	})

	t.Run("RED_conditional_result_cannot_omit_obligations", func(t *testing.T) {
		result := validResultTemplate(eligibility.StatusConditional)
		result.Obligations = nil
		if err := result.Validate(); !errors.Is(err, eligibility.ErrResultObligations) {
			t.Fatalf("error = %v, want ErrResultObligations", err)
		}
	})

	t.Run("RED_unknown_result_cannot_omit_missing_facts", func(t *testing.T) {
		result := validResultTemplate(eligibility.StatusUnknown)
		result.MissingFacts = nil
		if err := result.Validate(); !errors.Is(err, eligibility.ErrResultMissingFacts) {
			t.Fatalf("error = %v, want ErrResultMissingFacts", err)
		}
	})

	t.Run("GREEN_well_formed_result_is_exactly_one_of_four_statuses_and_canonical", func(t *testing.T) {
		for _, status := range []eligibility.Status{
			eligibility.StatusEligible, eligibility.StatusIneligible,
			eligibility.StatusConditional, eligibility.StatusUnknown,
		} {
			result := validResultTemplate(status)
			if err := result.Validate(); err != nil {
				t.Fatalf("status %s: Validate: %v", status, err)
			}
			if result.Canonical() == nil {
				t.Fatalf("status %s: a valid result must have a canonical encoding", status)
			}
		}
	})
}

// validResultTemplate builds a Result that satisfies every status-specific
// requirement, for tests that then knock out one field at a time.
func validResultTemplate(status eligibility.Status) eligibility.Result {
	r := eligibility.Result{
		Status:            status,
		ProgramVersion:    "2026.1",
		FactSnapshotRef:   "fact-snap-1",
		RuleSnapshotRef:   "rule-snap-1",
		EffectiveInterval: fixedInterval(),
		Reasons:           []eligibility.Reason{{Kind: eligibility.ReasonFactPassed, Ref: "grade"}},
		Evidence:          []string{"fact:grade"},
	}
	if status == eligibility.StatusConditional {
		r.Obligations = []eligibility.Obligation{{Reason: eligibility.ObligationRulePartial, Ref: "manager-attestation"}}
	}
	if status == eligibility.StatusUnknown {
		r.MissingFacts = []string{"grade"}
	}
	return r
}

// TestTodo_ELIG_001_Property proves two independently built, identical
// requests produce byte-identical canonical encodings, and a single-field
// difference changes it.
func TestTodo_ELIG_001_Property(t *testing.T) {
	a := validRequest()
	b := validRequest()
	if string(a.Canonical()) != string(b.Canonical()) {
		t.Fatal("two independently built, identical requests produced different canonical bytes")
	}
	mutated := validRequest()
	mutated.Purpose = "a-different-purpose"
	if string(mutated.Canonical()) == string(a.Canonical()) {
		t.Fatal("changing purpose did not change the canonical encoding")
	}
}

// TestTodo_ELIG_001_Golden pins the wire tokens for status and subject-matter
// kind so a silent renumbering is caught by a test diff.
func TestTodo_ELIG_001_Golden(t *testing.T) {
	cases := map[fmt.Stringer]string{
		eligibility.StatusEligible:           "ELIGIBLE",
		eligibility.StatusIneligible:         "INELIGIBLE",
		eligibility.StatusConditional:        "CONDITIONAL",
		eligibility.StatusUnknown:            "UNKNOWN",
		eligibility.SubjectMatterProgram:     "PROGRAM",
		eligibility.SubjectMatterOpportunity: "OPPORTUNITY",
		eligibility.SubjectMatterAction:      "ACTION",
	}
	for value, want := range cases {
		if got := value.String(); got != want {
			t.Fatalf("%#v.String() = %q, want %q", value, got, want)
		}
	}
}

// TestTodo_ELIG_001_Security proves a result never discloses more than its
// own typed fields: Canonical() over an unauthorized (incomplete/invalid)
// result is nil, never a partial encoding a caller could misread as
// authoritative.
func TestTodo_ELIG_001_Security(t *testing.T) {
	result := validResultTemplate(eligibility.StatusConditional)
	result.Obligations = nil // now invalid
	if result.Canonical() != nil {
		t.Fatal("an invalid CONDITIONAL result (no obligations) still produced a canonical encoding")
	}
}

// TestTodo_ELIG_001_Mutation proves every required Request field is load
// bearing independently.
func TestTodo_ELIG_001_Mutation(t *testing.T) {
	fields := map[string]func(*eligibility.Request){
		"subject":            func(r *eligibility.Request) { r.Subject = values.EntityRef{} },
		"subject matter":     func(r *eligibility.Request) { r.SubjectMatter = eligibility.SubjectMatterRef{} },
		"requested interval": func(r *eligibility.Request) { r.RequestedInterval = values.EffectiveInterval{} },
		"effective interval": func(r *eligibility.Request) { r.EffectiveInterval = values.EffectiveInterval{} },
		"jurisdiction":       func(r *eligibility.Request) { r.Jurisdiction = "" },
		"snapshots":          func(r *eligibility.Request) { r.Snapshots = eligibility.Snapshots{} },
		"purpose":            func(r *eligibility.Request) { r.Purpose = "" },
		"authority":          func(r *eligibility.Request) { r.Authority = "" },
	}
	for name, mutate := range fields {
		req := validRequest()
		mutate(&req)
		if err := req.Validate(); err == nil {
			t.Fatalf("zeroing %s was accepted", name)
		}
		if req.Canonical() != nil {
			t.Fatalf("zeroing %s still produced a canonical encoding", name)
		}
	}
}
