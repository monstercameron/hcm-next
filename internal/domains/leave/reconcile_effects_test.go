package leave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

func effectSet() []ExternalEffect {
	return []ExternalEffect{
		{System: "payroll", Mandatory: true, FreshnessTick: 100, DeadlineTick: 200, State: EffectPass, Observation: "obs:payroll-1", Owner: "payroll-ops"},
		{System: "benefits", Mandatory: true, FreshnessTick: 100, DeadlineTick: 200, State: EffectUnknown, Owner: "benefits-ops"},
		{System: "wfm", Mandatory: false, FreshnessTick: 100, DeadlineTick: 200, State: EffectPass, Observation: "obs:wfm-1", Owner: "wfm-ops"},
	}
}

func TestTodo_LEAVE_011(t *testing.T) {
	dimensions, err := ReconcileEffects(effectSet())
	if err != nil {
		t.Fatalf("ReconcileEffects: %v", err)
	}
	// Payroll and WFM pass while benefits stays unknown: no collapse.
	bySystem := map[string]ExternalEffect{}
	for _, effect := range dimensions.Effects {
		bySystem[effect.System] = effect
	}
	if bySystem["payroll"].State != EffectPass || bySystem["benefits"].State != EffectUnknown || bySystem["wfm"].State != EffectPass {
		t.Fatalf("effects=%+v", dimensions.Effects)
	}
	if dimensions.Business != BusinessLeaveActive || dimensions.ConsistencyState != "DEGRADED" || dimensions.ObligationState != "PENDING" {
		t.Fatalf("dimensions=%+v", dimensions)
	}
	if err := dimensions.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Targeted repair proceeds without a second local leave transaction.
	plan, err := dimensions.RepairPlan("benefits")
	if err != nil || plan == "" {
		t.Fatalf("plan=%q err=%v", plan, err)
	}
	if _, err := dimensions.RepairPlan("payroll"); err == nil {
		t.Fatal("passing effect repaired")
	}
	// RED: provider acceptance without observation never passes, and
	// unavailable observations never roll back local leave.
	accepted := effectSet()
	accepted[0].Observation = ""
	accepted[0].ProviderAccepted = true
	held, err := ReconcileEffects(accepted)
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range held.Effects {
		if effect.System == "payroll" && effect.State == EffectPass {
			t.Fatal("provider acceptance reported pass")
		}
	}
	if held.Business != BusinessLeaveActive {
		t.Fatal("unavailable observation rolled back valid leave")
	}
	if _, err := ReconcileEffects(nil); err == nil {
		t.Fatal("empty effects reconciled")
	}
}

func TestTodo_LEAVE_011_Property(t *testing.T) {
	first, err := ReconcileEffects(effectSet())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileEffects(effectSet())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("reconciliation is not deterministic")
	}
	// All-pass holds every dimension green.
	healthy := effectSet()
	healthy[1].State = EffectPass
	healthy[1].Observation = "obs:benefits-1"
	green, err := ReconcileEffects(healthy)
	if err != nil {
		t.Fatal(err)
	}
	if green.ConsistencyState != "OK" || green.ObligationState != "SATISFIED" || len(green.RepairTargets) != 0 {
		t.Fatalf("green=%+v", green)
	}
	// Optional failures degrade consistency but hold no obligation.
	optional := effectSet()
	optional[1].State = EffectPass
	optional[1].Observation = "obs:benefits-1"
	optional[2].State = EffectFailed
	amber, err := ReconcileEffects(optional)
	if err != nil {
		t.Fatal(err)
	}
	if amber.ConsistencyState != "DEGRADED" || amber.ObligationState != "SATISFIED" {
		t.Fatalf("amber=%+v", amber)
	}
}

func TestTodo_LEAVE_011_Golden(t *testing.T) {
	dimensions, err := ReconcileEffects(effectSet())
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"business=" + dimensions.Business,
		"consistency=" + dimensions.ConsistencyState,
		"obligation=" + dimensions.ObligationState,
		"repair=" + strings.Join(dimensions.RepairTargets, ","),
		"digest=" + dimensions.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave011_effects.golden")
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

func TestTodo_LEAVE_011_Integration(t *testing.T) {
	// A failed benefits observation reconciles through the real delivery
	// boundary into bounded fallback work while leave stays active.
	reconciliation, err := delivery.Reconcile("intent-leave-11", "worker:w1", "INTERNAL", "benefits-observation", delivery.FailureOutage, 2, 100, delivery.RetryPolicy{MaxAttempts: 5, BackoffTicks: 30, FallbackAfter: 3})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if reconciliation.Outcome != delivery.ReconcileFallbackInbox {
		t.Fatalf("reconciliation=%+v", reconciliation)
	}
	effects := effectSet()
	effects[1].State = EffectFailed
	dimensions, err := ReconcileEffects(effects)
	if err != nil {
		t.Fatal(err)
	}
	if dimensions.Business != BusinessLeaveActive || dimensions.ObligationState != "PENDING" {
		t.Fatalf("dimensions=%+v", dimensions)
	}
	plan, err := dimensions.RepairPlan("benefits")
	if err != nil || plan == "" {
		t.Fatalf("plan=%q err=%v", plan, err)
	}
}

func TestTodo_LEAVE_011_Mutation(t *testing.T) {
	base, err := ReconcileEffects(effectSet())
	if err != nil {
		t.Fatal(err)
	}
	// Benefits observation arrival clears the degraded dimensions.
	observed := effectSet()
	observed[1].State = EffectPass
	observed[1].Observation = "obs:benefits-2"
	cleared, err := ReconcileEffects(observed)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ConsistencyState != "OK" || cleared.Digest == base.Digest {
		t.Fatalf("cleared=%+v", cleared)
	}
	// Forged dimensions never verify.
	forged := base
	forged.ObligationState = "SATISFIED"
	if err := forged.Verify(); err == nil {
		t.Fatal("forged dimensions verified")
	}
}
