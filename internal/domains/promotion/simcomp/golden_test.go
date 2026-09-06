package simcomp_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
)

// update rewrites the golden tables instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the compensation simulation golden files")

// TestTodo_PROMO_003_Golden pins the whole artifact over the PROMO-001 golden
// fixture: the same harborcare-demo snapshot ready-promotion.txt records, and
// every amount, day count, finding, effect, refusal and digest this simulation
// derives from it.
func TestTodo_PROMO_003_Golden(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))

	// The snapshot this simulation is golden over is byte-for-byte the one
	// PROMO-001's own golden records; if that ever stops being true, the two
	// goldens have drifted apart and this fails first.
	assertSnapshotMatchesPromo001Golden(t, snap)

	result, err := simcomp.Simulate(simulateRequest(t, snap))
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	assertGolden(t, "compensation-ready.txt", renderGolden(result))
}

// TestTodo_PROMO_003_Golden_Withheld pins the same artifact for a caller who
// may not see the subject's pay or the compensation pool. It sits beside the
// ready table so that what a denial changes -- and what it must not change --
// is a diff between two committed files.
func TestTodo_PROMO_003_Golden_Withheld(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Compensation.SubjectDisclosable = false
	req.Authorization.Compensation.SubjectDenialReason = "policy:no_compensation_disclosure"
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"

	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with two withheld required inputs was accepted")
	}
	result, err := simcomp.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	assertGolden(t, "compensation-withheld.txt", renderGolden(result))
}

// assertSnapshotMatchesPromo001Golden reads PROMO-001's own golden table and
// checks that this fixture still produces the material digest recorded there.
func assertSnapshotMatchesPromo001Golden(t testing.TB, snap promosnapshot.PromotionInputSnapshot) {
	t.Helper()
	path := filepath.Join("..", "snapshot", "testdata", "golden", "ready-promotion.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the PROMO-001 golden at %s: %v", path, err)
	}
	const marker = "material digest: "
	for line := range strings.SplitSeq(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, marker) {
			continue
		}
		if want := strings.TrimPrefix(line, marker); want != snap.Digest {
			t.Fatalf("this lane's fixture snapshot digests to %s, but the PROMO-001 golden records %s",
				snap.Digest, want)
		}
		return
	}
	t.Fatalf("the PROMO-001 golden at %s records no material digest", path)
}

// renderGolden writes the simulation as a stable, diffable table.
func renderGolden(result simcomp.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tenant: %s\n", result.Tenant)
	fmt.Fprintf(&b, "subject: %s\n", result.Subject)
	fmt.Fprintf(&b, "effective on: %s\n", result.EffectiveOn)
	fmt.Fprintf(&b, "snapshot digest: %s\n", result.SnapshotDigest)
	fmt.Fprintf(&b, "result digest: %s\n", result.Digest)
	fmt.Fprintf(&b, "executable: %t\n", result.Executable())
	fmt.Fprintf(&b, "required approvals: %s\n", orEmpty(strings.Join(result.RequiredApprovals, ",")))

	b.WriteString("\nbase pay:\n")
	fmt.Fprintf(&b, "    evaluated:  %t\n", result.BasePay.Evaluated)
	if result.BasePay.Evaluated {
		fmt.Fprintf(&b, "    current:    %s\n", result.BasePay.CurrentAnnualized)
		fmt.Fprintf(&b, "    desired:    %s\n", result.BasePay.DesiredAnnualized)
		fmt.Fprintf(&b, "    delta:      %s\n", result.BasePay.Delta)
		fmt.Fprintf(&b, "    direction:  %s\n", result.BasePay.Direction)
	}

	b.WriteString("\npay band:\n")
	fmt.Fprintf(&b, "    evaluated:  %t\n", result.Band.Evaluated)
	if result.Band.Evaluated {
		fmt.Fprintf(&b, "    band:       %s@%s\n", result.Band.BandID, result.Band.BandVersion)
		fmt.Fprintf(&b, "    current:    %s\n", orEmpty(result.Band.CurrentClass))
		fmt.Fprintf(&b, "    desired:    %s\n", orEmpty(result.Band.DesiredClass))
		fmt.Fprintf(&b, "    boundary:   %s\n", orEmpty(result.Band.DesiredBoundary))
		fmt.Fprintf(&b, "    approval:   %s\n", orEmpty(result.Band.ApprovalRequirementID))
	}

	b.WriteString("\nproration:\n")
	fmt.Fprintf(&b, "    evaluated:  %t\n", result.Proration.Evaluated)
	if result.Proration.Evaluated {
		fmt.Fprintf(&b, "    period:     %s [%s,%s)\n",
			result.Proration.PeriodID, result.Proration.PeriodStart, result.Proration.PeriodEnd)
		fmt.Fprintf(&b, "    days:       %d = %d prior + %d new\n",
			result.Proration.DaysInPeriod, result.Proration.DaysAtPriorRate, result.Proration.DaysAtNewRate)
		fmt.Fprintf(&b, "    days/year:  %s\n", result.Proration.DaysPerYear)
		fmt.Fprintf(&b, "    prior rate: %s\n", result.Proration.PriorDailyRate)
		fmt.Fprintf(&b, "    new rate:   %s\n", result.Proration.NewDailyRate)
		fmt.Fprintf(&b, "    prior part: %s\n", result.Proration.PriorPortion)
		fmt.Fprintf(&b, "    new part:   %s\n", result.Proration.NewPortion)
		fmt.Fprintf(&b, "    period:     %s (unprorated %s, delta %s)\n",
			result.Proration.PeriodAmount, result.Proration.UnproratedPeriodAmount, result.Proration.PeriodDelta)
	}

	b.WriteString("\nbudget:\n")
	fmt.Fprintf(&b, "    evaluated:  %t\n", result.Budget.Evaluated)
	fmt.Fprintf(&b, "    planned:    %t\n", result.Budget.Planned)
	fmt.Fprintf(&b, "    reason:     %s\n", orEmpty(result.Budget.Reason))
	if result.Budget.Evaluated {
		fmt.Fprintf(&b, "    scope:      %s/%s\n", result.Budget.Scope, result.Budget.Period)
		fmt.Fprintf(&b, "    type/unit:  %s/%s\n", result.Budget.BudgetType, result.Budget.Unit)
		fmt.Fprintf(&b, "    baseline:   %s\n", result.Budget.BaselineVersion)
		fmt.Fprintf(&b, "    available:  %s\n", result.Budget.Available)
		fmt.Fprintf(&b, "    amount:     %s\n", result.Budget.Amount)
		fmt.Fprintf(&b, "    sufficient: %t\n", result.Budget.Sufficient)
	}
	if result.Budget.Planned {
		fmt.Fprintf(&b, "    remaining:  %s\n", result.Budget.Remaining)
		fmt.Fprintf(&b, "    request key: %s\n", result.Budget.Request.IdempotencyKey)
	}

	b.WriteString("\neffects:\n")
	for _, effect := range result.Effects {
		fmt.Fprintf(&b, "- %s\n", effect.Kind)
		fmt.Fprintf(&b, "    effect id:      %s\n", effect.EffectID)
		fmt.Fprintf(&b, "    participant:    %s (%s, local=%t)\n", effect.Participant, effect.StorageClass, effect.Local)
		fmt.Fprintf(&b, "    destination:    %s\n", effect.DestinationRef)
		fmt.Fprintf(&b, "    reversibility:  %s\n", effect.Reversibility)
		fmt.Fprintf(&b, "    compensation:   %s (%s)\n", effect.CompensationRef, effect.CompensationStrategy)
		fmt.Fprintf(&b, "    observation:    %s\n", effect.ObservationRef)
		fmt.Fprintf(&b, "    idempotency:    %s\n", effect.IdempotencyKey)
		fmt.Fprintf(&b, "    baseline:       %s\n", effect.ExpectedRevision)
		fmt.Fprintf(&b, "    derived from:   %s\n", strings.Join(effect.DerivedFrom, ","))
		for _, change := range effect.Changes {
			fmt.Fprintf(&b, "    change %s: %s -> %s (changed=%t, from %s)\n",
				change.Field, orEmpty(change.Before), orEmpty(change.After), change.Changed, change.SourceInput)
		}
	}

	b.WriteString("\nrefusals:\n")
	for _, refusal := range result.Refusals {
		fmt.Fprintf(&b, "- %s: %s\n", refusal.Kind, refusal.Reason)
		fmt.Fprintf(&b, "    input:        %s\n", orEmpty(refusal.InputName))
		fmt.Fprintf(&b, "    availability: %s\n", orEmpty(string(refusal.Availability)))
		fmt.Fprintf(&b, "    detail:       %s\n", refusal.Detail)
	}
	return b.String()
}

func orEmpty(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// assertGolden compares rendered against the committed file, or rewrites it
// under -update.
func assertGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
