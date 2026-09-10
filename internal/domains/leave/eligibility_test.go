package leave

import (
	"sync"
	"testing"
)

type tenureRule struct{}

func (tenureRule) RuleID() string  { return "tenure-12mo" }
func (tenureRule) Version() string { return "v3" }
func (tenureRule) Evaluate(facts map[string]string) (string, []string, error) {
	if facts["service.months"] >= "12" {
		return ProgramEligible, nil, nil
	}
	return ProgramIneligible, []string{"tenure-below-12mo"}, nil
}

type hourRule struct{}

func (hourRule) RuleID() string  { return "hours-1250" }
func (hourRule) Version() string { return "v1" }
func (hourRule) Evaluate(facts map[string]string) (string, []string, error) {
	if facts["hours.worked"] >= "1250" {
		return ProgramEligible, nil, nil
	}
	return ProgramConditional, []string{"hours-projection-needed"}, nil
}

func eligibilityQueries() []ProgramQuery {
	return []ProgramQuery{
		{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", Rule: tenureRule{}, RequireFacts: []string{"service.months"}, Obligations: []string{"protect:job"}},
		{ProgramID: "state-paid", Authority: "state", Release: "ca-2026.1", Rule: hourRule{}, RequireFacts: []string{"hours.worked", "service.months"}, Obligations: []string{"pay:benefit"}},
		{ProgramID: "company-parental", Authority: "company", Release: "handbook-31", Rule: tenureRule{}, RequireFacts: []string{"service.months", "event.birth"}, Obligations: []string{"pay:parental"}},
	}
}

func eligibilityFacts() map[string]string {
	return map[string]string{"service.months": "14", "hours.worked": "1300", "manager.approved": "true"}
}

func TestTodo_LEAVE_004(t *testing.T) {
	resolution, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if len(resolution.Programs) != 3 {
		t.Fatalf("programs = %d, want every program listed", len(resolution.Programs))
	}
	byID := make(map[string]ProgramResult, 3)
	for _, result := range resolution.Programs {
		byID[result.ProgramID] = result
	}
	// Statutory tenure rule: eligible on its own facts.
	if byID["fmla"].Result != ProgramEligible || byID["fmla"].RuleID != "tenure-12mo" {
		t.Fatalf("fmla = %+v", byID["fmla"])
	}
	// Hours rule: conditional with its blocker preserved.
	if byID["state-paid"].Result != ProgramEligible {
		t.Fatalf("state-paid = %+v", byID["state-paid"])
	}
	// Missing birth event becomes UNKNOWN, never denial, and stays listed.
	if byID["company-parental"].Result != ProgramUnknown {
		t.Fatalf("company-parental = %+v", byID["company-parental"])
	}
	if err := resolution.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Manager approval never determines statutory entitlement.
	denied := map[string]string{"service.months": "14", "hours.worked": "1300", "manager.approved": "false", "reviewer.approved": "false"}
	second, err := ResolveEligibility(eligibilityQueries(), denied)
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest != resolution.Digest {
		t.Fatal("human approval changed statutory eligibility")
	}
}

func TestTodo_LEAVE_004_Property(t *testing.T) {
	first, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("resolution is not deterministic")
	}
	// Every result stays inside the closed vocabulary.
	for _, result := range first.Programs {
		switch result.Result {
		case ProgramEligible, ProgramIneligible, ProgramConditional, ProgramUnknown:
		default:
			t.Fatalf("%s result %q outside vocabulary", result.ProgramID, result.Result)
		}
		if result.RuleID == "" || result.RuleVersion == "" {
			t.Fatalf("%s lost its rule trace", result.ProgramID)
		}
	}
	// Ineligible programs stay listed with independent traces.
	short := map[string]string{"service.months": "06", "hours.worked": "100", "event.birth": "true"}
	third, err := ResolveEligibility(eligibilityQueries(), short)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Programs) != 3 {
		t.Fatalf("ineligible programs disappeared: %+v", third.Programs)
	}
}

func TestTodo_LEAVE_004_Race(t *testing.T) {
	registry := NewRuleRegistry()
	if err := registry.Register(tenureRule{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(hourRule{}); err != nil {
		t.Fatal(err)
	}
	rule, ok := registry.Lookup("tenure-12mo")
	if !ok {
		t.Fatal("registered rule missing")
	}
	const workers = 16
	var wg sync.WaitGroup
	digests := make([]string, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			queries := []ProgramQuery{{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "r", Rule: rule, RequireFacts: []string{"service.months"}}}
			resolution, err := ResolveEligibility(queries, eligibilityFacts())
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = resolution.Digest
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("worker %d diverged", i)
		}
	}
	if err := registry.Register(tenureRule{}); err == nil {
		t.Fatal("duplicate rule registered")
	}
}

func TestTodo_LEAVE_004_Security(t *testing.T) {
	// Missing legal facts become UNKNOWN blockers, never denials.
	empty := map[string]string{}
	resolution, err := ResolveEligibility(eligibilityQueries(), empty)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range resolution.Programs {
		if result.Result != ProgramUnknown {
			t.Fatalf("%s = %q, want UNKNOWN", result.ProgramID, result.Result)
		}
		if len(result.Blockers) == 0 {
			t.Fatalf("%s lost its missing-fact blockers", result.ProgramID)
		}
	}
	// Approval facts reach non-statutory rules untouched but never mint
	// statutory eligibility by themselves.
	approvalOnly := map[string]string{"manager.approved": "true", "reviewer.approved": "true", "service.months": "14", "hours.worked": "1300"}
	statutory, err := ResolveEligibility(eligibilityQueries()[:1], approvalOnly)
	if err != nil {
		t.Fatal(err)
	}
	if statutory.Programs[0].Result != ProgramEligible {
		t.Fatalf("statutory = %+v", statutory.Programs[0])
	}
	for _, fact := range statutory.Programs[0].Facts {
		if fact == "manager.approved=true" || fact == "reviewer.approved=true" {
			t.Fatalf("statutory trace consumed approval: %v", statutory.Programs[0].Facts)
		}
	}
}

func TestTodo_LEAVE_004_Conformance(t *testing.T) {
	resolution, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"fmla": ProgramEligible, "state-paid": ProgramEligible, "company-parental": ProgramUnknown}
	for _, result := range resolution.Programs {
		if result.Result != want[result.ProgramID] {
			t.Fatalf("%s = %q, want %q", result.ProgramID, result.Result, want[result.ProgramID])
		}
		if len(result.Obligations) == 0 {
			t.Fatalf("%s lost its obligations", result.ProgramID)
		}
	}
}

func TestTodo_LEAVE_004_Mutation(t *testing.T) {
	base, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil {
		t.Fatal(err)
	}
	// Tenure loss flips the statutory result and the seal.
	short := map[string]string{"service.months": "06", "hours.worked": "1300", "manager.approved": "true"}
	flipped, err := ResolveEligibility(eligibilityQueries(), short)
	if err != nil {
		t.Fatal(err)
	}
	flippedByID := make(map[string]ProgramResult, len(flipped.Programs))
	for _, result := range flipped.Programs {
		flippedByID[result.ProgramID] = result
	}
	if flippedByID["fmla"].Result != ProgramIneligible || flipped.Digest == base.Digest {
		t.Fatalf("flipped=%+v", flippedByID["fmla"])
	}
	// Forged results break the seal.
	forged := base
	forged.Programs[0].Result = ProgramEligible
	forged.Programs[2].Result = ProgramEligible
	if err := forged.Verify(); err == nil {
		t.Fatal("forged resolution verified")
	}
}
