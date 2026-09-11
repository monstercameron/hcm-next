package rules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func reevaluateDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	dec, err := values.NewDecimal(text, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return dec
}

func standardInput() PromotionApprovalInput {
	// Poured from the fixture well: 5% in-band with sufficient budget and
	// no grade change resolves STANDARD via the otherwise row.
	return PromotionApprovalInput{
		IncreasePercent: values.MustDecimal("5.0000", 4, values.RoundingHalfEven),
		BandPosition:    BandPositionInBand,
		BudgetAuthority: BudgetAuthoritySufficient,
	}
}

func approveFixture(t *testing.T, table Table, in PromotionApprovalInput) ApprovedPlan {
	t.Helper()
	decision, err := EvaluatePromotionApproval(table, in)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := inputDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	return ApprovedPlan{
		Input: in, InputDigest: digest, Tier: decision.Tier, MatchedRowID: decision.MatchedRowID,
		TableID: decision.TableID, TableVersion: decision.TableVersion, TableDigest: decision.TableDigest,
		ApprovalDigest: "sha256:governance-approval",
	}
}

// TestTodo_RULE_004 is the RULE-004 primary test: the same material
// result confirms the plan, moved inputs require re-approval even when
// the tier holds, and a moved result invalidates for replanning — always
// with both rule versions cited.
func TestTodo_RULE_004(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	approved := approveFixture(t, table, standardInput())
	if approved.Tier != ApprovalTierStandard {
		t.Fatalf("fixture tier = %s, want STANDARD", approved.Tier)
	}

	// Identical inputs under the identical table confirm the plan.
	confirmed, err := ReevaluatePromotionApproval(table, approved, standardInput())
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Verdict != VerdictConfirmed || confirmed.MovedInput || confirmed.MovedTable {
		t.Fatalf("reevaluation = %+v, want clean CONFIRMED", confirmed)
	}
	if confirmed.OriginalTableVer == "" || confirmed.OriginalTableVer != confirmed.CurrentTableVer {
		t.Fatalf("reevaluation loses its version citation: %+v", confirmed)
	}

	// A moved threshold with a held tier still requires re-approval: the
	// old approval set cannot stay valid on moved inputs.
	moved := standardInput()
	moved.IncreasePercent = reevaluateDecimal(t, "6.0000")
	reapprove, err := ReevaluatePromotionApproval(table, approved, moved)
	if err != nil {
		t.Fatal(err)
	}
	if reapprove.Verdict != VerdictReapprovalRequired || !reapprove.MovedInput {
		t.Fatalf("moved-input reevaluation = %+v, want REAPPROVAL_REQUIRED", reapprove)
	}
	if reapprove.Tier != ApprovalTierStandard {
		t.Fatalf("moved tier = %s, want the held STANDARD tier", reapprove.Tier)
	}

	// A moved tier invalidates the plan for replanning.
	rich := standardInput()
	rich.IncreasePercent = reevaluateDecimal(t, "15.0000")
	invalid, err := ReevaluatePromotionApproval(table, approved, rich)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Verdict != VerdictInvalidated || invalid.Tier != ApprovalTierFinanceRequired {
		t.Fatalf("moved-tier reevaluation = %+v, want INVALIDATED at FINANCE_REQUIRED", invalid)
	}

	// Unresolvable inputs invalidate rather than execute.
	unknown := standardInput()
	unknown.BudgetAuthority = BudgetAuthorityUnknown
	blocked, err := ReevaluatePromotionApproval(table, approved, unknown)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Verdict != VerdictInvalidated || blocked.Tier != ApprovalTierUnknownBlocked {
		t.Fatalf("unknown reevaluation = %+v, want INVALIDATED at UNKNOWN_BLOCKED", blocked)
	}

	// A republished table that changes nothing material confirms under
	// the new citation with the original version retained.
	republished := table
	republished.Version = table.Version + "+republished"
	kept, err := ReevaluatePromotionApproval(republished, approved, standardInput())
	if err != nil {
		t.Fatal(err)
	}
	if kept.Verdict != VerdictConfirmed || !kept.MovedTable {
		t.Fatalf("republished reevaluation = %+v, want CONFIRMED with a table move", kept)
	}
	if kept.OriginalTableVer != approved.TableVersion || kept.CurrentTableVer != republished.Version {
		t.Fatalf("version citation = %+v, want original plus current", kept)
	}
}

func TestTodo_RULE_004_Property(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	approved := approveFixture(t, table, standardInput())
	// Verdicts are total: every well-formed input lands in exactly one of
	// the three verdicts, and identical inputs always confirm.
	percents := []string{"0.0000", "5.0000", "9.9999", "10.0001", "15.0000", "25.0000"}
	for _, percent := range percents {
		in := standardInput()
		in.IncreasePercent = reevaluateDecimal(t, percent)
		first, err := ReevaluatePromotionApproval(table, approved, in)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ReevaluatePromotionApproval(table, approved, in)
		if err != nil {
			t.Fatal(err)
		}
		if first.Verdict != second.Verdict || first.Tier != second.Tier {
			t.Fatalf("percent %s is not deterministic: %+v vs %+v", percent, first, second)
		}
		switch first.Verdict {
		case VerdictConfirmed, VerdictReapprovalRequired, VerdictInvalidated:
		default:
			t.Fatalf("percent %s verdict %q is outside the vocabulary", percent, first.Verdict)
		}
	}
	same, err := ReevaluatePromotionApproval(table, approved, standardInput())
	if err != nil || same.Verdict != VerdictConfirmed {
		t.Fatalf("identical inputs = %+v, %v; want CONFIRMED", same, err)
	}
}

func TestTodo_RULE_004_Golden(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	approved := approveFixture(t, table, standardInput())
	confirmed, err := ReevaluatePromotionApproval(table, approved, standardInput())
	if err != nil {
		t.Fatal(err)
	}
	rich := standardInput()
	rich.IncreasePercent = reevaluateDecimal(t, "15.0000")
	invalid, err := ReevaluatePromotionApproval(table, approved, rich)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	writeVerdict(&b, confirmed)
	writeVerdict(&b, invalid)
	got := b.String()
	path := filepath.Join("testdata", "rule004_reevaluation.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func writeVerdict(b *strings.Builder, verdict Reevaluation) {
	b.WriteString("verdict: " + verdict.Verdict + "\n")
	b.WriteString("tier: " + string(verdict.Tier) + " row: " + verdict.MatchedRowID + "\n")
	b.WriteString("original: " + verdict.OriginalTableID + "@" + verdict.OriginalTableVer + "\n")
	b.WriteString("current: " + verdict.CurrentTableID + "@" + verdict.CurrentTableVer + "\n")
	b.WriteString("moved-input: " + boolText(verdict.MovedInput) + " moved-table: " + boolText(verdict.MovedTable) + "\n")
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func FuzzTodo_RULE_004(f *testing.F) {
	f.Add("5.0000", 0, 0, false)
	f.Add("15.0000", 1, 1, true)
	f.Add("0.0000", 3, 2, false)

	bands := []BandPosition{BandPositionBelowMinimum, BandPositionInBand, BandPositionAboveMaximum, BandPositionUnknown}
	budgets := []BudgetAuthority{BudgetAuthoritySufficient, BudgetAuthorityInsufficient, BudgetAuthorityUnknown}

	f.Fuzz(func(t *testing.T, percent string, bandIdx, budgetIdx int, gradeChange bool) {
		if len(percent) > 32 {
			t.Skip("not a threshold this fixture shapes")
		}
		table := PromotionApprovalThresholdTable()
		approved := approveFixture(t, table, standardInput())
		pct, err := values.NewDecimal(percent, 4, values.RoundingHalfEven)
		if err != nil {
			t.Skip("not a decimal input")
		}
		in := PromotionApprovalInput{
			IncreasePercent: pct,
			BandPosition:    bands[((bandIdx%len(bands))+len(bands))%len(bands)],
			BudgetAuthority: budgets[((budgetIdx%len(budgets))+len(budgets))%len(budgets)],
			GradeChange:     gradeChange,
		}
		verdict, err := ReevaluatePromotionApproval(table, approved, in)
		if err != nil {
			// Malformed inputs are caller defects, never panics: only
			// well-formed inputs reach a verdict.
			if !errors.Is(err, ErrPlanTampered) && !errors.Is(err, ErrPromotionInputInvalid) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		switch verdict.Verdict {
		case VerdictConfirmed, VerdictReapprovalRequired, VerdictInvalidated:
		default:
			t.Fatalf("verdict %q is outside the vocabulary", verdict.Verdict)
		}
		if verdict.OriginalTableVer == "" || verdict.CurrentTableVer == "" {
			t.Fatal("verdict drops its version citation")
		}
		again, err := ReevaluatePromotionApproval(table, approved, in)
		if err != nil || again.Verdict != verdict.Verdict || again.Tier != verdict.Tier {
			t.Fatal("reevaluation is not deterministic")
		}
	})
}

func TestTodo_RULE_004_Mutation(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	approved := approveFixture(t, table, standardInput())

	// Mutant 1: stored inputs that do not reproduce their digest are
	// tampered history, not an approval.
	tampered := approved
	tampered.Input.IncreasePercent = reevaluateDecimal(t, "50.0000")
	if _, err := ReevaluatePromotionApproval(table, tampered, standardInput()); !errors.Is(err, ErrPlanTampered) {
		t.Fatalf("tampered plan = %v, want ErrPlanTampered", err)
	}
	// Mutant 2: a plan with no approval binding never executes.
	unbound := approved
	unbound.ApprovalDigest = ""
	if _, err := ReevaluatePromotionApproval(table, unbound, standardInput()); !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("unbound plan = %v, want ErrPlanNotApproved", err)
	}
	// Mutant 3: malformed execution-time inputs are caller defects.
	broken := standardInput()
	broken.BandPosition = ""
	if _, err := ReevaluatePromotionApproval(table, approved, broken); !errors.Is(err, ErrPromotionInputInvalid) {
		t.Fatalf("malformed input = %v, want ErrPromotionInputInvalid", err)
	}
	// Mutant 4: a stricter republished table that moves the tier
	// invalidates instead of confirming.
	strict := table
	for i, row := range strict.Rows {
		if row.ID == "otherwise-standard" {
			strict.Rows[i].Outputs[0] = StringValue(string(ApprovalTierFinanceRequired))
		}
	}
	moved, err := ReevaluatePromotionApproval(strict, approved, standardInput())
	if err != nil {
		t.Fatal(err)
	}
	if moved.Verdict != VerdictInvalidated {
		t.Fatalf("stricter table = %+v, want INVALIDATED", moved)
	}
}
