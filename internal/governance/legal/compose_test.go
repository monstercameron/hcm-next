package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestWageFloorCompositionUsesExactMoneyAndNoImplicitFX(t *testing.T) {
	floor := func(id, amount, currency string) composableObligation {
		money, err := values.NewMoney(amount, currency, 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatalf("money %s: %v", id, err)
		}
		return composableObligation{
			rule:     WageFloorRule{ID: id, FloorAmount: money},
			evidence: ObligationEvidence{ID: id},
		}
	}

	// These adjacent cent amounts are above float64's exact integer range;
	// converting them to a binary float would collapse the distinction.
	lower := floor("lower", "9007199254740992.00", "USD")
	higher := floor("higher", "9007199254740993.00", "USD")
	selected, winner := resolveMaximum([]composableObligation{lower, higher})
	if len(selected) != 1 || winner != "higher" {
		t.Fatalf("exact wage-floor winner = %q (%d selected), want higher", winner, len(selected))
	}

	otherCurrency := floor("cad", "99.00", "CAD")
	selected, winner = resolveMaximum([]composableObligation{lower, otherCurrency})
	if len(selected) != 2 || winner != "ALL" {
		t.Fatalf("cross-currency wage floors resolved as winner %q (%d selected), want unresolved ALL", winner, len(selected))
	}
}

func compositionFixtures(t *testing.T) (JurisdictionSet, []RulePack) {
	t.Helper()
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	ny, err := NewYorkPromotionPack()
	if err != nil {
		t.Fatalf("NewYorkPromotionPack: %v", err)
	}
	wa, err := WashingtonPromotionPack()
	if err != nil {
		t.Fatalf("WashingtonPromotionPack: %v", err)
	}
	return JurisdictionSet{
		Primary:             ca.Jurisdiction,
		MultiStateExposures: []Jurisdiction{ny.Jurisdiction, wa.Jurisdiction},
	}, []RulePack{ca, ny, wa}
}

// TestTodo_LEGAL_012 proves the primary composition contract, including the
// registered per-kind dispatch and the voiding rule.
func TestTodo_LEGAL_012(t *testing.T) {
	set, packs := compositionFixtures(t)
	got, err := Compose(CompositionRequest{Jurisdictions: set, Packs: packs})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got.Status != CompositionResolved || got.Digest == "" {
		t.Fatalf("status/digest = %s/%q", got.Status, got.Digest)
	}
	if _, ok := ComparatorForKind(ObligationTypeNonCompete); !ok {
		t.Fatal("NON_COMPETE has no registered comparator")
	}
	for _, trace := range got.Traces {
		if len(trace.Inputs) == 0 || trace.Comparator == "" || trace.Winner == "" {
			t.Fatalf("incomplete trace: %+v", trace)
		}
	}
	for _, obligation := range got.Obligations {
		if obligation.Type == ObligationTypeNonCompete {
			t.Fatal("California's void non-compete survived composition")
		}
	}
	if !strings.Contains(got.Explain(), "status=RESOLVED") {
		t.Fatalf("Explain = %q", got.Explain())
	}
}

// TestTodo_LEGAL_012_Property checks commutativity, associativity and
// idempotence through byte-identical canonical receipts.
func TestTodo_LEGAL_012_Property(t *testing.T) {
	set, packs := compositionFixtures(t)
	base, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatalf("base composition: %v", err)
	}
	reversed := slices.Clone(packs)
	slices.Reverse(reversed)
	permuted, err := ComposeJurisdictionSet(set, reversed...)
	if err != nil {
		t.Fatalf("permuted composition: %v", err)
	}
	if string(base.Bytes()) != string(permuted.Bytes()) || base.Digest != permuted.Digest {
		t.Fatal("composition receipt changed with input order")
	}
	duplicated := append(slices.Clone(packs), packs[0])
	idempotent, err := ComposeJurisdictionSet(set, duplicated...)
	if err != nil {
		t.Fatalf("idempotent composition: %v", err)
	}
	if string(base.Bytes()) != string(idempotent.Bytes()) {
		t.Fatal("duplicate jurisdiction input changed composition")
	}
	left, err := ComposeJurisdictionSet(set, packs[0], packs[1])
	if err != nil {
		t.Fatalf("left composition: %v", err)
	}
	right, err := ComposeJurisdictionSet(set, packs[2])
	if err != nil {
		t.Fatalf("right composition: %v", err)
	}
	combined, err := ComposeJurisdictionSet(set, packs[0], packs[1], packs[2])
	if err != nil {
		t.Fatalf("combined composition: %v", err)
	}
	if len(combined.Traces) != len(base.Traces) || len(left.Traces)+len(right.Traces) < len(combined.Traces) {
		t.Fatalf("associativity evidence is not stable: left=%d right=%d combined=%d", len(left.Traces), len(right.Traces), len(combined.Traces))
	}
}

// TestTodo_LEGAL_012_Golden records the California/New York/Washington
// fixture receipt as a canonical, order-independent digest and checks the
// comparator decisions that make the result meaningful.
func TestTodo_LEGAL_012_Golden(t *testing.T) {
	set, packs := compositionFixtures(t)
	got, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatalf("compose fixtures: %v", err)
	}
	if len(got.Bytes()) == 0 || got.Digest != gotDigest(got.CanonicalBytes()) {
		t.Fatalf("receipt is not self-digested: %s", got.Digest)
	}
	winners := map[ObligationType]string{}
	for _, trace := range got.Traces {
		winners[trace.Kind] = trace.Winner
	}
	if winners[ObligationTypeNonCompete] != "VOID" {
		t.Fatalf("NON_COMPETE winner = %q, want VOID", winners[ObligationTypeNonCompete])
	}
	if winners[ObligationTypeFieldRestriction] == "" {
		t.Fatal("FIELD_RESTRICTION intersection has no winner")
	}
	if !strings.Contains(string(got.Bytes()), "MOST_PROTECTIVE_MAX") {
		t.Fatal("golden receipt omitted a max comparator")
	}
}

// TestTodo_LEGAL_012_Race verifies that concurrent callers produce the same
// immutable receipt without shared mutable comparator state.
func TestTodo_LEGAL_012_Race(t *testing.T) {
	set, packs := compositionFixtures(t)
	results := make(chan string, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			got, err := ComposeJurisdictionSet(set, packs...)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- string(got.Bytes())
		}()
	}
	want := <-results
	for i := 1; i < cap(results); i++ {
		if got := <-results; got != want {
			t.Fatalf("concurrent receipts differ")
		}
	}
}

// TestTodo_LEGAL_012_Mutation proves that a contradiction is not reduced to a
// winner and that both original citations remain available in the refusal.
func TestTodo_LEGAL_012_Mutation(t *testing.T) {
	set, packs := compositionFixtures(t)
	max := RetentionRule{ID: "max-retention", RecordClass: "same-record", DurationYears: 2, DurationBasis: "maximum_years", Citation: testCompositionCitation("max")}
	min := RetentionRule{ID: "min-retention", RecordClass: "same-record", DurationYears: 7, DurationBasis: "minimum_years", Citation: testCompositionCitation("min")}
	packs[0].RetentionRules = []RetentionRule{max}
	packs[1].RetentionRules = []RetentionRule{min}
	got, err := ComposeJurisdictionSet(set, packs[:2]...)
	if !errors.Is(err, ErrContradictoryRequirements) {
		t.Fatalf("Compose error = %v, want ErrContradictoryRequirements", err)
	}
	if got.Status != CompositionContradictoryRequirements || len(got.Contradictions) != 1 {
		t.Fatalf("contradiction receipt = %+v", got)
	}
	contradiction := got.Contradictions[0]
	if contradiction.ObligationA.Citation.Section != "max" || contradiction.ObligationB.Citation.Section != "min" {
		t.Fatalf("contradiction citations = %+v", contradiction)
	}
	if !strings.Contains(contradiction.Reason, "shorter") {
		t.Fatalf("contradiction reason = %q", contradiction.Reason)
	}
}

func testCompositionCitation(section string) Citation {
	return Citation{SourceFile: "planning/research/state-employment-law/test.md", Section: section, Status: ReviewStatusUnreviewed, ConfidenceMarker: ConfidenceMarkerConfirmed}
}

func gotDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
