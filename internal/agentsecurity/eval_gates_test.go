package agentsecurity

import (
	"fmt"
	"sync"
	"testing"
)

func testRelease() Release {
	return Release{Agent: "concierge", AgentBuild: "b1", Model: "m1", ModelDigest: "sha256:m", Tool: "people.lookup", ToolVersion: 3, Prompt: "triage", PromptHash: "sha256:p"}
}

func passingFixtures() []EvalFixture {
	return []EvalFixture{
		{Name: "safety", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
		{Name: "grounding", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
		{Name: "tool-selection", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
	}
}

func TestTodo_AGENT_004(t *testing.T) {
	evaluator := NewEvaluator()
	publisher := NewPublisher()
	run, err := evaluator.Evaluate(testRelease(), passingFixtures())
	if err != nil || !run.Passed {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if err := run.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := publisher.Publish(run); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !publisher.Published(run.Digest) {
		t.Fatal("passing run not published")
	}
	// A failed safety fixture grounds the release: it cannot deploy.
	failed := passingFixtures()
	failed[0].SafetyPass = false
	grounded, err := evaluator.Evaluate(testRelease(), failed)
	if err != nil || grounded.Passed {
		t.Fatalf("grounded=%+v err=%v", grounded, err)
	}
	if err := publisher.Publish(grounded); err == nil {
		t.Fatal("failed evaluation deployed")
	}
	// Kill switch revokes in-flight write-capable calls with fallback.
	switchBoard := NewKillSwitch()
	if err := switchBoard.Grant(Lease{ID: "l1", Agent: "concierge", Model: "m1", Tool: "people.lookup", Tenant: "acme", WriteCapable: true}); err != nil {
		t.Fatal(err)
	}
	revoked, fallback := switchBoard.Disable(DisableScope{Agent: "concierge"})
	if revoked != 1 || !fallback.NonAIAvailable {
		t.Fatalf("revoked=%d fallback=%+v", revoked, fallback)
	}
	if _, err := switchBoard.Invoke("l1"); err == nil {
		t.Fatal("revoked write-capable call still in flight")
	}
}

func TestTodo_AGENT_004_Property(t *testing.T) {
	evaluator := NewEvaluator()
	publisher := NewPublisher()
	// Publication holds exactly for passing sealed runs.
	for i, fixtures := range [][]EvalFixture{passingFixtures(), {}, {{Name: "only", SafetyPass: true, GroundingPass: false, ToolSelectionPass: true}}} {
		run, err := evaluator.Evaluate(testRelease(), fixtures)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		err = publisher.Publish(run)
		if run.Passed && err != nil {
			t.Fatalf("case %d: passing run refused: %v", i, err)
		}
		if !run.Passed && err == nil {
			t.Fatalf("case %d: failing run published", i)
		}
	}
	// Version change is a new release: the old seal never covers it.
	oldRun, _ := evaluator.Evaluate(testRelease(), passingFixtures())
	changed := testRelease()
	changed.ModelDigest = "sha256:m2"
	newRun, err := evaluator.Evaluate(changed, passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if oldRun.Digest == newRun.Digest {
		t.Fatal("version change reused the old eval seal")
	}
	// Fallback always keeps non-AI HCM available.
	switchBoard := NewKillSwitch()
	if err := switchBoard.Grant(Lease{ID: "w1", Agent: "a", WriteCapable: true}); err != nil {
		t.Fatal(err)
	}
	if _, fallback := switchBoard.Disable(DisableScope{AllAI: true}); !fallback.NonAIAvailable {
		t.Fatal("kill-all-AI removed non-AI availability")
	}
}

func TestTodo_AGENT_004_Race(t *testing.T) {
	switchBoard := NewKillSwitch()
	for i := 0; i < 16; i++ {
		if err := switchBoard.Grant(Lease{ID: fmt.Sprintf("lease-%d", i), Agent: "concierge", WriteCapable: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			switchBoard.Disable(DisableScope{Agent: "concierge"})
		}(i)
		go func(i int) {
			defer wg.Done()
			switchBoard.Invoke(fmt.Sprintf("lease-%d", i))
		}(i)
	}
	wg.Wait()
	// After the storm every lease is revoked: none stays in flight.
	for i := 0; i < 16; i++ {
		if _, err := switchBoard.Invoke(fmt.Sprintf("lease-%d", i)); err == nil {
			t.Fatalf("lease-%d survived the kill switch", i)
		}
	}
}

func TestTodo_AGENT_004_Security(t *testing.T) {
	evaluator := NewEvaluator()
	publisher := NewPublisher()
	// A tampered run seal never publishes.
	run, err := evaluator.Evaluate(testRelease(), passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	run.Passed = true
	run.Fixtures[0].SafetyPass = false
	if err := publisher.Publish(run); err == nil {
		t.Fatal("tampered eval seal published")
	}
	// Unknown leases and foreign scopes never invoke.
	switchBoard := NewKillSwitch()
	if _, err := switchBoard.Invoke("ghost"); err == nil {
		t.Fatal("unknown lease invoked")
	}
	if err := switchBoard.Grant(Lease{ID: "keep", Agent: "other", WriteCapable: true}); err != nil {
		t.Fatal(err)
	}
	revoked, _ := switchBoard.Disable(DisableScope{Agent: "concierge"})
	if revoked != 0 {
		t.Fatalf("foreign scope revoked %d leases", revoked)
	}
	if _, err := switchBoard.Invoke("keep"); err != nil {
		t.Fatalf("out-of-scope lease revoked: %v", err)
	}
	// Incidents bind exact versions and material.
	release := testRelease()
	incident, err := RecordIncident(release, "sha256:in", "sha256:out", []string{"people.lookup"})
	if err != nil || incident.IncidentDigest == "" || incident.Release.ModelDigest != "sha256:m" {
		t.Fatalf("incident=%+v err=%v", incident, err)
	}
	if _, err := RecordIncident(Release{}, "", "", nil); err == nil {
		t.Fatal("empty incident recorded")
	}
}
