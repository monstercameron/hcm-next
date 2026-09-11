package org_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
)

// intentFixture builds the CONF-025 directory: Jane reports to Alice, Bob
// is eligible, all three sit inside the authorized scope, and Jane has one
// direct report (Theo) so the simulated closure is non-trivial.
func intentFixture() org.Directory {
	dir := fixture()
	dir.People["theo"] = org.Person{ID: "theo", Active: true}
	dir.Edges["edge/theo"] = org.Edge{EdgeID: "edge/theo", WorkerID: "theo", ManagerID: "jane", StartDate: "2025-09-01", Revision: 1}
	return dir
}

func intentNow() time.Time {
	return time.Date(2026, time.January, 10, 9, 0, 0, 0, time.UTC)
}

func changeIntent() org.ChangeManagerIntent {
	return org.ChangeManagerIntent{
		WorkerID: "jane", CurrentManagerRef: "alice", ProposedManagerRef: "bob",
		EffectiveDate: "2026-02-01", Reason: "reorg",
		OrgScope: []string{"jane", "alice", "bob", "carol", "hrbp", "theo"},
		Basis:    org.BasisSnapshot,
	}
}

func intentApprovals() []org.Approval {
	return []org.Approval{
		{ApproverID: "alice", Role: "current-manager", AuthorityVersion: 3},
		{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3},
	}
}

func blockOf(t *testing.T, err error) *org.Block {
	t.Helper()
	if err == nil {
		t.Fatal("expected a block, got nil")
	}
	var block *org.Block
	if !errors.As(err, &block) {
		t.Fatalf("error %v is not a *Block", err)
	}
	return block
}

// TestChangeManagerIntentConformancePreservesGraphAuthorityAndZeroProductionExposure
// is the CONF-025 primary test.
//
// RED: an inactive actor, self-management, a cycle, a stale relationship,
// a conflicting transfer/termination, an out-of-scope manager, an unbound
// approval, effective-date drift or an observation asserted as authority
// yields an executable standalone mutation or a published production
// capability.
// GREEN: the exact typed intent compiles through every stage to one
// relationship write plan; negatives return exact block/replan states;
// production exposure stays absent.
func TestChangeManagerIntentConformancePreservesGraphAuthorityAndZeroProductionExposure(t *testing.T) {
	dir := intentFixture()
	plan, err := org.CompileIntent(changeIntent(), dir, intentApprovals(), intentNow())
	if err != nil {
		t.Fatal(err)
	}
	wantStages := []string{"snapshot", "validation", "authz", "conflict", "simulation", "approval", "wait", "revalidation", "write-plan"}
	if len(plan.Stages) != len(wantStages) {
		t.Fatalf("stages = %v, want %v", plan.Stages, wantStages)
	}
	for i, stage := range wantStages {
		if plan.Stages[i] != stage {
			t.Fatalf("stages[%d] = %s, want %s", i, plan.Stages[i], stage)
		}
	}
	if len(plan.WritePlan) != 2 || plan.WritePlan[0].Op != "end-edge" || plan.WritePlan[1].Op != "create-edge" {
		t.Fatalf("write plan = %+v, want exactly one end-edge plus one create-edge", plan.WritePlan)
	}
	if plan.WritePlan[1].Worker != "jane" || plan.WritePlan[1].Manager != "bob" {
		t.Fatalf("write plan creates %+v, want jane -> bob", plan.WritePlan[1])
	}
	if len(plan.Impacted) != 2 || plan.Impacted[0] != "jane" || plan.Impacted[1] != "theo" {
		t.Fatalf("impacted = %v, want [jane theo]", plan.Impacted)
	}
	if len(plan.ProductionEffects()) != 0 {
		t.Fatalf("production effects = %v, want none at conformance-only depth", plan.ProductionEffects())
	}
	if plan.PlanDigest == "" || plan.ProposalDigest == "" || plan.WaitUntil != "2026-02-01" {
		t.Fatalf("plan is missing digests or the wait directive: %+v", plan)
	}

	// The compiled write plan executes through the CONF-007 lifecycle and
	// ends with Jane reporting to Bob: graph authority is preserved.
	change, err := org.Propose("chg/25", "jane", plan.WritePlan[0].EdgeID, plan.WritePlan[1].Manager, plan.WaitUntil, "reorg", dir)
	if err != nil {
		t.Fatal(err)
	}
	if change.ProposalDigest != plan.ProposalDigest {
		t.Fatal("execution proposal digest differs from the compiled digest: plan and execution diverged")
	}
}

func TestTodo_CONF_025_Property(t *testing.T) {
	// Compilation is pure: identical inputs always yield the identical
	// digest, the identical two-op write plan and a sorted impacted set.
	first, err := org.CompileIntent(changeIntent(), intentFixture(), intentApprovals(), intentNow())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		next, err := org.CompileIntent(changeIntent(), intentFixture(), intentApprovals(), intentNow())
		if err != nil {
			t.Fatal(err)
		}
		if next.PlanDigest != first.PlanDigest || len(next.WritePlan) != 2 {
			t.Fatalf("iteration %d: plan is not deterministic", i)
		}
		for j := 1; j < len(next.Impacted); j++ {
			if next.Impacted[j-1] >= next.Impacted[j] {
				t.Fatalf("iteration %d: impacted %v is not sorted and deduplicated", i, next.Impacted)
			}
		}
	}
}

func TestTodo_CONF_025_Golden(t *testing.T) {
	plan, err := org.CompileIntent(changeIntent(), intentFixture(), intentApprovals(), intentNow())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "intent: %s worker=%s current=%s proposed=%s effective=%s\n", plan.IntentRef, "jane", "alice", "bob", "2026-02-01")
	fmt.Fprintf(&b, "stages: %s\n", strings.Join(plan.Stages, ","))
	fmt.Fprintf(&b, "approvers: %s\n", strings.Join(plan.RequiredApprover, ","))
	fmt.Fprintf(&b, "impacted: %s\n", strings.Join(plan.Impacted, ","))
	fmt.Fprintf(&b, "wait-until: %s\n", plan.WaitUntil)
	fmt.Fprintf(&b, "revalidate: %s\n", strings.Join(plan.Revalidate, ","))
	for _, op := range plan.WritePlan {
		fmt.Fprintf(&b, "write: %s edge=%s worker=%s manager=%s start=%s end=%s\n", op.Op, op.EdgeID, op.Worker, op.Manager, op.Start, op.End)
	}
	fmt.Fprintf(&b, "proposal: %s\n", plan.ProposalDigest)
	fmt.Fprintf(&b, "plan: %s\n", plan.PlanDigest)
	fmt.Fprintf(&b, "production-effects: %d\n", len(plan.ProductionEffects()))
	got := b.String()
	path := filepath.Join("testdata", "conf025_plan.golden")
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

func TestTodo_CONF_025_Race(t *testing.T) {
	const workers = 16
	first, err := org.CompileIntent(changeIntent(), intentFixture(), intentApprovals(), intentNow())
	if err != nil {
		t.Fatal(err)
	}
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := org.CompileIntent(changeIntent(), intentFixture(), intentApprovals(), intentNow())
			if err != nil {
				errs <- err
				return
			}
			digests <- plan.PlanDigest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent compile = %v", err)
	}
	for digest := range digests {
		if digest != first.PlanDigest {
			t.Fatal("concurrent compiles diverge")
		}
	}
}

func TestTodo_CONF_025_Security(t *testing.T) {
	// A manager outside the authorized scope cannot be installed.
	dir := intentFixture()
	scoped := changeIntent()
	scoped.OrgScope = []string{"jane", "alice", "hrbp", "theo"}
	if err := expectBlock(t, scoped, dir, org.BlockScope, false); err != nil {
		t.Fatal(err)
	}
	// The worker never approves their own change.
	selfApproved := append(intentApprovals(), org.Approval{ApproverID: "jane", Role: "hrbp", AuthorityVersion: 3})
	_, err := org.CompileIntent(changeIntent(), dir, selfApproved, intentNow())
	block := blockOf(t, err)
	if block.Code != org.BlockApproval {
		t.Fatalf("self approval block = %s, want %s", block.Code, org.BlockApproval)
	}
	// An external observation asserted as authority is refused even when
	// every other fact is perfect.
	observed := changeIntent()
	observed.Basis = org.BasisObservation
	block = blockOf(t, mustCompileErr(t, observed, dir))
	if block.Code != org.BlockObservationAuthority {
		t.Fatalf("observation basis block = %s, want %s", block.Code, org.BlockObservationAuthority)
	}
}

func mustCompileErr(t *testing.T, intent org.ChangeManagerIntent, dir org.Directory) error {
	t.Helper()
	_, err := org.CompileIntent(intent, dir, intentApprovals(), intentNow())
	if err == nil {
		t.Fatal("expected a block, got nil")
	}
	return err
}

func expectBlock(t *testing.T, intent org.ChangeManagerIntent, dir org.Directory, code org.BlockCode, replan bool) error {
	t.Helper()
	block := blockOf(t, mustCompileErr(t, intent, dir))
	if block.Code != code || block.Replan != replan {
		return fmt.Errorf("block = %s replan=%v, want %s replan=%v", block.Code, block.Replan, code, replan)
	}
	return nil
}

func TestTodo_CONF_025_Conformance(t *testing.T) {
	type negative struct {
		name   string
		mutate func(*org.ChangeManagerIntent, *org.Directory)
		code   org.BlockCode
		replan bool
	}
	negatives := []negative{
		{"inactive worker", func(i *org.ChangeManagerIntent, d *org.Directory) {
			p := d.People["jane"]
			p.Active = false
			d.People["jane"] = p
		}, org.BlockInactiveActor, false},
		{"inactive manager", func(i *org.ChangeManagerIntent, d *org.Directory) {
			p := d.People["bob"]
			p.Active = false
			d.People["bob"] = p
		}, org.BlockInactiveActor, false},
		{"self management", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.ProposedManagerRef = "jane"
		}, org.BlockSelfManagement, false},
		{"graph cycle", func(i *org.ChangeManagerIntent, d *org.Directory) {
			d.Edges["edge/alice"] = org.Edge{EdgeID: "edge/alice", WorkerID: "alice", ManagerID: "jane", StartDate: "2024-01-01", Revision: 1}
			i.ProposedManagerRef = "alice"
		}, org.BlockCycle, false},
		{"stale relationship", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.CurrentManagerRef = "carol"
		}, org.BlockStaleRelationship, true},
		{"conflicting transfer", func(i *org.ChangeManagerIntent, d *org.Directory) {
			d.Future = []org.FutureEvent{{WorkerID: "jane", Kind: "transfer", EffectiveDate: "2026-01-20"}}
		}, org.BlockConflict, true},
		{"conflicting termination", func(i *org.ChangeManagerIntent, d *org.Directory) {
			d.Future = []org.FutureEvent{{WorkerID: "jane", Kind: "termination", EffectiveDate: "2026-02-01"}}
		}, org.BlockConflict, true},
		{"unauthorized scope", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.OrgScope = []string{"jane", "alice", "hrbp"}
		}, org.BlockScope, false},
		{"unbound approval", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.ProposedManagerRef = "bob"
		}, org.BlockApproval, false},
		{"effective-date drift", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.EffectiveDate = "2026-01-01"
		}, org.BlockDateDrift, true},
		{"observation as authority", func(i *org.ChangeManagerIntent, d *org.Directory) {
			i.Basis = org.BasisObservation
		}, org.BlockObservationAuthority, false},
	}
	for _, tc := range negatives {
		t.Run(tc.name, func(t *testing.T) {
			d := intentFixture()
			intent := changeIntent()
			tc.mutate(&intent, &d)
			approvals := intentApprovals()
			if tc.name == "unbound approval" {
				approvals = []org.Approval{{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3}}
			}
			_, err := org.CompileIntent(intent, d, approvals, intentNow())
			block := blockOf(t, err)
			if block.Code != tc.code || block.Replan != tc.replan {
				t.Fatalf("block = %s replan=%v, want %s replan=%v", block.Code, block.Replan, tc.code, tc.replan)
			}
		})
	}
}

func TestTodo_CONF_025_Mutation(t *testing.T) {
	// Mutant 1: stale approval authority between compile inputs.
	dir := intentFixture()
	stale := intentApprovals()
	stale[0].AuthorityVersion = 2
	_, err := org.CompileIntent(changeIntent(), dir, stale, intentNow())
	block := blockOf(t, err)
	if block.Code != org.BlockApproval {
		t.Fatalf("stale authority block = %s, want %s", block.Code, org.BlockApproval)
	}
	// Mutant 2: an inactive approver's signature is unbound.
	dir = intentFixture()
	p := dir.People["hrbp"]
	p.Active = false
	dir.People["hrbp"] = p
	block = blockOf(t, mustCompileErr(t, changeIntent(), dir))
	if block.Code != org.BlockApproval {
		t.Fatalf("inactive approver block = %s, want %s", block.Code, org.BlockApproval)
	}
	// Mutant 3: an empty scope authorizes nobody.
	scopeless := changeIntent()
	scopeless.OrgScope = nil
	block = blockOf(t, mustCompileErr(t, scopeless, intentFixture()))
	if block.Code != org.BlockScope {
		t.Fatalf("empty scope block = %s, want %s", block.Code, org.BlockScope)
	}
	// Mutant 4: the graph moving under the intent (new graph version with
	// a superseding edge) reads stale at snapshot.
	dir = intentFixture()
	edge := dir.Edges["edge/jane"]
	edge.Ended = true
	edge.EndDate = "2026-01-15"
	dir.Edges["edge/jane"] = edge
	block = blockOf(t, mustCompileErr(t, changeIntent(), dir))
	if block.Code != org.BlockStaleRelationship || !block.Replan {
		t.Fatalf("moved graph block = %s replan=%v, want STALE_RELATIONSHIP replan=true", block.Code, block.Replan)
	}
}
