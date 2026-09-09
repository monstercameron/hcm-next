package importing_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// validationMappingSpec compiles a mapping with one REQUIRED string target
// (identity), one REQUIRED decimal target, and one OPTIONAL string target, so
// the validator's presence, transform-failure and duplicate-identity rules
// can each be exercised independently.
func validationMappingSpec() importing.MappingSpecInput {
	return importing.MappingSpecInput{
		Version: "mapping.acme.validate/v1",
		Fields: []importing.FieldMapping{
			{
				SourceColumn: "id_raw",
				Target:       model.PropertyRef("job.title"), // REQUIRED string.
				Transform:    importing.TransformSpec{Kind: importing.TransformTrim},
				IsIdentity:   true,
			},
			{
				SourceColumn: "amount_raw",
				Target:       model.PropertyRef("compensation_component.amount"), // REQUIRED decimal.
				Transform:    importing.TransformSpec{Kind: importing.TransformMoneyParse, Currency: "USD"},
			},
			{
				SourceColumn: "band_raw",
				Target:       model.PropertyRef("position.pay_band_ref"), // OPTIONAL string.
				Transform: importing.TransformSpec{
					Kind:             importing.TransformLookup,
					Crosswalk:        map[string]string{"east": "BAND_E"},
					CrosswalkVersion: "crosswalk.region_band/v1",
				},
			},
		},
	}
}

func stageValidationBatch(t *testing.T, records [][]string) importing.Batch {
	t.Helper()
	header := []string{"id_raw", "amount_raw", "band_raw"}
	b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header, records)
	if err != nil {
		t.Fatalf("StageBatch: %v", err)
	}
	return b
}

func TestTodo_DATAOPS_004(t *testing.T) {
	reg := testRegistry(t)
	m, err := importing.Compile(reg, validationMappingSpec())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Run("valid rows and invalid rows are partitioned exactly", func(t *testing.T) {
		b := stageValidationBatch(t, [][]string{
			{"Engineer A", "USD 100.00", "east"}, // valid
			{"", "USD 200.00", "east"},           // presence.required_missing on job.title
			{"Engineer C", "not money", "east"},  // transform.money_parse_failed
			{"Engineer D", "USD 50.00", "north"}, // transform.lookup_unresolved (optional field, still an error)
		})
		result, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		if result.Summary.TotalRows != 4 {
			t.Fatalf("TotalRows = %d, want 4", result.Summary.TotalRows)
		}
		if result.Summary.ValidRows != 1 || result.Summary.InvalidRows != 3 {
			t.Fatalf("ValidRows/InvalidRows = %d/%d, want 1/3", result.Summary.ValidRows, result.Summary.InvalidRows)
		}
		if result.Summary.ValidRows+result.Summary.InvalidRows != result.Summary.TotalRows {
			t.Fatal("valid + invalid rows does not sum to total rows")
		}
		if result.Summary.ByRule[importing.RuleRequiredMissing] != 1 {
			t.Fatalf("ByRule[required_missing] = %d, want 1", result.Summary.ByRule[importing.RuleRequiredMissing])
		}
		if result.Summary.ByRule[importing.RuleMoneyParseFailed] != 1 {
			t.Fatalf("ByRule[money_parse_failed] = %d, want 1", result.Summary.ByRule[importing.RuleMoneyParseFailed])
		}
		if result.Summary.ByRule[importing.RuleLookupUnresolved] != 1 {
			t.Fatalf("ByRule[lookup_unresolved] = %d, want 1", result.Summary.ByRule[importing.RuleLookupUnresolved])
		}
	})

	t.Run("stable error identity: identical inputs reproduce identical error sets", func(t *testing.T) {
		records := [][]string{
			{"", "USD 200.00", "east"},
			{"Engineer C", "not money", "east"},
		}
		b := stageValidationBatch(t, records)
		r1, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		r2, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch (rerun): %v", err)
		}
		if len(r1.Errors) == 0 {
			t.Fatal("expected at least one error")
		}
		if len(r1.Errors) != len(r2.Errors) {
			t.Fatalf("error count changed across reruns: %d vs %d", len(r1.Errors), len(r2.Errors))
		}
		for i := range r1.Errors {
			if r1.Errors[i].Identity != r2.Errors[i].Identity {
				t.Fatalf("error identity changed across reruns at %d: %s vs %s",
					i, r1.Errors[i].Identity, r2.Errors[i].Identity)
			}
		}
	})

	// The stable-error-identity RED clause is "row order changes diagnostic
	// identity": which rules a given row's content triggers must be a
	// function of that row's content and the mapping, never of the row's
	// position in the batch or in any internal iteration. A batch's own
	// digest is legitimately order-sensitive (DATAOPS-001), so this compares
	// rule sets by row content across two differently-ordered batches rather
	// than comparing the batch-bound Identity strings directly.
	t.Run("a row's own diagnostic rules do not depend on its position in the batch", func(t *testing.T) {
		records := [][]string{
			{"", "USD 200.00", "east"},
			{"Engineer C", "not money", "east"},
			{"Engineer D", "USD 50.00", "east"},
		}
		reversed := [][]string{records[2], records[1], records[0]}

		b1 := stageValidationBatch(t, records)
		b2 := stageValidationBatch(t, reversed)

		r1, err := importing.ValidateBatch(b1, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		r2, err := importing.ValidateBatch(b2, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch (reordered): %v", err)
		}

		rulesByContent := func(records [][]string, result importing.Result) map[string][]string {
			out := make(map[string][]string, len(records))
			for i, rec := range records {
				var rules []string
				for _, e := range result.Rows[i].Errors {
					rules = append(rules, e.RuleID)
				}
				sort.Strings(rules)
				out[strings.Join(rec, "\x1f")] = rules
			}
			return out
		}
		got1 := rulesByContent(records, r1)
		got2 := rulesByContent(reversed, r2)
		if len(got1) != len(got2) {
			t.Fatalf("distinct row contents differ: %d vs %d", len(got1), len(got2))
		}
		for key, rules := range got1 {
			if !reflect.DeepEqual(rules, got2[key]) {
				t.Fatalf("row %q: rules = %v before reordering, %v after", key, rules, got2[key])
			}
		}
	})

	t.Run("duplicate identity is flagged on both rows, and required-missing identities are not", func(t *testing.T) {
		b := stageValidationBatch(t, [][]string{
			{"Same Title", "USD 100.00", "east"},
			{"Same Title", "USD 200.00", "east"},
			{"", "USD 300.00", "east"}, // empty identity: never counted as a duplicate of anything.
			{"", "USD 400.00", "east"},
		})
		result, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		if got := result.Summary.ByRule[importing.RuleIdentityDuplicate]; got != 2 {
			t.Fatalf("ByRule[identity.duplicate] = %d, want 2 (one per duplicate row)", got)
		}
		for _, rr := range result.Rows[:2] {
			hasDup := false
			for _, e := range rr.Errors {
				if e.RuleID == importing.RuleIdentityDuplicate {
					hasDup = true
				}
			}
			if !hasDup {
				t.Fatalf("row %s: expected an identity.duplicate finding", rr.RowID)
			}
		}
	})

	t.Run("an unmapped column is a batch-level warning, not a row error", func(t *testing.T) {
		header := []string{"id_raw", "amount_raw", "band_raw", "extra_column"}
		b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header,
			[][]string{{"Engineer A", "USD 100.00", "east", "ignored"}})
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		result, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		if len(result.Warnings) != 1 {
			t.Fatalf("Warnings = %d, want 1", len(result.Warnings))
		}
		if result.Warnings[0].RuleID != importing.RuleColumnUnmapped {
			t.Fatalf("warning rule = %s, want %s", result.Warnings[0].RuleID, importing.RuleColumnUnmapped)
		}
		if !result.Rows[0].Valid {
			t.Fatal("an unmapped column should not invalidate the row")
		}
	})

	t.Run("a mapping that reads a column the batch does not have is a structural error", func(t *testing.T) {
		header := []string{"id_raw"} // missing amount_raw and band_raw.
		b, err := importing.StageBatch(csvSource(t, "s3://x"), fixedInstant(t), header, [][]string{{"x"}})
		if err != nil {
			t.Fatalf("StageBatch: %v", err)
		}
		if _, err := importing.ValidateBatch(b, m, reg); err == nil {
			t.Fatal("expected ErrMappingColumnMissing, got nil")
		}
	})

	t.Run("Sample never exceeds the bound", func(t *testing.T) {
		var records [][]string
		for i := 0; i < importing.MaxErrorSample+20; i++ {
			records = append(records, []string{"", "USD 1.00", "east"})
		}
		b := stageValidationBatch(t, records)
		result, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		if len(result.Sample()) != importing.MaxErrorSample {
			t.Fatalf("Sample length = %d, want %d", len(result.Sample()), importing.MaxErrorSample)
		}
	})
}

// TestTodo_DATAOPS_004_Mutation asserts that each rule fires on exactly the
// condition it claims to, by mutating one input at a time away from a known
// all-valid row and confirming exactly the expected rule (and no other)
// appears - a validator that always reports VALID, or that reports every rule
// on every row, would still pass a naive count-based test but fails these.
func TestTodo_DATAOPS_004_Mutation(t *testing.T) {
	reg := testRegistry(t)
	m, err := importing.Compile(reg, validationMappingSpec())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	baseline := []string{"Engineer A", "USD 100.00", "east"}
	assertRules := func(t *testing.T, record []string, wantRules []string) {
		t.Helper()
		b := stageValidationBatch(t, [][]string{record})
		result, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		got := make(map[string]bool)
		for _, e := range result.Rows[0].Errors {
			got[e.RuleID] = true
		}
		if len(got) != len(wantRules) {
			t.Fatalf("record %v: got rules %v, want exactly %v", record, keys(got), wantRules)
		}
		for _, r := range wantRules {
			if !got[r] {
				t.Fatalf("record %v: missing expected rule %s (got %v)", record, r, keys(got))
			}
		}
	}

	t.Run("baseline row is valid", func(t *testing.T) {
		assertRules(t, append([]string(nil), baseline...), nil)
	})

	t.Run("blanking the identity column fires only required_missing", func(t *testing.T) {
		mutated := append([]string(nil), baseline...)
		mutated[0] = ""
		assertRules(t, mutated, []string{importing.RuleRequiredMissing})
	})

	t.Run("corrupting the money column fires only money_parse_failed", func(t *testing.T) {
		mutated := append([]string(nil), baseline...)
		mutated[1] = "not-a-number"
		assertRules(t, mutated, []string{importing.RuleMoneyParseFailed})
	})

	t.Run("an unresolvable lookup fires only lookup_unresolved", func(t *testing.T) {
		mutated := append([]string(nil), baseline...)
		mutated[2] = "does-not-exist"
		assertRules(t, mutated, []string{importing.RuleLookupUnresolved})
	})

	t.Run("two independent defects fire two independent rules", func(t *testing.T) {
		mutated := []string{"", "not-a-number", "east"}
		assertRules(t, mutated, []string{importing.RuleRequiredMissing, importing.RuleMoneyParseFailed})
	})
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func FuzzTodo_DATAOPS_004(f *testing.F) {
	f.Add("", "USD 1.00", "east")
	f.Add("Title", "not money", "east")
	f.Add("Title", "USD 1.00", "nowhere")
	f.Add("Title", "USD 1.00", "east")

	f.Fuzz(func(t *testing.T, idRaw, amountRaw, bandRaw string) {
		reg := testRegistry(t)
		m, err := importing.Compile(reg, validationMappingSpec())
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		b, err := importing.StageBatch(csvSource(t, "s3://fuzz"), fixedInstant(t),
			[]string{"id_raw", "amount_raw", "band_raw"}, [][]string{{idRaw, amountRaw, bandRaw}})
		if err != nil {
			return // Rejected at staging is an allowed outcome.
		}
		r1, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch: %v", err)
		}
		r2, err := importing.ValidateBatch(b, m, reg)
		if err != nil {
			t.Fatalf("ValidateBatch (rerun): %v", err)
		}
		if len(r1.Errors) != len(r2.Errors) {
			t.Fatalf("nondeterministic error count: %d vs %d", len(r1.Errors), len(r2.Errors))
		}
		for i := range r1.Errors {
			if r1.Errors[i].Identity != r2.Errors[i].Identity {
				t.Fatalf("nondeterministic error identity at %d: %s vs %s", i, r1.Errors[i].Identity, r2.Errors[i].Identity)
			}
		}
		if r1.Summary.ValidRows+r1.Summary.InvalidRows != r1.Summary.TotalRows {
			t.Fatal("valid + invalid rows does not sum to total rows")
		}
	})
}
