package approval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

func successorInput() approval.ReplanInput {
	return approval.ReplanInput{
		PriorProposalDigest: "digest:rev-1",
		NewSnapshotDigest:   "digest:snapshot-2",
		Components:          map[string]string{"entitlement": "digest:ent-2", "schedule": "digest:sch-2"},
		Retained: []approval.ReuseDecision{
			{Decision: approval.ApprovalDecision{DecisionID: "d1"}, Verdict: approval.ReuseRetainedByReference, ApplicabilityLink: "digest:rev-1", PolicyVersion: "reuse-2026.1"},
		},
		Route: approval.RouteReview,
	}
}

func TestTodo_REPLAN_004(t *testing.T) {
	input := successorInput()
	successor, err := approval.CreateSuccessor(input)
	if err != nil {
		t.Fatalf("CreateSuccessor: %v", err)
	}
	// The successor advances past the old digest with its own.
	if successor.SuccessorDigest == "" || successor.SuccessorDigest == input.PriorProposalDigest {
		t.Fatalf("successor=%+v", successor)
	}
	if successor.Supersedes != "digest:rev-1" || successor.SnapshotDigest != "digest:snapshot-2" {
		t.Fatalf("successor=%+v", successor)
	}
	if len(successor.RetainedReceipts) != 1 || successor.Route != approval.RouteReview {
		t.Fatalf("successor=%+v", successor)
	}
	if err := successor.Verify(input); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: skipped reapproval never routes.
	material := successorInput()
	material.Retained[0].Verdict = approval.ReuseReapprovalRequired
	material.Route = approval.RouteReview
	if _, err := approval.CreateSuccessor(material); err == nil {
		t.Fatal("material drift routed to review")
	}
	material.Route = approval.RouteReapproval
	routed, err := approval.CreateSuccessor(material)
	if err != nil || routed.Route != approval.RouteReapproval {
		t.Fatalf("routed=%+v err=%v", routed, err)
	}
	// Unknown drift forces revalidation.
	blinded := successorInput()
	blinded.Retained[0].Verdict = approval.ReuseUnknown
	blinded.Route = approval.RouteRevalidation
	if _, err := approval.CreateSuccessor(blinded); err != nil {
		t.Fatalf("revalidation: %v", err)
	}
	blinded.Route = approval.RouteReview
	if _, err := approval.CreateSuccessor(blinded); err == nil {
		t.Fatal("unknown drift routed to review")
	}
	// Hollow inputs never produce a successor.
	empty := successorInput()
	empty.Components = nil
	if _, err := approval.CreateSuccessor(empty); err == nil {
		t.Fatal("component-free successor created")
	}
	stale := successorInput()
	stale.NewSnapshotDigest = stale.PriorProposalDigest
	if _, err := approval.CreateSuccessor(stale); err == nil {
		t.Fatal("non-advancing successor created")
	}
}

func TestTodo_REPLAN_004_Golden(t *testing.T) {
	input := successorInput()
	successor, err := approval.CreateSuccessor(input)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"successor=" + successor.SuccessorDigest,
		"supersedes=" + successor.Supersedes,
		"route=" + successor.Route,
		"retained=" + strings.Join(successor.RetainedReceipts, ","),
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "replan004_successor.golden")
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

func TestTodo_REPLAN_004_Mutation(t *testing.T) {
	input := successorInput()
	base, err := approval.CreateSuccessor(input)
	if err != nil {
		t.Fatal(err)
	}
	// Recomputed component change re-identifies the successor.
	changed := successorInput()
	changed.Components["schedule"] = "digest:sch-3"
	recomputed, err := approval.CreateSuccessor(changed)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed.SuccessorDigest == base.SuccessorDigest {
		t.Fatal("component mutation kept the successor digest")
	}
	// Verdict flip reroutes the successor.
	flipped := successorInput()
	flipped.Retained[0].Verdict = approval.ReuseReapprovalRequired
	flipped.Route = approval.RouteReapproval
	rerouted, err := approval.CreateSuccessor(flipped)
	if err != nil {
		t.Fatal(err)
	}
	if rerouted.Route == base.Route || rerouted.SuccessorDigest == base.SuccessorDigest {
		t.Fatalf("rerouted=%+v", rerouted)
	}
	// Forged successors never verify.
	forged := base
	forged.Route = approval.RouteReapproval
	if err := forged.Verify(input); err == nil {
		t.Fatal("forged successor verified")
	}
}
