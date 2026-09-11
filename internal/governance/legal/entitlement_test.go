package legal

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func entitlementPrograms() []EntitlementProgram {
	return []EntitlementProgram{
		{ID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", Eligible: true, EvidenceKnown: true, Weeks: 12, PaidWeeks: 0, Interaction: InteractMostProtective, Evidence: []string{"service:12mo"}, Notices: []string{"notice:rights"}, Effects: []string{"protect:job"}},
		{ID: "cba-topup", Authority: AuthorityCollective, Release: "cba-7", Eligible: true, EvidenceKnown: true, Weeks: 12, PaidWeeks: 6, Interaction: InteractOverlap, Evidence: []string{"cba:art-9"}, Notices: []string{"notice:cba"}, Effects: []string{"pay:topup"}},
		{ID: "company-parental", Authority: AuthorityCompany, Release: "handbook-31", Eligible: true, EvidenceKnown: true, Weeks: 16, PaidWeeks: 8, Interaction: InteractMostProtective, Evidence: []string{"hr:policy-31"}, Notices: []string{"notice:parental"}, Effects: []string{"pay:parental"}},
	}
}

func TestTodo_LEGAL_006(t *testing.T) {
	composition, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", false)
	if err != nil {
		t.Fatalf("ComposeEntitlements: %v", err)
	}
	if composition.State != EntitlementValid {
		t.Fatalf("state = %q", composition.State)
	}
	// Most-protective composition: 16 weeks with 8 paid.
	if composition.ComposedWeeks != 16 || composition.ComposedPaid != 8 {
		t.Fatalf("composed = %d weeks %d paid", composition.ComposedWeeks, composition.ComposedPaid)
	}
	if len(composition.Trace) == 0 {
		t.Fatal("composition carries no calculation trace")
	}
	if err := composition.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Manager approval never changes statutory eligibility.
	approved, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != composition.State || approved.ComposedWeeks != composition.ComposedWeeks {
		t.Fatal("manager approval changed statutory eligibility")
	}
	// Accrued balance survives promotion and demotion off leave.
	for _, change := range []string{"promotion", "demotion"} {
		moved, err := ComposeEntitlements(entitlementPrograms(), 40, false, change, false)
		if err != nil {
			t.Fatalf("ComposeEntitlements(%s): %v", change, err)
		}
		if moved.AccruedBalance != 40 || moved.State != EntitlementValid {
			t.Fatalf("%s: balance=%d state=%q", change, moved.AccruedBalance, moved.State)
		}
	}
	// RED: narrowing company policy reports CONFLICT, never silent success.
	narrowed := entitlementPrograms()
	narrowed[0].Weeks = 12
	narrowed[2].Weeks = 8
	narrowed[2].PaidWeeks = 4
	conflict, err := ComposeEntitlements(narrowed, 40, false, "", false)
	if err != nil {
		t.Fatalf("ComposeEntitlements: %v", err)
	}
	if conflict.State != EntitlementConflict {
		t.Fatalf("narrowing state = %q", conflict.State)
	}
	// Unknown evidence requires review: never approval or denial.
	blind := entitlementPrograms()
	blind[0].EvidenceKnown = false
	review, err := ComposeEntitlements(blind, 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != EntitlementReviewRequired {
		t.Fatalf("unknown evidence state = %q", review.State)
	}
	// Structural faults refuse.
	if _, err := ComposeEntitlements(nil, 0, false, "", false); err == nil {
		t.Fatal("empty composition composed")
	}
	rogue := entitlementPrograms()
	rogue[0].Interaction = "fold"
	if _, err := ComposeEntitlements(rogue, 0, false, "", false); err == nil {
		t.Fatal("collapsed interaction composed")
	}
	rogue = entitlementPrograms()
	rogue[1].ID = "fmla"
	if _, err := ComposeEntitlements(rogue, 0, false, "", false); err == nil {
		t.Fatal("duplicate program composed")
	}
}

func TestTodo_LEGAL_006_Property(t *testing.T) {
	first, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", false)
	if err != nil || first.Digest != second.Digest {
		t.Fatal("composition is not deterministic")
	}
	// State vocabulary stays closed.
	for _, state := range []string{first.State} {
		switch state {
		case EntitlementValid, EntitlementConflict, EntitlementReviewRequired, EntitlementUnknown:
		default:
			t.Fatalf("state %q outside vocabulary", state)
		}
	}
	// Stacking adds; overlap takes the maximum.
	stacked := []EntitlementProgram{
		{ID: "a", Authority: AuthorityCompany, Release: "r1", Eligible: true, EvidenceKnown: true, Weeks: 4, PaidWeeks: 2, Interaction: InteractStack},
		{ID: "b", Authority: AuthorityCompany, Release: "r1", Eligible: true, EvidenceKnown: true, Weeks: 6, PaidWeeks: 1, Interaction: InteractStack},
	}
	stack, err := ComposeEntitlements(stacked, 0, false, "", false)
	if err != nil || stack.ComposedWeeks != 10 || stack.ComposedPaid != 3 {
		t.Fatalf("stack=%+v err=%v", stack, err)
	}
}

func TestTodo_LEGAL_006_Golden(t *testing.T) {
	composition, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"state=" + composition.State,
		"weeks=" + itoa(composition.ComposedWeeks) + " paid=" + itoa(composition.ComposedPaid) + " balance=" + itoa(composition.AccruedBalance),
		"digest=" + composition.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "legal006_entitlement.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func TestTodo_LEGAL_006_Race(t *testing.T) {
	catalog := NewProgramCatalog()
	var wg sync.WaitGroup
	for _, program := range entitlementPrograms() {
		wg.Add(1)
		go func(program EntitlementProgram) {
			defer wg.Done()
			if err := catalog.Register(program); err != nil {
				t.Errorf("Register(%s): %v", program.ID, err)
			}
		}(program)
	}
	wg.Wait()
	const readers = 8
	compositions := make([]Composition, readers)
	errs := make([]error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			compositions[i], errs[i] = ComposeEntitlements(catalog.Snapshot(), 40, false, "", false)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
		if compositions[i].Digest != compositions[0].Digest || compositions[i].State != EntitlementValid {
			t.Fatalf("reader %d diverged: %+v", i, compositions[i])
		}
	}
}

func TestTodo_LEGAL_006_Security(t *testing.T) {
	// Ineligible programs contribute nothing but stay listed with trace.
	programs := entitlementPrograms()
	programs[1].Eligible = false
	composition, err := ComposeEntitlements(programs, 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if composition.State != EntitlementValid || composition.ComposedPaid != 8 {
		t.Fatalf("composition=%+v", composition)
	}
	// Negative balances and incoherent weeks never compose.
	if _, err := ComposeEntitlements(entitlementPrograms(), -1, false, "", false); err == nil {
		t.Fatal("negative balance composed")
	}
	rogue := entitlementPrograms()
	rogue[0].PaidWeeks = 99
	if _, err := ComposeEntitlements(rogue, 0, false, "", false); err == nil {
		t.Fatal("incoherent weeks composed")
	}
	// Forged compositions never verify.
	composition.Programs[0].Weeks = 1
	if err := composition.Verify(); err == nil {
		t.Fatal("forged composition verified")
	}
}

func TestTodo_LEGAL_006_Mutation(t *testing.T) {
	base, err := ComposeEntitlements(entitlementPrograms(), 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Weeks change recomposes with a new seal.
	changed := entitlementPrograms()
	changed[2].Weeks = 20
	recomposed, err := ComposeEntitlements(changed, 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if recomposed.ComposedWeeks != 20 || recomposed.Digest == base.Digest {
		t.Fatalf("recomposed=%+v", recomposed)
	}
	// Evidence loss flips the state to review.
	blinded := entitlementPrograms()
	blinded[2].EvidenceKnown = false
	review, err := ComposeEntitlements(blinded, 40, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != EntitlementReviewRequired {
		t.Fatalf("blinded state = %q", review.State)
	}
	// Balance change re-identifies the composition without forfeiture.
	moved, err := ComposeEntitlements(entitlementPrograms(), 41, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if moved.AccruedBalance != 41 || moved.Digest == base.Digest {
		t.Fatalf("moved=%+v", moved)
	}
}
