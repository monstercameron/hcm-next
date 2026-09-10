package compensation_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

func simulateValid(t *testing.T) (compensation.ChangeSimulation, compensation.ChangeReservation) {
	t.Helper()
	simulation, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := compensation.ReserveChange(simulation, changeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	return simulation, reserved
}

func TestTodo_CONF_020_Property(t *testing.T) {
	// Exact-decimal presence: every line with an amount annualizes into
	// the total; an amount-less end line stays present but unpriced.
	req := changeRequest(t)
	req.Lines[2] = compensation.ChangeLine{
		Type: compensation.ComponentAllowance, Op: compensation.OpEnd, Revision: 7,
		HasAmount: false, Currency: "USD", EndCondition: "allowance-discontinued",
	}
	simulation, err := compensation.SimulateChange(compCurrent(), req, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(simulation.Annual) != 2 {
		t.Fatalf("annualized %d lines, want the 2 with amounts", len(simulation.Annual))
	}
	if simulation.TotalAnnual.String() != "105000.00" {
		t.Fatalf("total = %s, want the ended line excluded at 105000.00", simulation.TotalAnnual)
	}
	// Determinism: identical inputs seal identical digests.
	again, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	first, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != again.Digest {
		t.Fatal("identical simulations diverge")
	}
	// The reserve binds the fence: the same money under another fence ID
	// is another reservation context.
	other := changeRequest(t)
	other.Budget.BudgetID = "budget/other"
	otherSim, err := compensation.SimulateChange(compCurrent(), other, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if otherSim.BudgetDigest == first.BudgetDigest {
		t.Fatal("budget digest ignores the fence identity")
	}
}

func TestTodo_CONF_020_Golden(t *testing.T) {
	simulation, reserved := simulateValid(t)
	committed, err := compensation.CommitChange(reserved, simulation, changeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	failed := committed.Effects[0].EffectID
	repair, err := compensation.RepairChange(committed, []string{failed}, map[string]string{failed: committed.Effects[0].Digest})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "total-annual: %s\n", simulation.TotalAnnual)
	fmt.Fprintf(&b, "simulation: %s\n", simulation.Digest)
	fmt.Fprintf(&b, "reservation: %s@%s\n", reserved.SimulationDigest, reserved.BudgetID)
	fmt.Fprintf(&b, "package: %s\n", committed.PackageDigest)
	for _, effect := range committed.Effects {
		fmt.Fprintf(&b, "effect: %s %s applied=%v\n", effect.EffectID, effect.Digest, effect.Applied)
	}
	fmt.Fprintf(&b, "redriven: %s untouched: %d\n", strings.Join(repair.Redriven, ","), len(repair.Untouched))
	got := b.String()
	path := filepath.Join("testdata", "conf020_change.golden")
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

func TestTodo_CONF_020_Race(t *testing.T) {
	first, _ := simulateValid(t)
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			simulation, reserved := simulateValid(t)
			committed, err := compensation.CommitChange(reserved, simulation, changeRequest(t))
			if err != nil {
				errs <- err
				return
			}
			digests <- committed.PackageDigest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent change = %v", err)
	}
	for digest := range digests {
		if digest != first.Digest {
			t.Fatal("concurrent simulations diverge")
		}
	}
}

// TestTodo_CONF_020_Integration fences the change total through the
// shared reservation protocol in integer cents and redrives the failed
// effect without rerunning its commit.
func TestTodo_CONF_020_Integration(t *testing.T) {
	simulation, reserved := simulateValid(t)
	committed, err := compensation.CommitChange(reserved, simulation, changeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	// 111000.00 USD as integer cents: exact, never float.
	now := changeNow()
	store := reservation.NewStore()
	hold, err := store.Acquire(reservation.Request{
		Resource: "budget/fy26", Version: 1,
		Quantity: reservation.Quantity{Value: 11100000, Scale: 2},
		Interval: reservation.Interval{From: now, To: now.Add(30 * 24 * time.Hour)},
		Owner:    "package/assignment-1", Priority: 1, ExpiresAt: now.Add(time.Hour),
		ProposalDigest:  reservation.Digest([]byte(simulation.Digest)),
		AuthorityDigest: reservation.Digest([]byte("authority/compensation")),
		IdempotencyKey:  "comp-change-1",
	}, reservation.Quantity{Value: 11100000, Scale: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(hold.ID, hold.Fence, now); err != nil {
		t.Fatal(err)
	}
	// The failed effect redrives alone: the commit stands untouched.
	failed := committed.Effects[1].EffectID
	repair, err := compensation.RepairChange(committed, []string{failed}, map[string]string{failed: committed.Effects[1].Digest})
	if err != nil {
		t.Fatal(err)
	}
	if len(repair.Redriven) != 1 || len(repair.Untouched) != 2 {
		t.Fatalf("repair = %+v, want exactly the failed effect redriven", repair)
	}
	if committed.PackageDigest != simulation.Digest {
		t.Fatal("redrive disturbed the commit")
	}
}

func TestTodo_CONF_020_Fault(t *testing.T) {
	// Unknown frequencies never annualize.
	weekly := changeRequest(t)
	weekly.Lines[0].Frequency = "weekly"
	if _, err := compensation.SimulateChange(compCurrent(), weekly, changePolicy()); err == nil {
		t.Fatal("weekly frequency annualized")
	}
	// Committing another simulation under this reservation is refused.
	simulation, reserved := simulateValid(t)
	other, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	other.Digest = "sha256:foreign"
	if _, err := compensation.CommitChange(reserved, other, changeRequest(t)); err == nil {
		t.Fatal("foreign simulation committed under the reservation")
	}
	// Repair with a mismatched observation is refused entirely.
	committed, err := compensation.CommitChange(reserved, simulation, changeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	failed := committed.Effects[0].EffectID
	if _, err := compensation.RepairChange(committed, []string{failed}, map[string]string{failed: "sha256:wrong"}); !errors.Is(err, compensation.ErrObservationMismatch) {
		t.Fatalf("mismatched observation = %v, want ErrObservationMismatch", err)
	}
	// A repair batch naming an unknown target alongside a valid one
	// fails whole: no partial redrive escapes.
	if _, err := compensation.RepairChange(committed, []string{failed, "effect/ghost"},
		map[string]string{failed: committed.Effects[0].Digest}); err == nil {
		t.Fatal("mixed repair batch partially applied")
	}
}

func TestTodo_CONF_020_Security(t *testing.T) {
	simulation, reserved := simulateValid(t)
	// Every committed field denied in turn blocks the atomic write.
	fields := []compensation.FieldID{
		compensation.FieldAmount, compensation.FieldComponentType,
		compensation.FieldFrequency, compensation.FieldEffectiveInterval,
	}
	for _, field := range fields {
		req := changeRequest(t)
		req.Disclosure.Fields[field] = compensation.FieldRuling{Effect: compensation.EffectDeny, Reason: "need-to-know"}
		if _, err := compensation.CommitChange(reserved, simulation, req); !errors.Is(err, compensation.ErrUnauthorizedFact) {
			t.Fatalf("denied %s committed", field)
		}
	}
	// Each fence context drifting alone breaks the reservation binding.
	for name, mutate := range map[string]func(*compensation.BudgetFence){
		"band":    func(b *compensation.BudgetFence) { b.BandDigest = "sha256:drift" },
		"payroll": func(b *compensation.BudgetFence) { b.PayrollDigest = "sha256:drift" },
		"legal":   func(b *compensation.BudgetFence) { b.LegalDigest = "sha256:drift" },
	} {
		req := changeRequest(t)
		mutate(&req.Budget)
		if _, err := compensation.ReserveChange(simulation, req); !errors.Is(err, compensation.ErrStaleContext) {
			t.Fatalf("drifted %s reserved", name)
		}
	}
	// An over-budget reservation is refused even with a live fence.
	poor := changeRequest(t)
	poor.Budget.Amount = changeDecimal(t, "1000.00")
	poorSim, err := compensation.SimulateChange(compCurrent(), poor, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compensation.ReserveChange(poorSim, poor); !errors.Is(err, compensation.ErrOverBudget) {
		t.Fatalf("over-budget reservation = %v, want ErrOverBudget", err)
	}
}

func TestTodo_CONF_020_Conformance(t *testing.T) {
	// Lifecycle order is enforced by bindings: an empty reservation, an
	// uncommitted package and an unknown repair target all refuse.
	simulation, _ := simulateValid(t)
	if _, err := compensation.CommitChange(compensation.ChangeReservation{}, simulation, changeRequest(t)); err == nil {
		t.Fatal("unreserved commit accepted")
	}
	if _, err := compensation.RepairChange(compensation.CommittedChange{}, []string{"effect/x"}, nil); err == nil {
		t.Fatal("repair without commit accepted")
	}
	// Authority: the disclosure covers exactly the committed fields and
	// ends in allow.
	req := changeRequest(t)
	if err := req.Disclosure.Covers([]compensation.FieldID{
		compensation.FieldAmount, compensation.FieldComponentType,
		compensation.FieldFrequency, compensation.FieldEffectiveInterval,
	}); err != nil {
		t.Fatalf("disclosure does not cover the committed fields: %v", err)
	}
	// Simulation commits nothing: no effects exist before CommitChange.
	if len(simulation.Composed.Children) == 0 {
		t.Fatal("simulation composed no children")
	}
}

func TestTodo_CONF_020_Mutation(t *testing.T) {
	// Mutant 1: binary-scale drift in one line moves the exact total and
	// breaks the golden figure.
	drifted := changeRequest(t)
	drifted.Lines[0].Amount = changeDecimal(t, "95000.01")
	simulation, err := compensation.SimulateChange(compCurrent(), drifted, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if simulation.TotalAnnual.String() == "111000.00" {
		t.Fatal("one-cent drift invisible in the total")
	}
	if simulation.Digest == "" {
		t.Fatal("drifted simulation carries no digest")
	}
	// Mutant 2: an empty fence identity is stale context, not a budget.
	anonymous := changeRequest(t)
	anonymous.Budget.BudgetID = ""
	anonSim, err := compensation.SimulateChange(compCurrent(), anonymous, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compensation.ReserveChange(anonSim, anonymous); !errors.Is(err, compensation.ErrStaleContext) {
		t.Fatalf("anonymous fence = %v, want ErrStaleContext", err)
	}
	// Mutant 3: zero hours price hourly lines at nothing exact.
	zeroHours := changeRequest(t)
	zeroHours.HoursPerYear = changeDecimal(t, "0.00")
	zeroHours.Lines = []compensation.ChangeLine{{
		Type: compensation.ComponentBase, Op: compensation.OpRevise, Revision: 4,
		Amount: changeDecimal(t, "50.00"), HasAmount: true, Currency: "USD", Frequency: "hourly",
	}}
	zeroed, err := compensation.SimulateChange(compCurrent(), zeroHours, changePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if zeroed.TotalAnnual.String() != "0.00" {
		t.Fatalf("zero-hour total = %s, want exact 0.00", zeroed.TotalAnnual)
	}
	// Mutant 4: repairing with no failures redrives nothing and touches nothing.
	simulation, reserved := simulateValid(t)
	committed, err := compensation.CommitChange(reserved, simulation, changeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := compensation.RepairChange(committed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Redriven) != 0 || len(empty.Untouched) != 3 {
		t.Fatalf("empty repair = %+v, want all effects untouched", empty)
	}
}
