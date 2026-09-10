package org_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
)

// fixture builds the CONF-007 directory: Jane reports to Alice; Bob is an
// eligible peer manager under Carol.
func fixture() org.Directory {
	return org.Directory{
		People: map[string]org.Person{
			"jane":  {ID: "jane", Active: true},
			"alice": {ID: "alice", Active: true},
			"bob":   {ID: "bob", Active: true},
			"carol": {ID: "carol", Active: true},
			"hrbp":  {ID: "hrbp", Active: true},
		},
		Edges: map[string]org.Edge{
			"edge/jane":  {EdgeID: "edge/jane", WorkerID: "jane", ManagerID: "alice", StartDate: "2025-06-01", Revision: 1},
			"edge/alice": {EdgeID: "edge/alice", WorkerID: "alice", ManagerID: "carol", StartDate: "2024-01-01", Revision: 1},
			"edge/bob":   {EdgeID: "edge/bob", WorkerID: "bob", ManagerID: "carol", StartDate: "2024-01-01", Revision: 1},
		},
		GraphVersion:     7,
		AuthorityVersion: 3,
	}
}

func drive(t *testing.T, dir *org.Directory, id string) org.ManagerChange {
	t.Helper()
	effective := "2026-02-01"
	change, err := org.Propose(id, "jane", "edge/jane", "bob", effective, "reorg", *dir)
	if err != nil {
		t.Fatal(err)
	}
	if change, err = org.Validate(change, *dir); err != nil {
		t.Fatal(err)
	}
	approvals := []org.Approval{
		{ApproverID: "alice", Role: "current-manager", AuthorityVersion: 3},
		{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3},
	}
	if change, err = org.Approve(change, *dir, approvals...); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Schedule(change, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Revalidate(change, *dir); err != nil {
		t.Fatal(err)
	}
	if change, err = org.ApplyChange(change, dir); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Observe(change, *dir); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Reconcile(change); err != nil {
		t.Fatal(err)
	}
	return change
}

func TestTodo_CONF_007(t *testing.T) {
	dir := fixture()
	change := drive(t, &dir, "chg/1")

	want := []org.ChangeState{
		org.ChangeProposed, org.ChangeValidated, org.ChangeApproved,
		org.ChangeScheduled, org.ChangeRevalidated, org.ChangeChanged,
		org.ChangeObserved, org.ChangeReconciled,
	}
	if len(change.Story) != len(want) {
		t.Fatalf("story = %v, want exactly %v", change.Story, want)
	}
	for i, state := range want {
		if change.Story[i] != state {
			t.Fatalf("story[%d] = %s, want %s (full story %v)", i, change.Story[i], state, change.Story)
		}
	}
	manager, ok := org.CurrentManager(dir, "jane", "2026-02-01")
	if !ok || manager != "bob" {
		t.Fatalf("Jane's manager on 2026-02-01 = %s, want bob", manager)
	}
	// Before the effective date the old edge still projects.
	manager, ok = org.CurrentManager(dir, "jane", "2026-01-15")
	if !ok || manager != "alice" {
		t.Fatalf("Jane's manager on 2026-01-15 = %s, want alice", manager)
	}
}

func TestTodo_CONF_007_Conformance(t *testing.T) {
	dir := fixture()
	change, err := org.Propose("chg/1", "jane", "edge/jane", "bob", "2026-02-01", "reorg", dir)
	if err != nil {
		t.Fatal(err)
	}
	// Out-of-order steps are refused: approve before validate, schedule
	// before approve, commit before revalidate, observe before change,
	// reconcile before observe.
	if _, err := org.Approve(change, dir, org.Approval{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3}); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("approve before validate = %v, want ErrChangeState", err)
	}
	if _, err := org.Schedule(change, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("schedule before approve = %v, want ErrChangeState", err)
	}
	if _, err := org.ApplyChange(change, &dir); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("commit before revalidate = %v, want ErrChangeState", err)
	}
	if _, err := org.Observe(change, dir); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("observe before change = %v, want ErrChangeState", err)
	}
	if _, err := org.Reconcile(change); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("reconcile before observe = %v, want ErrChangeState", err)
	}
	// Scheduling before the effective date waits durably.
	validated, err := org.Validate(change, dir)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := org.Approve(validated, dir, org.Approval{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := org.Schedule(approved, time.Date(2026, time.January, 15, 9, 0, 0, 0, time.UTC)); !errors.Is(err, org.ErrChangeState) {
		t.Fatalf("early schedule = %v, want ErrChangeState", err)
	}
}

func TestTodo_CONF_007_Mutation(t *testing.T) {
	newChange := func(t *testing.T, dir org.Directory, worker, old, newMgr string) org.ManagerChange {
		t.Helper()
		change, err := org.Propose("chg/x", worker, old, newMgr, "2026-02-01", "reorg", dir)
		if err != nil {
			t.Fatal(err)
		}
		return change
	}

	// Mutant 1: inactive proposed manager never reaches commit.
	dir := fixture()
	dir.People["bob"] = org.Person{ID: "bob", Active: false}
	if _, err := org.Validate(newChange(t, dir, "jane", "edge/jane", "bob"), dir); !errors.Is(err, org.ErrInactiveManager) {
		t.Fatalf("inactive manager = %v, want ErrInactiveManager", err)
	}

	// Mutant 2: self-management is refused.
	dir = fixture()
	if _, err := org.Validate(newChange(t, dir, "jane", "edge/jane", "jane"), dir); !errors.Is(err, org.ErrSelfManager) {
		t.Fatalf("self manager = %v, want ErrSelfManager", err)
	}

	// Mutant 3: a cycle (Alice reports to Jane) is refused.
	dir = fixture()
	dir.Edges["edge/alice"] = org.Edge{EdgeID: "edge/alice", WorkerID: "alice", ManagerID: "jane", StartDate: "2024-01-01", Revision: 1}
	if _, err := org.Validate(newChange(t, dir, "jane", "edge/jane", "alice"), dir); !errors.Is(err, org.ErrManagerCycle) {
		t.Fatalf("cycle = %v, want ErrManagerCycle", err)
	}

	// Mutant 4: a future termination colliding with the change blocks commit.
	dir = fixture()
	dir.Future = []org.FutureEvent{{WorkerID: "jane", Kind: "termination", EffectiveDate: "2026-02-01"}}
	change := newChange(t, dir, "jane", "edge/jane", "bob")
	var err error
	if change, err = org.Validate(change, dir); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Approve(change, dir, org.Approval{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3}); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Schedule(change, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err = org.Revalidate(change, dir); !errors.Is(err, org.ErrFutureConflict) {
		t.Fatalf("future conflict = %v, want ErrFutureConflict", err)
	}

	// Mutant 5: authority moving between approval and execution is stale.
	dir = fixture()
	change = newChange(t, dir, "jane", "edge/jane", "bob")
	if change, err = org.Validate(change, dir); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Approve(change, dir, org.Approval{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3}); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Schedule(change, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	dir.AuthorityVersion = 4
	if _, err = org.Revalidate(change, dir); !errors.Is(err, org.ErrStaleAuthority) {
		t.Fatalf("stale authority = %v, want ErrStaleAuthority", err)
	}

	// Mutant 6: a proposal edited after approval never reaches commit.
	dir2 := fixture()
	change = newChange(t, dir2, "jane", "edge/jane", "bob")
	if change, err = org.Validate(change, dir2); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Approve(change, dir2, org.Approval{ApproverID: "hrbp", Role: "hrbp", AuthorityVersion: 3}); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Schedule(change, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if change, err = org.Revalidate(change, dir2); err != nil {
		t.Fatal(err)
	}
	change.NewManagerID = "carol"
	if _, err = org.ApplyChange(change, &dir2); !errors.Is(err, org.ErrProposalTampered) {
		t.Fatalf("changed proposal = %v, want ErrProposalTampered", err)
	}
}
