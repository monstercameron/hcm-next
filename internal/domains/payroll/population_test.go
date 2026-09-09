package payroll

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func payrollAsOf(t *testing.T, unix int64) values.Instant {
	t.Helper()
	in, err := values.NewInstantFromUnix(unix, 0)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func payrollMembers() []PopulationMember {
	return []PopulationMember{
		{WorkerRef: "worker-b", EmploymentRef: "employment-b", PayGroupRef: "monthly"},
		{WorkerRef: "worker-a", EmploymentRef: "employment-a", PayGroupRef: "monthly"},
	}
}

func frozenPayrollPopulation(t *testing.T) FrozenPopulation {
	t.Helper()
	p, err := FreezePopulation(validPayrollRun(t), payrollAsOf(t, 1798761600), payrollMembers(), LateEntryPolicyExplicitAmendment)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTodo_PAYRUN_002 is the primary acceptance case for a digested,
// as-of-bound population and explicit immutable amendment revisions.
func TestTodo_PAYRUN_002(t *testing.T) {
	p := frozenPayrollPopulation(t)
	if p.State != PopulationStateFrozen || p.Revision != 1 || p.Digest == "" || len(p.MemberList()) != 2 {
		t.Fatalf("frozen population = %+v", p)
	}
	amendment, err := NewPopulationAmendment(PopulationAmendmentLateEntry,
		PopulationMember{WorkerRef: "worker-c", EmploymentRef: "employment-c", PayGroupRef: "monthly"},
		"hire effective after cutoff", payrollAsOf(t, 1798848000))
	if err != nil {
		t.Fatal(err)
	}
	next, err := p.Amend(amendment)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || next.SupersedesDigest != p.Digest || next.Digest == p.Digest || len(next.Members) != 3 {
		t.Fatalf("amended population = %+v", next)
	}
	if p.Revision != 1 || len(p.Members) != 2 {
		t.Fatal("amendment mutated the original population")
	}
}

// TestTodo_PAYRUN_002_Property checks deterministic ordering and duplicate
// employment/worker ambiguity refusal.
func TestTodo_PAYRUN_002_Property(t *testing.T) {
	first := frozenPayrollPopulation(t)
	reordered, err := FreezePopulation(validPayrollRun(t), first.AsOf, []PopulationMember{payrollMembers()[1], payrollMembers()[0]}, LateEntryPolicyExplicitAmendment)
	if err != nil || reordered.Digest != first.Digest {
		t.Fatalf("membership ordering changed digest: %v %s %s", err, reordered.Digest, first.Digest)
	}
	for _, members := range [][]PopulationMember{
		{{WorkerRef: "w", EmploymentRef: "e", PayGroupRef: "monthly"}, {WorkerRef: "w2", EmploymentRef: "e", PayGroupRef: "monthly"}},
		{{WorkerRef: "w", EmploymentRef: "e1", PayGroupRef: "monthly"}, {WorkerRef: "w", EmploymentRef: "e2", PayGroupRef: "monthly"}},
		{{WorkerRef: "w", EmploymentRef: "e", PayGroupRef: "weekly"}},
	} {
		if _, err := FreezePopulation(validPayrollRun(t), first.AsOf, members, LateEntryPolicyExplicitAmendment); !errors.Is(err, ErrPopulationAmbiguous) {
			t.Fatalf("ambiguous members error = %v", err)
		}
	}
}

// TestTodo_PAYRUN_002_Golden pins stable digest and explanation facts.
func TestTodo_PAYRUN_002_Golden(t *testing.T) {
	p := frozenPayrollPopulation(t)
	other := frozenPayrollPopulation(t)
	if p.Digest != other.Digest {
		t.Fatalf("identical populations differ: %q %q", p.Digest, other.Digest)
	}
	explanation, err := p.Explain()
	if err != nil || explanation.MemberCount != 2 || explanation.LateEntryPolicy != LateEntryPolicyExplicitAmendment {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}
}

// TestTodo_PAYRUN_002_Race proves independent readers and revisions do not
// mutate shared membership.
func TestTodo_PAYRUN_002_Race(t *testing.T) {
	p := frozenPayrollPopulation(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Explain(); err != nil {
				errs <- err
			}
			members := p.MemberList()
			members[0].WorkerRef = "changed-copy"
			if p.MemberList()[0].WorkerRef == "changed-copy" {
				errs <- errors.New("member list leaked mutable storage")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestTodo_PAYRUN_002_Integration proves the population binding and pay group
// remain attached when the run is calculated.
func TestTodo_PAYRUN_002_Integration(t *testing.T) {
	run := validPayrollRun(t)
	p, err := FreezePopulation(run, payrollAsOf(t, 1798761600), payrollMembers(), LateEntryPolicyExplicitAmendment)
	if err != nil {
		t.Fatal(err)
	}
	calculated, err := CalculateAgainstPopulation(run, p, "sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	if calculated.State != PayrollRunStateCalculated || calculated.Population != run.Population {
		t.Fatalf("calculated = %+v", calculated)
	}
}

// TestTodo_PAYRUN_002_Fault covers missing as-of, incomplete members and
// invalid explicit amendments.
func TestTodo_PAYRUN_002_Fault(t *testing.T) {
	run := validPayrollRun(t)
	if _, err := FreezePopulation(run, values.Instant{}, payrollMembers(), LateEntryPolicyExplicitAmendment); !errors.Is(err, ErrInvalidFrozenPopulation) {
		t.Fatalf("missing as-of error = %v", err)
	}
	if _, err := FreezePopulation(run, payrollAsOf(t, 1798761600), []PopulationMember{{WorkerRef: "w", EmploymentRef: "e", PayGroupRef: "monthly"}}, LateEntryPolicy("UNKNOWN")); !errors.Is(err, ErrInvalidFrozenPopulation) {
		t.Fatalf("unknown policy error = %v", err)
	}
	p := frozenPayrollPopulation(t)
	bad, err := NewPopulationAmendment(PopulationAmendmentLateEntry, PopulationMember{WorkerRef: "worker-a", EmploymentRef: "employment-a", PayGroupRef: "monthly"}, "duplicate", payrollAsOf(t, 1798848000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Amend(bad); !errors.Is(err, ErrPopulationAmendment) {
		t.Fatalf("duplicate amendment error = %v", err)
	}
}

// TestTodo_PAYRUN_002_Mutation proves a successor digest changes while the
// original revision and binding remain unchanged.
func TestTodo_PAYRUN_002_Mutation(t *testing.T) {
	p := frozenPayrollPopulation(t)
	a, err := NewPopulationAmendment(PopulationAmendmentRemoval, payrollMembers()[0], "employment ended", payrollAsOf(t, 1798848000))
	if err != nil {
		t.Fatal(err)
	}
	next, err := p.Amend(a)
	if err != nil || next.Digest == p.Digest || p.Digest == "" {
		t.Fatalf("next = %+v, err=%v", next, err)
	}
	if p.State != PopulationStateFrozen || len(p.Members) != 2 {
		t.Fatal("original revision changed")
	}
	superseded, err := p.SupersededRevision(next)
	if err != nil || superseded.State != PopulationStateSuperseded {
		t.Fatalf("superseded = %+v, err=%v", superseded, err)
	}
	if _, err := CalculateAgainstPopulation(validPayrollRun(t), superseded, "sha256:calculation"); !errors.Is(err, ErrPopulationSuperseded) {
		t.Fatalf("superseded calculation error = %v", err)
	}
}

// TestTodo_PAYRUN_002_Security proves only governed references enter the
// snapshot and an unrelated run cannot calculate against it.
func TestTodo_PAYRUN_002_Security(t *testing.T) {
	p := frozenPayrollPopulation(t)
	other, err := NewPayrollRun("other-run", "monthly", PeriodRef{ID: "other-period", Version: "v1", Digest: "sha256:other-period"}, PopulationBindingRef{DefinitionID: "other-population", RevisionVersion: "v1", Digest: "sha256:other-population"}, "sha256:other-inputs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateAgainstPopulation(other, p, "sha256:calculation"); !errors.Is(err, ErrPopulationBindingMismatch) {
		t.Fatalf("cross-run calculation error = %v", err)
	}
	if _, err := FreezePopulation(validPayrollRun(t), p.AsOf, []PopulationMember{{WorkerRef: "w", EmploymentRef: "e", PayGroupRef: "monthly", MemberRef: "raw-different"}}, LateEntryPolicyExplicitAmendment); !errors.Is(err, ErrInvalidFrozenPopulation) {
		t.Fatalf("disagreeing refs error = %v", err)
	}
}
